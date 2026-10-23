package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"licentivo-launch/internal/app"
	"licentivo-launch/internal/security"
)

type application struct {
	service            *app.Service
	publicURL          func() string
	forceSecureCookies bool
	limiter            limiter
}
type endpoint func(http.ResponseWriter, *http.Request, app.Principal) (any, error)
type window struct {
	count int
	until time.Time
}
type limiter struct {
	mu    sync.Mutex
	items map[string]window
}

func (l *limiter) allow(key string, max int, period time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.items == nil {
		l.items = map[string]window{}
	}
	at := time.Now()
	v := l.items[key]
	if !v.until.After(at) {
		v = window{until: at.Add(period)}
	}
	if len(l.items) >= 10000 {
		for key, v := range l.items {
			if !v.until.After(at) {
				delete(l.items, key)
			}
		}
		if len(l.items) >= 10000 {
			return false
		}
	}
	if v.count >= max {
		return false
	}
	v.count++
	l.items[key] = v
	return true
}

func (a *application) route(mux *http.ServeMux, pattern string, authenticated bool, limit int, fn endpoint) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if a.service == nil {
			apiFail(w, r, &app.Error{Status: 503, Code: "DATABASE_REQUIRED", Message: "请先配置数据库和主密钥并重启服务"})
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" && origin != a.publicURL() {
				apiFail(w, r, app.ErrForbidden)
				return
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				apiFail(w, r, app.ErrForbidden)
				return
			}
		}
		if limit > 0 {
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			if host == "" {
				host = r.RemoteAddr
			}
			if !a.limiter.allow(pattern+"|"+host, limit, time.Minute) {
				w.Header().Set("Retry-After", "60")
				apiFail(w, r, &app.Error{Status: 429, Code: "RATE_LIMITED", Message: "操作过于频繁，请稍后重试"})
				return
			}
		}
		var p app.Principal
		if authenticated {
			var token, kind string
			if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
				token, kind = strings.TrimPrefix(header, "Bearer "), "desktop"
			} else if cookie, err := r.Cookie("launch_session"); err == nil {
				token, kind = cookie.Value, "website"
			}
			var err error
			p, err = a.service.Authenticate(r.Context(), token, kind)
			if err != nil {
				apiFail(w, r, err)
				return
			}
			if kind == "website" && r.Method != "GET" && r.Method != "HEAD" {
				csrf := r.Header.Get("X-CSRF-Token")
				if len(csrf) != 43 || subtle.ConstantTimeCompare(security.Digest(csrf), p.Session.CSRFHash) != 1 {
					apiFail(w, r, &app.Error{Status: 403, Code: "CSRF_INVALID", Message: "页面会话已变化，请刷新后重试"})
					return
				}
			}
			if p.User.PasswordChangeRequired && pattern != "GET /api/v1/me" && pattern != "POST /api/v1/me/password" && pattern != "POST /api/v1/auth/logout" {
				apiFail(w, r, &app.Error{Status: 403, Code: "PASSWORD_CHANGE_REQUIRED", Message: "请先修改初始密码再使用管理功能"})
				return
			}
		}
		result, err := fn(w, r, p)
		if err != nil {
			apiFail(w, r, err)
			return
		}
		if raw, ok := result.(app.PaymentWebhookResponse); ok {
			for name, values := range raw.Header {
				for _, value := range values {
					w.Header().Add(name, value)
				}
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write(raw.Body)
			return
		}
		respond(w, r, 200, result)
	})
}

func input(w http.ResponseWriter, r *http.Request, out any) error {
	return inputLimit(w, r, out, 64<<10)
}

func inputLimit(w http.ResponseWriter, r *http.Request, out any, limit int64) error {
	typeName, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typeName != "application/json" {
		return &app.Error{Status: 415, Code: "JSON_REQUIRED", Message: "请求必须使用 JSON 格式"}
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return app.ErrInvalid
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return app.ErrInvalid
	}
	return nil
}

func apiFail(w http.ResponseWriter, r *http.Request, err error) {
	var business *app.Error
	if !errors.As(err, &business) {
		business = &app.Error{Status: 503, Code: "SERVICE_UNAVAILABLE", Message: "服务暂不可用，请稍后重试"}
	}
	if business.Code == "ENTITLEMENT_PENDING" {
		w.Header().Set("Retry-After", "3")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(business.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": business.Code, "message": business.Message}, "request_id": w.Header().Get("X-Request-ID")})
}

func (a *application) cookie(w http.ResponseWriter, name, value string, expires time.Time, httpOnly bool) {
	maxAge := int(time.Until(expires).Seconds())
	if value == "" {
		maxAge = -1
	}
	secure := a.forceSecureCookies || strings.HasPrefix(a.publicURL(), "https://")
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: httpOnly, Secure: secure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge})
}

func requestLocale(r *http.Request) string {
	if locale := r.URL.Query().Get("locale"); locale != "" {
		return locale
	}
	return r.Header.Get("Accept-Language")
}

func (a *application) register(mux *http.ServeMux) {
	a.registerCommerce(mux)
	a.registerIntegration(mux)
	mux.HandleFunc("GET /api/v1/product-document-images/{id}", func(w http.ResponseWriter, r *http.Request) {
		if a.service == nil {
			apiFail(w, r, &app.Error{Status: 503, Code: "DATABASE_REQUIRED", Message: "请先配置数据库和主密钥并重启服务"})
			return
		}
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host == "" {
			host = r.RemoteAddr
		}
		if !a.limiter.allow("product-document-image|"+host, 240, time.Minute) {
			w.Header().Set("Retry-After", "60")
			apiFail(w, r, &app.Error{Status: 429, Code: "RATE_LIMITED", Message: "图片请求过于频繁，请稍后重试"})
			return
		}
		image, err := a.service.OpenProductDocumentImage(r.Context(), r.PathValue("id"))
		if err != nil {
			apiFail(w, r, err)
			return
		}
		defer image.File.Close()
		w.Header().Set("Content-Type", image.ContentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": image.Name}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, image.Name, image.UpdatedAt, image.File)
	})
	mux.HandleFunc("GET /api/v1/downloads/{id}/{name}", func(w http.ResponseWriter, r *http.Request) {
		if a.service == nil {
			apiFail(w, r, &app.Error{Status: 503, Code: "DATABASE_REQUIRED", Message: "请先配置数据库和主密钥并重启服务"})
			return
		}
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if host == "" {
			host = r.RemoteAddr
		}
		if !a.limiter.allow("product-download|"+host, 120, time.Minute) {
			w.Header().Set("Retry-After", "60")
			apiFail(w, r, &app.Error{Status: 429, Code: "RATE_LIMITED", Message: "下载请求过于频繁，请稍后重试"})
			return
		}
		download, err := a.service.OpenProductDownload(r.Context(), r.PathValue("id"))
		if err != nil {
			apiFail(w, r, err)
			return
		}
		defer download.File.Close()
		contentType := download.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": download.Name}))
		http.ServeContent(w, r, download.Name, download.UpdatedAt, download.File)
	})
	accepted := map[string]any{"accepted": true, "message": "如信息有效，邮件会发送至该地址，请检查收件箱"}
	a.route(mux, "GET /api/v1/site", false, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		env := r.URL.Query().Get("environment")
		if env == "" {
			env = "live"
		}
		return a.service.Site(r.Context(), env, r.URL.Query().Get("product"), requestLocale(r))
	})
	a.route(mux, "POST /api/v1/setup", false, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.BootstrapInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"installed": true}, a.service.Bootstrap(r.Context(), in)
	})
	a.route(mux, "POST /api/v1/auth/register", false, 8, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return accepted, a.service.Register(r.Context(), in.Email, in.Password)
	})
	a.route(mux, "POST /api/v1/auth/login", false, 20, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Kind     string `json:"kind"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		if in.Kind == "" {
			in.Kind = "website"
		}
		if in.Kind == "desktop" && r.Header.Get("Origin") != "" {
			return nil, app.ErrForbidden
		}
		grant, err := a.service.Login(r.Context(), in.Email, in.Password, in.Kind)
		if err != nil {
			return nil, err
		}
		if in.Kind == "website" {
			a.cookie(w, "launch_session", grant.AccessToken, grant.ExpiresAt, true)
			a.cookie(w, "launch_csrf", grant.CSRFToken, grant.ExpiresAt, false)
			grant.AccessToken = ""
			grant.CSRFToken = ""
		}
		return grant, nil
	})
	a.route(mux, "POST /api/v1/auth/email", false, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Email string `json:"email"`
			Kind  string `json:"kind"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return accepted, a.service.RequestEmail(r.Context(), in.Email, in.Kind)
	})
	a.route(mux, "POST /api/v1/auth/complete-email", false, 20, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"completed": true}, a.service.CompleteEmail(r.Context(), in.Token, in.Password)
	})
	a.route(mux, "GET /api/v1/me", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return map[string]any{"user": p.View(), "session_id": p.Session.ID}, nil
	})
	a.route(mux, "POST /api/v1/auth/logout", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		err := a.service.Logout(r.Context(), p)
		if err == nil {
			a.cookie(w, "launch_session", "", time.Unix(1, 0), true)
			a.cookie(w, "launch_csrf", "", time.Unix(1, 0), false)
		}
		return map[string]bool{"completed": true}, err
	})
	a.route(mux, "GET /api/v1/me/sessions", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Sessions(r.Context(), p)
	})
	a.route(mux, "DELETE /api/v1/me/sessions/{id}", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return map[string]bool{"completed": true}, a.service.RevokeSession(r.Context(), p, r.PathValue("id"))
	})
	a.route(mux, "POST /api/v1/me/password", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Current string `json:"current_password"`
			New     string `json:"new_password"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"completed": true}, a.service.ChangePassword(r.Context(), p, in.Current, in.New)
	})
	a.route(mux, "POST /api/v1/me/email", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return accepted, a.service.ChangeEmail(r.Context(), p, in.Email, in.Password)
	})
	a.route(mux, "GET /api/v1/me/orders", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Orders(r.Context(), p)
	})
	a.route(mux, "GET /api/v1/admin/dashboard", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Dashboard(r.Context(), p)
	})
	a.route(mux, "GET /api/v1/admin/readiness", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.OperationalReadiness(r.Context(), p, "")
	})
	a.route(mux, "POST /api/v1/admin/readiness/check", true, 3, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Key string `json:"key"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.OperationalReadiness(r.Context(), p, in.Key)
	})
	a.route(mux, "GET /api/v1/admin/catalog", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.AdminCatalog(r.Context(), p)
	})
	a.route(mux, "PUT /api/v1/admin/brand", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.Brand
		if err := inputLimit(w, r, &in, 192<<10); err != nil {
			return nil, err
		}
		return in, a.service.SaveBrand(r.Context(), p, in)
	})
	a.route(mux, "PUT /api/v1/admin/product", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.ProductView
		if err := inputLimit(w, r, &in, 256<<10); err != nil {
			return nil, err
		}
		return a.service.SaveProduct(r.Context(), p, in)
	})
	a.route(mux, "PUT /api/v1/admin/products/{id}/downloads/{platform}", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.ProductDownloadInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveProductDownload(r.Context(), p, r.PathValue("id"), r.PathValue("platform"), in)
	})
	a.route(mux, "POST /api/v1/admin/products/{id}/downloads/{platform}", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		r.Body = http.MaxBytesReader(w, r.Body, app.MaxProductUploadSize+(2<<20))
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, &app.Error{Status: 413, Code: "UPLOAD_TOO_LARGE", Message: "安装包不能超过 1 GB"}
			}
			return nil, app.ErrInvalid
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			return nil, &app.Error{Status: 400, Code: "UPLOAD_REQUIRED", Message: "请选择要上传的安装包"}
		}
		defer file.Close()
		return a.service.SaveUploadedProductDownload(r.Context(), p, r.PathValue("id"), r.PathValue("platform"), r.FormValue("version"), header.Filename, file)
	})
	a.route(mux, "POST /api/v1/admin/products/{id}/document-images", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		r.Body = http.MaxBytesReader(w, r.Body, app.MaxProductDocumentImageSize+(1<<20))
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return nil, &app.Error{Status: 413, Code: "DOCUMENT_IMAGE_TOO_LARGE", Message: "文档图片不能超过 8 MB"}
			}
			return nil, app.ErrInvalid
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			return nil, &app.Error{Status: 400, Code: "DOCUMENT_IMAGE_REQUIRED", Message: "请选择要插入的图片"}
		}
		defer file.Close()
		return a.service.SaveProductDocumentImage(r.Context(), p, r.PathValue("id"), header.Filename, file)
	})
	a.route(mux, "DELETE /api/v1/admin/products/{id}/downloads/{platform}", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return map[string]bool{"deleted": true}, a.service.DeleteProductDownload(r.Context(), p, r.PathValue("id"), r.PathValue("platform"))
	})
	a.route(mux, "POST /api/v1/admin/products/{id}/default", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return map[string]bool{"saved": true}, a.service.SetDefaultProduct(r.Context(), p, r.PathValue("id"))
	})
	a.route(mux, "POST /api/v1/admin/plans", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.PlanView
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SavePlan(r.Context(), p, in)
	})
	a.route(mux, "POST /api/v1/admin/plans/{id}/publish", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			VersionID   string `json:"version_id"`
			Environment string `json:"environment"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"published": true}, a.service.PublishPlan(r.Context(), p, r.PathValue("id"), in.VersionID, in.Environment)
	})
	a.route(mux, "GET /api/v1/admin/plans/{id}/versions", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.PlanVersions(r.Context(), p, r.PathValue("id"))
	})
	a.route(mux, "POST /api/v1/admin/plans/{id}/rollback", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			VersionID   string `json:"version_id"`
			Environment string `json:"environment"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"rolled_back": true}, a.service.RollbackPlan(r.Context(), p, r.PathValue("id"), in.VersionID, in.Environment)
	})
	a.route(mux, "POST /api/v1/admin/plans/{id}/archive", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return map[string]bool{"archived": true}, a.service.ArchivePlan(r.Context(), p, r.PathValue("id"))
	})
	a.route(mux, "GET /api/v1/admin/users", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Users(r.Context(), p, r.URL.Query().Get("q"))
	})
	a.route(mux, "POST /api/v1/admin/users/{id}/status", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return map[string]bool{"completed": true}, a.service.SetUserStatus(r.Context(), p, r.PathValue("id"), in.Status, in.Reason)
	})
	a.route(mux, "GET /api/v1/admin/audit", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.Audit(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/mail/retry", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return map[string]bool{"completed": true}, a.service.RetryMail(r.Context(), p)
	})
	a.route(mux, "GET /api/v1/admin/mail/settings", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.MailSettings(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/mail/settings", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.MailSettingsInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveMailSettings(r.Context(), p, in)
	})
	a.route(mux, "POST /api/v1/admin/mail/check", true, 3, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.MailCheckInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.CheckMail(r.Context(), p, in)
	})
	a.route(mux, "GET /api/v1/admin/tls/settings", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.TLSSettings(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/tls/settings", true, 3, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.TLSSettingsInput
		if err := inputLimit(w, r, &in, 512<<10); err != nil {
			return nil, err
		}
		return a.service.SaveTLSSettings(r.Context(), p, in)
	})
	a.route(mux, "GET /api/v1/admin/transport/settings", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.TransportSettings(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/transport/settings", true, 3, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.TransportSettingsInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		result, err := a.service.SaveTransportSettings(r.Context(), p, in)
		if err != nil {
			return nil, err
		}
		if session, cookieErr := r.Cookie("launch_session"); cookieErr == nil {
			a.cookie(w, "launch_session", session.Value, p.Session.ExpiresAt, true)
		}
		if csrf, cookieErr := r.Cookie("launch_csrf"); cookieErr == nil {
			a.cookie(w, "launch_csrf", csrf.Value, p.Session.ExpiresAt, false)
		}
		return result, nil
	})
	a.route(mux, "GET /api/v1/admin/translations", true, 0, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.TranslationStatus(r.Context(), p)
	})
	a.route(mux, "POST /api/v1/admin/translations", true, 5, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		var in app.TranslationSettingsInput
		if err := input(w, r, &in); err != nil {
			return nil, err
		}
		return a.service.SaveTranslationSettings(r.Context(), p, in)
	})
	a.route(mux, "POST /api/v1/admin/translations/sync", true, 3, func(w http.ResponseWriter, r *http.Request, p app.Principal) (any, error) {
		return a.service.SyncTranslations(r.Context(), p)
	})
}
