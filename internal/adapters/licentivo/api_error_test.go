package licentivo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectionErrorsPreserveDiagnosticsWithoutLeakingKey(t *testing.T) {
	key := "lv_api_" + strings.Repeat("a", 43)
	for _, status := range []int{400, 401, 403, 409, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "LAUNCH_HTTPS_REQUIRED", "message": "HTTP not allowed for " + key}, "request_id": "req-http-test"})
			}))
			defer server.Close()
			client, err := NewBridgeClient(server.URL, key, true)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Connections(context.Background())
			var remote *APIError
			if !errors.As(err, &remote) || remote.Status != status || remote.Code != "LAUNCH_HTTPS_REQUIRED" || remote.RequestID != "req-http-test" || strings.Contains(remote.Message, key) || !strings.Contains(remote.Message, "[redacted]") {
				t.Fatalf("lost or unsafe diagnostics: %v", err)
			}
			if (status == 401 || status == 403) && !errors.Is(err, ErrCredentials) {
				t.Fatal("credential error compatibility was lost")
			}
		})
	}
}

func TestMalformedRemoteErrorDoesNotExposeBody(t *testing.T) {
	for _, body := range []string{"<html>private-debug-output</html>", `{"error":{"code":"bad<script>","message":"test\nmessage"},"request_id":"bad<script>"}`} {
		err := decodeAPIError(502, []byte(body), "lv_api_secret")
		if err.Code != "" || err.RequestID != "" || strings.Contains(err.Message, "private-debug-output") || strings.Contains(err.Message, "\n") {
			t.Fatal("invalid response content leaked", err)
		}
	}
}
