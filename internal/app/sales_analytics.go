package app

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"

	"gorm.io/gorm"
	"licentivo-launch/internal/storage"
)

type SalesQuery struct {
	From        string `json:"from"`
	To          string `json:"to"`
	TimeZone    string `json:"time_zone"`
	Environment string `json:"environment"`
	Currency    string `json:"currency"`
	ProductID   string `json:"product_id"`
	PlanID      string `json:"plan_id"`
}

type SalesMetrics struct {
	Gross           int64 `json:"gross_minor"`
	Refunds         int64 `json:"refund_minor"`
	Reversals       int64 `json:"reversal_minor"`
	Net             int64 `json:"net_minor"`
	Orders          int   `json:"orders"`
	Customers       int   `json:"customers"`
	Average         int64 `json:"average_minor"`
	NewCustomers    int   `json:"new_customers"`
	RepeatCustomers int   `json:"repeat_customers"`
}

type SalesGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	SalesMetrics
	Attempts  int64 `json:"attempts"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
	Pending   int64 `json:"pending"`
}

type SalesDay struct {
	Date string `json:"date"`
	SalesMetrics
	PreviousNet int64 `json:"previous_net_minor"`
}

type SalesOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ProductID string `json:"product_id,omitempty"`
}

type SalesReport struct {
	Query            SalesQuery    `json:"query"`
	Metrics          SalesMetrics  `json:"metrics"`
	Previous         SalesMetrics  `json:"previous"`
	Days             []SalesDay    `json:"days"`
	Products         []SalesGroup  `json:"products"`
	Plans            []SalesGroup  `json:"plans"`
	Actions          []SalesGroup  `json:"actions"`
	Channels         []SalesGroup  `json:"channels"`
	Currencies       []string      `json:"currencies"`
	ProductOptions   []SalesOption `json:"product_options"`
	PlanOptions      []SalesOption `json:"plan_options"`
	TrafficCollected bool          `json:"traffic_collected"`
	GeneratedAt      time.Time     `json:"generated_at"`
}

type salesWindow struct {
	start, end, previous time.Time
	location             *time.Location
	dates                []string
}

func normalizeSalesQuery(q SalesQuery, at time.Time) (SalesQuery, salesWindow, error) {
	var w salesWindow
	if q.TimeZone == "" {
		q.TimeZone = "UTC"
	}
	loc, err := time.LoadLocation(q.TimeZone)
	if err != nil {
		return q, w, problem(400, "SALES_TIME_ZONE_INVALID", "请选择有效的报表时区")
	}
	w.location = loc
	today := at.In(loc).Format(time.DateOnly)
	if q.To == "" {
		q.To = today
	}
	last, err := time.ParseInLocation(time.DateOnly, q.To, loc)
	if err != nil || q.To > today {
		return q, w, ErrInvalid
	}
	if q.From == "" {
		q.From = last.AddDate(0, 0, -29).Format(time.DateOnly)
	}
	first, err := time.ParseInLocation(time.DateOnly, q.From, loc)
	if err != nil || first.After(last) {
		return q, w, ErrInvalid
	}
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		w.dates = append(w.dates, d.Format(time.DateOnly))
		if len(w.dates) > 366 {
			return q, w, problem(400, "SALES_RANGE_TOO_LONG", "单次报表最多查询 366 天")
		}
	}
	w.start, w.end, w.previous = first.UTC(), last.AddDate(0, 0, 1).UTC(), first.AddDate(0, 0, -len(w.dates)).UTC()
	if q.Environment == "" {
		q.Environment = "live"
	}
	if q.Environment != "live" && q.Environment != "sandbox" {
		return q, w, ErrInvalid
	}
	q.Currency = strings.ToUpper(strings.TrimSpace(q.Currency))
	if q.Currency != "" && (len(q.Currency) != 3 || strings.Trim(q.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "") {
		return q, w, ErrInvalid
	}
	for _, id := range []string{q.ProductID, q.PlanID} {
		if id != "" && !integrationUUIDPattern.MatchString(id) {
			return q, w, ErrInvalid
		}
	}
	return q, w, nil
}

type salesFact struct {
	SourceID                                                                    string
	Kind                                                                        string
	At                                                                          time.Time
	Amount                                                                      int64
	OrderID, UserID, ProductID, ProductName, PlanID, PlanName, Provider, Action string
}

type salesBucket struct {
	metrics SalesMetrics
	orders  map[string]bool
	buyers  map[string]map[string]bool
}

func newSalesBucket() *salesBucket {
	return &salesBucket{orders: map[string]bool{}, buyers: map[string]map[string]bool{}}
}
func (b *salesBucket) add(f salesFact) error {
	// The UI uses integer minor units. Reject amounts it cannot represent exactly.
	const limit = int64(9007199254740991)
	if f.Amount < 0 || f.Amount > limit {
		return problem(422, "SALES_AMOUNT_TOO_LARGE", "报表金额超出精确显示范围，请缩小查询范围")
	}
	var target *int64
	switch f.Kind {
	case "payment":
		target = &b.metrics.Gross
		b.orders[f.OrderID] = true
		if b.buyers[f.UserID] == nil {
			b.buyers[f.UserID] = map[string]bool{}
		}
		b.buyers[f.UserID][f.OrderID] = true
	case "refund":
		target = &b.metrics.Refunds
	case "reversal":
		target = &b.metrics.Reversals
	default:
		return ErrStorage
	}
	if *target > limit-f.Amount {
		return problem(422, "SALES_AMOUNT_TOO_LARGE", "报表金额超出精确显示范围，请缩小查询范围")
	}
	*target += f.Amount
	return nil
}
func (b *salesBucket) finish() SalesMetrics {
	v := b.metrics
	v.Net = v.Gross - v.Refunds - v.Reversals
	v.Orders, v.Customers = len(b.orders), len(b.buyers)
	if v.Orders > 0 {
		v.Average = v.Gross / int64(v.Orders)
	}
	return v
}

// Each branch filters on its own event time before the UNION. No current order
// status filter may erase a capture after a later refund or reversal.
func salesLedger(q SalesQuery, start, end time.Time) (string, []any) {
	filter := " AND p.environment = ? AND p.currency = ?"
	extra := []any{q.Environment, q.Currency}
	if q.ProductID != "" {
		filter += " AND o.product_id = ?"
		extra = append(extra, q.ProductID)
	}
	if q.PlanID != "" {
		filter += " AND pv.plan_id = ?"
		extra = append(extra, q.PlanID)
	}
	joins := " JOIN orders o ON o.id=p.order_id AND o.environment=p.environment JOIN products pr ON pr.id=o.product_id JOIN plan_versions pv ON pv.id=o.plan_version_id JOIN plans pl ON pl.id=pv.plan_id JOIN payment_accounts pa ON pa.id=p.account_id AND pa.environment=p.environment "
	columns := ",o.id order_id,o.user_id,o.product_id,pr.name product_name,pv.plan_id,COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(o.price_snapshot,'$.plan_name')),''),pl.name) plan_name,pa.provider,o.action"
	parts := []string{
		"SELECT p.id entry_id,'payment' kind,p.paid_at event_time,p.amount_minor amount" + columns + " FROM payments p" + joins + " WHERE p.status='succeeded' AND p.paid_at >= ? AND p.paid_at < ?" + filter,
		"SELECT r.id entry_id,'refund' kind,COALESCE(r.completed_at,r.created_at) event_time,r.amount_minor amount" + columns + " FROM refunds r JOIN payments p ON p.id=r.payment_id" + joins + " WHERE r.status='succeeded' AND COALESCE(r.completed_at,r.created_at) >= ? AND COALESCE(r.completed_at,r.created_at) < ?" + filter,
		"SELECT r.id entry_id,'reversal' kind,r.created_at event_time,r.amount_minor amount" + columns + " FROM payment_reversals r JOIN payments p ON p.id=r.payment_id" + joins + " WHERE r.created_at >= ? AND r.created_at < ?" + filter,
	}
	args := []any{}
	for range parts {
		args = append(args, start, end)
		args = append(args, extra...)
	}
	return strings.Join(parts, " UNION ALL "), args
}

type salesGroups struct {
	buckets map[string]*salesBucket
	names   map[string]string
}

func newSalesGroups() *salesGroups {
	return &salesGroups{buckets: map[string]*salesBucket{}, names: map[string]string{}}
}
func (g *salesGroups) add(id, name string, f salesFact) error {
	if g.buckets[id] == nil {
		g.buckets[id] = newSalesBucket()
		g.names[id] = name
	}
	return g.buckets[id].add(f)
}
func (g *salesGroups) finish() []SalesGroup {
	rows := []SalesGroup{}
	for id, b := range g.buckets {
		rows = append(rows, SalesGroup{ID: id, Name: g.names[id], SalesMetrics: b.finish()})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Net == rows[j].Net {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Net > rows[j].Net
	})
	return rows
}

func (s *Service) SalesAnalytics(ctx context.Context, p Principal, q SalesQuery) (SalesReport, error) {
	out := SalesReport{Days: []SalesDay{}, Products: []SalesGroup{}, Plans: []SalesGroup{}, Actions: []SalesGroup{}, Channels: []SalesGroup{}, Currencies: []string{}, ProductOptions: []SalesOption{}, PlanOptions: []SalesOption{}, GeneratedAt: now()}
	if !p.CanManage() {
		return out, ErrForbidden
	}
	q, w, err := normalizeSalesQuery(q, out.GeneratedAt)
	if err != nil {
		return out, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw("SELECT DISTINCT currency FROM (SELECT currency FROM payments WHERE environment=? UNION SELECT currency FROM plan_versions) c ORDER BY currency", q.Environment).Scan(&out.Currencies).Error; err != nil {
			return err
		}
		if len(out.Currencies) == 0 {
			out.Currencies = []string{"USD"}
		}
		if q.Currency == "" {
			q.Currency = out.Currencies[0]
		}
		var products []storage.Product
		if err := tx.Select("id,name").Order("name,id").Find(&products).Error; err != nil {
			return err
		}
		for _, row := range products {
			out.ProductOptions = append(out.ProductOptions, SalesOption{ID: row.ID, Name: row.Name})
		}
		var plans []storage.Plan
		if err := tx.Select("id,name,product_id").Order("name,id").Find(&plans).Error; err != nil {
			return err
		}
		for _, row := range plans {
			name := row.Name
			for _, product := range products {
				if product.ID == row.ProductID {
					name = product.Name + " · " + row.Name
					break
				}
			}
			out.PlanOptions = append(out.PlanOptions, SalesOption{ID: row.ID, Name: name, ProductID: row.ProductID})
		}
		out.Query = q
		ledger, args := salesLedger(q, w.previous, w.end)
		rows, err := tx.Raw(ledger, args...).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		current, previous := newSalesBucket(), newSalesBucket()
		productsGroup, plansGroup, actions, channels := newSalesGroups(), newSalesGroups(), newSalesGroups(), newSalesGroups()
		days, previousDays := map[string]*salesBucket{}, map[string]*salesBucket{}
		for rows.Next() {
			var f salesFact
			if err := rows.Scan(&f.SourceID, &f.Kind, &f.At, &f.Amount, &f.OrderID, &f.UserID, &f.ProductID, &f.ProductName, &f.PlanID, &f.PlanName, &f.Provider, &f.Action); err != nil {
				return err
			}
			date := f.At.In(w.location).Format(time.DateOnly)
			if f.At.Before(w.start) {
				if err := previous.add(f); err != nil {
					return err
				}
				if previousDays[date] == nil {
					previousDays[date] = newSalesBucket()
				}
				if err := previousDays[date].add(f); err != nil {
					return err
				}
				continue
			}
			if err := current.add(f); err != nil {
				return err
			}
			if days[date] == nil {
				days[date] = newSalesBucket()
			}
			if err := days[date].add(f); err != nil {
				return err
			}
			for _, group := range []struct {
				g        *salesGroups
				id, name string
			}{{productsGroup, f.ProductID, f.ProductName}, {plansGroup, f.PlanID, f.PlanName}, {actions, f.Action, f.Action}, {channels, f.Provider, f.Provider}} {
				if err := group.g.add(group.id, group.name, f); err != nil {
					return err
				}
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		out.Metrics, out.Previous = current.finish(), previous.finish()
		// A customer's first payment is evaluated using the same product/plan,
		// environment and currency filters as the selected report.
		prior := map[string]bool{}
		priorSQL := "SELECT DISTINCT o.user_id FROM payments p JOIN orders o ON o.id=p.order_id JOIN plan_versions pv ON pv.id=o.plan_version_id WHERE p.status='succeeded' AND p.paid_at < ? AND p.environment=? AND p.currency=?"
		priorArgs := []any{w.start, q.Environment, q.Currency}
		if q.ProductID != "" {
			priorSQL += " AND o.product_id=?"
			priorArgs = append(priorArgs, q.ProductID)
		}
		if q.PlanID != "" {
			priorSQL += " AND pv.plan_id=?"
			priorArgs = append(priorArgs, q.PlanID)
		}
		priorRows, err := tx.Raw(priorSQL, priorArgs...).Rows()
		if err != nil {
			return err
		}
		for priorRows.Next() {
			var id string
			if err := priorRows.Scan(&id); err != nil {
				priorRows.Close()
				return err
			}
			prior[id] = true
		}
		if err := priorRows.Err(); err != nil {
			priorRows.Close()
			return err
		}
		priorRows.Close()
		for id, orders := range current.buyers {
			if !prior[id] {
				out.Metrics.NewCustomers++
			}
			if prior[id] || len(orders) > 1 {
				out.Metrics.RepeatCustomers++
			}
		}
		for i, date := range w.dates {
			day := SalesDay{Date: date}
			if b := days[date]; b != nil {
				day.SalesMetrics = b.finish()
			}
			pd := w.previous.In(w.location).AddDate(0, 0, i).Format(time.DateOnly)
			if b := previousDays[pd]; b != nil {
				day.PreviousNet = b.finish().Net
			}
			out.Days = append(out.Days, day)
		}
		out.Products, out.Plans, out.Actions, out.Channels = productsGroup.finish(), plansGroup.finish(), actions.finish(), channels.finish()
		for i := range out.Plans {
			for _, option := range out.PlanOptions {
				if option.ID == out.Plans[i].ID {
					out.Plans[i].Name = option.Name
					break
				}
			}
		}
		attemptSQL := "SELECT pa.provider,COUNT(*) attempts,SUM(p.status='succeeded') succeeded,SUM(p.status='failed') failed,SUM(p.status IN ('creating','pending','unknown')) pending FROM payments p JOIN payment_accounts pa ON pa.id=p.account_id JOIN orders o ON o.id=p.order_id JOIN plan_versions pv ON pv.id=o.plan_version_id WHERE p.environment=? AND p.currency=? AND p.created_at>=? AND p.created_at<?"
		attemptArgs := []any{q.Environment, q.Currency, w.start, w.end}
		if q.ProductID != "" {
			attemptSQL += " AND o.product_id=?"
			attemptArgs = append(attemptArgs, q.ProductID)
		}
		if q.PlanID != "" {
			attemptSQL += " AND pv.plan_id=?"
			attemptArgs = append(attemptArgs, q.PlanID)
		}
		attemptSQL += " GROUP BY pa.provider"
		var attempts []struct {
			Provider                             string
			Attempts, Succeeded, Failed, Pending int64
		}
		if err := tx.Raw(attemptSQL, attemptArgs...).Scan(&attempts).Error; err != nil {
			return err
		}
		for _, a := range attempts {
			idx := -1
			for i, row := range out.Channels {
				if row.ID == a.Provider {
					idx = i
					break
				}
			}
			if idx < 0 {
				out.Channels = append(out.Channels, SalesGroup{ID: a.Provider, Name: a.Provider})
				idx = len(out.Channels) - 1
			}
			out.Channels[idx].Attempts, out.Channels[idx].Succeeded, out.Channels[idx].Failed, out.Channels[idx].Pending = a.Attempts, a.Succeeded, a.Failed, a.Pending
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, databaseError(err)
	}
	return out, nil
}

type SalesTransaction struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	EventTime   time.Time `json:"event_time"`
	AmountMinor int64     `json:"amount_minor"`
	OrderID     string    `json:"order_id"`
	ProductName string    `json:"product_name"`
	PlanName    string    `json:"plan_name"`
	Provider    string    `json:"provider"`
}
type SalesTransactionPage struct {
	Items  []SalesTransaction `json:"items"`
	Total  int64              `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

func (s *Service) SalesTransactions(ctx context.Context, p Principal, q SalesQuery, kind string, limit, offset int) (SalesTransactionPage, error) {
	out := SalesTransactionPage{Items: []SalesTransaction{}, Limit: limit, Offset: offset}
	if !p.CanManage() {
		return out, ErrForbidden
	}
	q, w, err := normalizeSalesQuery(q, now())
	if err != nil {
		return out, err
	}
	if q.Currency == "" || limit < 1 || limit > 100 || offset < 0 || offset > 100000 || (kind != "" && kind != "payment" && kind != "refund" && kind != "reversal") {
		return out, ErrInvalid
	}
	ledger, args := salesLedger(q, w.start, w.end)
	where := ""
	if kind != "" {
		where = " WHERE kind=?"
		args = append(args, kind)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw("SELECT COUNT(*) FROM ("+ledger+") ledger"+where, args...).Scan(&out.Total).Error; err != nil {
			return err
		}
		selectArgs := append(append([]any{}, args...), limit, offset)
		return tx.Raw("SELECT entry_id id,kind,event_time,amount amount_minor,order_id,product_name,plan_name,provider FROM ("+ledger+") ledger"+where+" ORDER BY event_time DESC,entry_id,kind LIMIT ? OFFSET ?", selectArgs...).Scan(&out.Items).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return out, databaseError(err)
}
