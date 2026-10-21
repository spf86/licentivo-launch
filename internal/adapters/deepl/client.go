package deepl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"licentivo-launch/internal/ports"
)

var ErrRequest = errors.New("deepl request failed")

type Client struct {
	key  string
	base string
	http *http.Client
}

func New(edition, key string) (*Client, error) {
	base := "https://api-free.deepl.com"
	if edition == "pro" {
		base = "https://api.deepl.com"
	} else if edition != "free" {
		return nil, ErrRequest
	}
	return newClient(base, key, &http.Client{Timeout: 25 * time.Second})
}

func newClient(base, key string, client *http.Client) (*Client, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 512 || strings.ContainsAny(key, "\r\n") || client == nil {
		return nil, ErrRequest
	}
	return &Client{key: key, base: strings.TrimRight(base, "/"), http: client}, nil
}

func (c *Client) call(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil || len(raw) > 128<<10 {
			return ErrRequest
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return ErrRequest
	}
	req.Header.Set("Authorization", "DeepL-Auth-Key "+c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return ErrRequest
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 2<<20)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, limited)
		return ErrRequest
	}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(out); err != nil {
		return ErrRequest
	}
	return nil
}

func (c *Client) Usage(ctx context.Context) (ports.TranslationUsage, error) {
	var response struct {
		CharacterCount uint64 `json:"character_count"`
		CharacterLimit uint64 `json:"character_limit"`
	}
	if err := c.call(ctx, http.MethodGet, "/v2/usage", nil, &response); err != nil {
		return ports.TranslationUsage{}, err
	}
	return ports.TranslationUsage{CharacterCount: response.CharacterCount, CharacterLimit: response.CharacterLimit}, nil
}

func (c *Client) Translate(ctx context.Context, source, target string, texts []string) ([]string, error) {
	if source == "" || target == "" || len(texts) == 0 || len(texts) > 50 {
		return nil, ErrRequest
	}
	request := struct {
		Text               []string `json:"text"`
		SourceLanguage     string   `json:"source_lang"`
		TargetLanguage     string   `json:"target_lang"`
		PreserveFormatting bool     `json:"preserve_formatting"`
	}{texts, source, target, true}
	var response struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := c.call(ctx, http.MethodPost, "/v2/translate", request, &response); err != nil || len(response.Translations) != len(texts) {
		return nil, ErrRequest
	}
	translated := make([]string, len(response.Translations))
	for i, item := range response.Translations {
		translated[i] = item.Text
	}
	return translated, nil
}
