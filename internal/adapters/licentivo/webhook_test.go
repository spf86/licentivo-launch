package licentivo

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func signedHeaders(body, secret []byte, stamp time.Time, prefix string) http.Header {
	h := http.Header{}
	id := "11111111-1111-4111-8111-111111111111"
	ts := strconv.FormatInt(stamp.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts + "." + id + "."))
	mac.Write(body)
	h.Set(prefix+"Event", id)
	h.Set(prefix+"Timestamp", ts)
	h.Set(prefix+"Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	return h
}

func TestWebhookRawBodyReplayAndCompatibility(t *testing.T) {
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	body, secret := []byte("{ \"id\": 1 }"), bytes.Repeat([]byte{7}, 32)
	h := signedHeaders(body, secret, now, "X-Licentivo-")
	if _, err := VerifyWebhook(h, body, secret, now, 5*time.Minute, false); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyWebhook(h, []byte(`{"id":1}`), secret, now, 5*time.Minute, false); err == nil {
		t.Fatal("body normalization accepted")
	}
	if _, err := VerifyWebhook(h, body, secret, now.Add(6*time.Minute), 5*time.Minute, false); err == nil {
		t.Fatal("stale delivery accepted")
	}
	if _, err := VerifyWebhook(h, body, secret, now.Add(-6*time.Minute), 5*time.Minute, false); err == nil {
		t.Fatal("future delivery accepted")
	}
	legacy := signedHeaders(body, secret, now, "X-Licenova-")
	if _, err := VerifyWebhook(legacy, body, secret, now, 5*time.Minute, false); err == nil {
		t.Fatal("legacy enabled implicitly")
	}
	if _, err := VerifyWebhook(legacy, body, secret, now, 5*time.Minute, true); err != nil {
		t.Fatal(err)
	}
	h.Set("X-Licenova-Event", "22222222-2222-4222-8222-222222222222")
	if _, err := VerifyWebhook(h, body, secret, now, 5*time.Minute, true); err == nil {
		t.Fatal("conflicting headers accepted")
	}
}
