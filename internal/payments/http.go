package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"licentivo-launch/internal/domain"
)

var (
	ErrUncertain    = errors.New("payment provider result uncertain; query or retry the original operation")
	ErrCredentials  = errors.New("payment credentials rejected")
	ErrRejected     = errors.New("payment request rejected")
	ErrVerification = errors.New("payment verification failed")
)

func client() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func request(ctx context.Context, c *http.Client, method, address, contentType string, body []byte, headers map[string]string, out any) error {
	raw, _, err := requestRaw(ctx, c, method, address, contentType, body, headers)
	if err != nil {
		return err
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return ErrUncertain
	}
	return nil
}

func requestRaw(ctx context.Context, c *http.Client, method, address, contentType string, body []byte, headers map[string]string) ([]byte, http.Header, error) {
	r, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return nil, nil, ErrRejected
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	response, err := c.Do(r)
	if err != nil {
		return nil, nil, ErrUncertain
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return nil, nil, ErrCredentials
	}
	if response.StatusCode == 429 || response.StatusCode >= 500 {
		return nil, nil, ErrUncertain
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, ErrRejected
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, nil, ErrUncertain
	}
	return raw, response.Header, nil
}
func encoded(v any) []byte { raw, _ := json.Marshal(v); return raw }
func validID(value string) bool {
	return value != "" && len(value) <= 160 && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.") == ""
}

// Initial checkout contract deliberately uses currencies with two minor digits.
// More currencies require provider-specific exponent and acceptance validation.
func SupportsAmount(m domain.Money) bool { return validAmount(m) }

func validAmount(m domain.Money) bool {
	return m.Validate() == nil && m.Minor > 0 && m.Minor <= 99999999999 && strings.Contains("|USD|EUR|GBP|CAD|AUD|", "|"+m.Currency+"|")
}
