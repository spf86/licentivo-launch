package domain

import (
	"errors"
	"math/big"
	"time"
)

// Money always uses integer minor units. Currency exponent is provider data.
type Money struct {
	Currency string `json:"currency"`
	Minor    int64  `json:"minor"`
}

func (m Money) Validate() error {
	if m.Minor < 0 || len(m.Currency) != 3 {
		return errors.New("invalid money")
	}
	for _, c := range m.Currency {
		if c < 'A' || c > 'Z' {
			return errors.New("currency must be a three-letter uppercase code")
		}
	}
	return nil
}

// UpgradeDifference rounds a same-currency, same-cycle price difference up.
// big.Int avoids overflow before division; no floating-point money is used.
func UpgradeDifference(current, target Money, start, end, quotedAt time.Time) (Money, error) {
	if current.Validate() != nil || target.Validate() != nil || current.Currency != target.Currency || target.Minor < current.Minor || !start.Before(end) || quotedAt.Before(start) || !quotedAt.Before(end) || end.Sub(start) > 200*366*24*time.Hour {
		return Money{}, errors.New("incompatible or expired upgrade quote")
	}
	total, remaining := int64(end.Sub(start)), int64(end.Sub(quotedAt))
	n := new(big.Int).Mul(big.NewInt(target.Minor-current.Minor), big.NewInt(remaining))
	n.Add(n, big.NewInt(total-1)).Quo(n, big.NewInt(total))
	if !n.IsInt64() {
		return Money{}, errors.New("upgrade amount exceeds supported range")
	}
	return Money{Currency: current.Currency, Minor: n.Int64()}, nil
}
