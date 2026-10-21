package deepl

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, value any) *http.Response {
	raw, _ := json.Marshal(value)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}
}

func TestClientUsesHeaderAndPreservesTextOrder(t *testing.T) {
	clientHTTP := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "DeepL-Auth-Key secret" {
			t.Fatal("missing header authentication")
		}
		switch r.URL.Path {
		case "/v2/usage":
			return jsonResponse(http.StatusOK, map[string]uint64{"character_count": 12, "character_limit": 500000}), nil
		case "/v2/translate":
			var body struct {
				Text               []string `json:"text"`
				SourceLanguage     string   `json:"source_lang"`
				TargetLanguage     string   `json:"target_lang"`
				PreserveFormatting bool     `json:"preserve_formatting"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.SourceLanguage != "ZH" || body.TargetLanguage != "EN-US" || !body.PreserveFormatting || len(body.Text) != 2 {
				t.Fatal("unexpected translation request")
			}
			return jsonResponse(http.StatusOK, map[string]any{"translations": []map[string]string{{"text": "one"}, {"text": "two"}}}), nil
		default:
			return jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}), nil
		}
	})}
	client, err := newClient("https://api.example", "secret", clientHTTP)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := client.Usage(context.Background())
	if err != nil || usage.CharacterCount != 12 || usage.CharacterLimit != 500000 {
		t.Fatalf("usage response mismatch: usage=%+v err=%v", usage, err)
	}
	translated, err := client.Translate(context.Background(), "ZH", "EN-US", []string{"一", "二"})
	if err != nil || len(translated) != 2 || translated[0] != "one" || translated[1] != "two" {
		t.Fatal("translation response mismatch")
	}
}

func TestClientRejectsProviderErrors(t *testing.T) {
	client, _ := newClient("https://api.example", "secret", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusForbidden, map[string]string{"message": "credential details"}), nil
	})})
	if _, err := client.Usage(context.Background()); err == nil {
		t.Fatal("expected provider error")
	}
}
