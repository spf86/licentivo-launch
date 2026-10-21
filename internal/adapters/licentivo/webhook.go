// Package licentivo contains primitives verified against the local upstream
// source. The proposed sales synchronization API has no HTTP client yet.
package licentivo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var ErrWebhook = errors.New("invalid licensing webhook")

// VerifyWebhook authenticates timestamp + "." + event ID + "." + raw body.
// Persistence must deduplicate (connection_id, event_id) after verification.
// allowLegacy is an explicit compatibility choice for X-Licenova-* headers.
func VerifyWebhook(headers http.Header, body, secret []byte, now time.Time, tolerance time.Duration, allowLegacy bool) (string, error) {
	if len(body) == 0 || len(body) > 1<<20 || len(secret) < 32 || tolerance <= 0 || tolerance > 15*time.Minute {
		return "", ErrWebhook
	}
	values := make([]string, 3)
	for i, suffix := range []string{"Event", "Timestamp", "Signature"} {
		modern, old := headers.Values("X-Licentivo-"+suffix), headers.Values("X-Licenova-"+suffix)
		if len(modern) > 1 || len(old) > 1 || (len(modern) == 1 && len(old) == 1 && modern[0] != old[0]) {
			return "", ErrWebhook
		}
		if len(modern) == 1 {
			values[i] = modern[0]
		} else if allowLegacy && len(old) == 1 {
			values[i] = old[0]
		} else {
			return "", ErrWebhook
		}
	}
	eventID, stamp, signature := values[0], values[1], values[2]
	if len(eventID) != 36 || len(stamp) > 12 || !strings.HasPrefix(signature, "sha256=") {
		return "", ErrWebhook
	}
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || seconds <= 0 {
		return "", ErrWebhook
	}
	sent := time.Unix(seconds, 0)
	if sent.Before(now.Add(-tolerance)) || sent.After(now.Add(tolerance)) {
		return "", ErrWebhook
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil || len(provided) != sha256.Size {
		return "", ErrWebhook
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(stamp + "." + eventID + "."))
	mac.Write(body)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return "", ErrWebhook
	}
	return eventID, nil
}
