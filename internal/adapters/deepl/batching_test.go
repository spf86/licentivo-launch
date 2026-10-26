package deepl

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLongDocumentsAreSplitByEncodedBytesWithoutReordering(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", 16000), strings.Repeat("<&深", 3000)} {
		calls := 0
		client, _ := newClient("https://api.example", "secret", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) > 120<<10 {
				t.Fatal("request exceeded body budget", len(raw))
			}
			var in translationRequest
			if json.Unmarshal(raw, &in) != nil || len(in.Text) > 50 || in.TargetLanguage != "EN-US" || !in.PreserveFormatting {
				t.Fatal("malformed batch")
			}
			out := make([]map[string]string, len(in.Text))
			for i, value := range in.Text {
				out[i] = map[string]string{"text": "translated:" + value}
			}
			return jsonResponse(200, map[string]any{"translations": out}), nil
		})})
		texts := make([]string, 10)
		for i := range texts {
			texts[i] = text + strings.Repeat("!", i)
		}
		out, err := client.Translate(context.Background(), "ZH", "EN-US", texts)
		if err != nil || calls < 2 || len(out) != len(texts) {
			t.Fatal("long document batch failed", err, calls)
		}
		for i := range texts {
			if out[i] != "translated:"+texts[i] {
				t.Fatal("translation order changed", i)
			}
		}
	}
}

func TestOversizedSingleItemFailsBeforeAnyBillableRequest(t *testing.T) {
	calls := 0
	client, _ := newClient("https://api.example", "secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })})
	if _, err := client.Translate(context.Background(), "ZH", "EN-US", []string{"small", strings.Repeat("<", 30000)}); err == nil || calls != 0 {
		t.Fatal("partial billable request preceded preflight failure")
	}
}
