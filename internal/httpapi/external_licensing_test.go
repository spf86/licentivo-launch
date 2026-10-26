package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicExternalLicensingDocuments(t *testing.T) {
	handler := Handler(Options{})
	for _, path := range []string{"/api/v1/licensing/documentation", "/api/v1/licensing/protocol"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, recorder.Code)
		}
		if path == "/api/v1/licensing/protocol" {
			var spec struct {
				Paths map[string]any `json:"paths"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &spec) != nil || len(spec.Paths) != 4 {
				t.Fatal("embedded protocol is not the four-operation receiver contract")
			}
		} else if !strings.Contains(recorder.Body.String(), "自定义授权系统接入指南") {
			t.Fatal("embedded integration guide missing")
		}
	}
}
