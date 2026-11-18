package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeClient(fn func(*http.Request) string) *Client {
	return &Client{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fn(r)))}, nil
	})}}
}

func TestProviderProtocols(t *testing.T) {
	ctx := context.Background()
	p := Provider{Protocol: "openai", Capability: "chat", BaseURL: "https://api.deepseek.com/v1", Model: "test-model", Key: "private-key"}
	c := fakeClient(func(r *http.Request) string {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer private-key" {
			t.Fatal("incorrect authenticated chat request")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body["tools"].([]any)) != 1 {
			t.Fatal("tools missing")
		}
		return `{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"query_system","arguments":"{\"area\":\"overview\"}"}}]}}]}`
	})
	answer, err := c.Chat(ctx, p, []Message{{Role: "user", Content: "test"}}, []Tool{{Name: "query_system", Description: "test", Parameters: map[string]any{"type": "object"}}})
	if err != nil || len(answer.ToolCalls) != 1 {
		t.Fatal(answer, err)
	}
	p.Protocol = "anthropic"
	p.BaseURL = "https://api.anthropic.com/v1"
	c = fakeClient(func(r *http.Request) string {
		if r.URL.Path != "/v1/messages" || r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "private-key" {
			t.Fatal("Claude auth or path incorrect")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["system"] == nil {
			t.Fatal("missing Claude system")
		}
		return `{"stop_reason":"tool_use","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"t1","name":"query_system","input":{"area":"overview"}}]}`
	})
	answer, err = c.Chat(ctx, p, []Message{{Role: "system", Content: "system"}, {Role: "user", Content: "test"}}, nil)
	if err != nil || answer.Content != "checking" || answer.ToolCalls[0].Function.Name != "query_system" {
		t.Fatal(answer, err)
	}
}

func TestModelDiscoveryAndMultimodalProtocols(t *testing.T) {
	p := Provider{Protocol: "openai", Capability: "chat", BaseURL: "https://api.example.com/v1", Model: "chat", Key: "secret"}
	c := fakeClient(func(r *http.Request) string {
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			t.Fatal("catalog request")
		}
		return `{"data":[{"id":"chat"},{"id":"vision","display_name":"Vision"},{"id":"chat"}]}`
	})
	models, err := c.Models(context.Background(), p)
	if err != nil || len(models) != 2 || models[1].Name != "Vision" {
		t.Fatal(models, err)
	}
	for _, protocol := range []string{"openai", "anthropic"} {
		p.Protocol = protocol
		c = fakeClient(func(r *http.Request) string {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Fatal("wire JSON")
			}
			messages := body["messages"].([]any)
			parts := messages[0].(map[string]any)["content"].([]any)
			if len(parts) != 2 {
				t.Fatal("missing image")
			}
			if protocol == "openai" {
				if parts[1].(map[string]any)["type"] != "image_url" {
					t.Fatal("OpenAI image wire")
				}
				return `{"choices":[{"message":{"role":"assistant","content":"image seen"}}]}`
			}
			if parts[1].(map[string]any)["type"] != "image" {
				t.Fatal("Claude image wire")
			}
			return `{"content":[{"type":"text","text":"image seen"}]}`
		})
		if _, err := c.Chat(context.Background(), p, []Message{{Role: "user", Content: "inspect", Images: []Image{{MIME: "image/png", Data: []byte("png")}}}}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMediaAndSafeErrors(t *testing.T) {
	p := Provider{Protocol: "dashscope", Capability: "video", BaseURL: "https://dashscope.aliyuncs.com/api/v1", Model: "video-model", Key: "secret"}
	c := fakeClient(func(r *http.Request) string {
		if r.Header.Get("X-DashScope-Async") != "enable" {
			t.Fatal("missing async header")
		}
		return `{"output":{"task_id":"remote-1","task_status":"PENDING"}}`
	})
	v, err := c.Video(context.Background(), p, "", "prompt", "", 5)
	if err != nil || v.ID != "remote-1" {
		t.Fatal(v, err)
	}
	c = fakeClient(func(r *http.Request) string {
		if r.Header.Get("Authorization") != "" {
			t.Fatal("credential leaked to media URL")
		}
		return "media"
	})
	if _, err = c.FetchMedia(context.Background(), "https://cdn.example.com/video.mp4?token=abc", 100); err != nil {
		t.Fatal(err)
	}
	c = &Client{HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("secret"))}, nil
	})}}
	_, err = c.Video(context.Background(), p, "", "prompt", "", 5)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("upstream error leaked", err)
	}
}

func TestRejectPrivateOrCredentialedDestinations(t *testing.T) {
	for _, raw := range []string{"http://api.example.com/v1", "https://localhost/v1", "https://127.0.0.1/v1", "https://10.1.2.3/v1", "https://169.254.169.254/v1", "https://100.64.0.1/v1", "https://[::1]/v1", "https://user:pass@example.com/v1", "https://api.example.com/v1?key=secret", "https://internal.local/v1"} {
		if ValidateURL(raw) == nil {
			t.Fatal("unsafe destination accepted", raw)
		}
	}
	if ValidateURL("https://api.example.com/v1") != nil {
		t.Fatal("valid provider rejected")
	}
}
