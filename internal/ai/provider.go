// Package ai implements bounded provider protocols. Models never receive
// credentials, arbitrary HTTP access, SQL, or an executable-code tool.
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Provider struct{ Protocol, Capability, BaseURL, Model, Key string }
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
type Call struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type Message struct {
	Images     []Image `json:"-"`
	Role       string  `json:"role"`
	Content    string  `json:"content"`
	ToolCalls  []Call  `json:"tool_calls,omitempty"`
	ToolCallID string  `json:"tool_call_id,omitempty"`
	Reasoning  string  `json:"reasoning_content,omitempty"`
}
type Image struct {
	MIME string
	Data []byte
}
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) Models(ctx context.Context, p Provider) ([]Model, error) {
	if p.Capability != "chat" {
		return []Model{{ID: p.Model, Name: p.Model}}, nil
	}
	data, err := c.request(ctx, p, "GET", "/models", nil, 2<<20, false)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &out) != nil {
		return nil, errors.New("模型列表格式无效")
	}
	models := []Model{}
	seen := map[string]bool{}
	for _, row := range out.Data {
		if row.ID == "" || len(row.ID) > 150 || strings.ContainsAny(row.ID, "\r\n/\\") || seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		name := row.Name
		if name == "" {
			name = row.DisplayName
		}
		if name == "" {
			name = row.ID
		}
		models = append(models, Model{row.ID, name})
		if len(models) >= 200 {
			break
		}
	}
	if len(models) == 0 {
		return nil, errors.New("服务商没有返回可用模型，请手动填写模型名称")
	}
	return models, nil
}

type Client struct{ HTTP *http.Client }

var callNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

func validateAnswer(m Message) error {
	if len(m.ToolCalls) > 8 || (m.Content == "" && len(m.ToolCalls) == 0) {
		return errors.New("AI 回复为空或工具调用过多")
	}
	seen := map[string]bool{}
	for _, call := range m.ToolCalls {
		if !callNamePattern.MatchString(call.Function.Name) || !remoteIDPattern.MatchString(call.ID) || seen[call.ID] || len(call.Function.Arguments) > 512<<10 {
			return errors.New("AI 工具调用格式无效")
		}
		seen[call.ID] = true
	}
	return nil
}

func publicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}

func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("请填写不含密钥、参数的 HTTPS API 地址")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || !strings.Contains(host, ".") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return errors.New("AI 地址必须使用公网域名")
	}
	if ip, e := netip.ParseAddr(host); e == nil && !publicAddress(ip) {
		return errors.New("AI 地址不能指向内网")
	}
	return nil
}

func NewClient() *Client {
	transport := &http.Transport{ForceAttemptHTTP2: true, MaxIdleConns: 20, MaxIdleConnsPerHost: 4, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 15 * time.Second, ExpectContinueTimeout: time.Second}
	// Resolve and validate at connection time, then pin the chosen IP. A saved
	// public hostname cannot be rebound to localhost or a cloud metadata service.
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("AI DNS unavailable")
		}
		for _, ip := range ips {
			if !publicAddress(ip) {
				return nil, errors.New("private AI address refused")
			}
		}
		var last error
		for _, ip := range ips {
			conn, e := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}
	return &Client{HTTP: &http.Client{Transport: transport, Timeout: 4 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func Validate(p Provider) error {
	if err := ValidateURL(p.BaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(p.Model) == "" || len(p.Model) > 150 || strings.ContainsAny(p.Model, "\r\n/") {
		return errors.New("请填写有效模型名称")
	}
	switch p.Protocol {
	case "openai":
		if p.Capability != "chat" && p.Capability != "image" {
			return errors.New("OpenAI 兼容协议支持聊天或生图")
		}
	case "anthropic":
		if p.Capability != "chat" {
			return errors.New("Claude 协议仅支持聊天")
		}
	case "dashscope":
		if p.Capability != "video" && p.Capability != "image" {
			return errors.New("百炼媒体协议用于图片或视频生成")
		}
	default:
		return errors.New("不支持的 AI 协议")
	}
	return nil
}

func (c *Client) request(ctx context.Context, p Provider, method, path string, body any, limit int64, async bool) ([]byte, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, errors.New("AI 请求格式无效")
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.BaseURL, "/")+path, reader)
	if err != nil {
		return nil, errors.New("AI 请求无效")
	}
	req.Header.Set("Content-Type", "application/json")
	if p.Protocol == "anthropic" {
		req.Header.Set("x-api-key", p.Key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+p.Key)
	}
	if async {
		req.Header.Set("X-DashScope-Async", "enable")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("AI 服务连接失败或超时；未自动重复计费请求")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("AI 服务返回 HTTP %d，请检查模型、密钥和额度", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("AI 响应过大或读取失败")
	}
	return data, nil
}

func (c *Client) Chat(ctx context.Context, p Provider, messages []Message, tools []Tool) (Message, error) {
	if p.Capability != "chat" {
		return Message{}, errors.New("当前模型不支持聊天")
	}
	if p.Protocol == "anthropic" {
		return c.claude(ctx, p, messages, tools)
	}
	wire := []map[string]any{}
	for _, t := range tools {
		wire = append(wire, map[string]any{"type": "function", "function": t})
	}
	conversation := []map[string]any{}
	for _, m := range messages {
		row := map[string]any{"role": m.Role, "content": m.Content}
		if len(m.ToolCalls) > 0 {
			row["tool_calls"] = m.ToolCalls
		}
		if m.ToolCallID != "" {
			row["tool_call_id"] = m.ToolCallID
		}
		if m.Reasoning != "" {
			row["reasoning_content"] = m.Reasoning
		}
		if len(m.Images) > 0 {
			parts := []map[string]any{{"type": "text", "text": m.Content}}
			for _, image := range m.Images {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + image.MIME + ";base64," + base64.StdEncoding.EncodeToString(image.Data)}})
			}
			row["content"] = parts
		}
		conversation = append(conversation, row)
	}
	body := map[string]any{"model": p.Model, "messages": conversation, "stream": false, "max_completion_tokens": 8192}
	// Most compatible endpoints use max_tokens; leave the model's default on
	// those endpoints to avoid sending provider-specific token parameters.
	delete(body, "max_completion_tokens")
	if len(wire) > 0 {
		body["tools"] = wire
		body["tool_choice"] = "auto"
	}
	data, err := c.request(ctx, p, "POST", "/chat/completions", body, 2<<20, false)
	if err != nil {
		return Message{}, err
	}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
			Finish  string  `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &out) != nil || len(out.Choices) == 0 {
		return Message{}, errors.New("AI 未返回有效回复")
	}
	m := out.Choices[0].Message
	m.Role = "assistant"
	if out.Choices[0].Finish == "length" {
		return Message{}, errors.New("AI 输出超出限制，请缩小任务或拆分文件")
	}
	if err := validateAnswer(m); err != nil {
		return Message{}, err
	}
	return m, nil
}

func (c *Client) claude(ctx context.Context, p Provider, messages []Message, tools []Tool) (Message, error) {
	var system string
	wire := []map[string]any{}
	for _, m := range messages {
		if m.Role == "system" {
			system += m.Content + "\n"
			continue
		}
		role := m.Role
		var content any = m.Content
		if len(m.Images) > 0 {
			blocks := []map[string]any{{"type": "text", "text": m.Content}}
			for _, image := range m.Images {
				blocks = append(blocks, map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": image.MIME, "data": base64.StdEncoding.EncodeToString(image.Data)}})
			}
			content = blocks
		}
		if m.Role == "tool" {
			role = "user"
			content = []map[string]any{{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content}}
		}
		if len(m.ToolCalls) > 0 {
			blocks := []map[string]any{}
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, call := range m.ToolCalls {
				var args any
				if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
					return Message{}, errors.New("AI 工具参数无效")
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": args})
			}
			content = blocks
		}
		// Anthropic requires adjacent tool results in one user message.
		if m.Role == "tool" && len(wire) > 0 && wire[len(wire)-1]["role"] == "user" {
			if prev, ok := wire[len(wire)-1]["content"].([]map[string]any); ok {
				wire[len(wire)-1]["content"] = append(prev, content.([]map[string]any)...)
				continue
			}
		}
		wire = append(wire, map[string]any{"role": role, "content": content})
	}
	tw := []map[string]any{}
	for _, t := range tools {
		tw = append(tw, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
	}
	body := map[string]any{"model": p.Model, "system": system, "messages": wire, "max_tokens": 8192}
	if len(tw) > 0 {
		body["tools"] = tw
	}
	data, err := c.request(ctx, p, "POST", "/messages", body, 2<<20, false)
	if err != nil {
		return Message{}, err
	}
	var out struct {
		Stop    string `json:"stop_reason"`
		Content []struct {
			Type, Text, ID, Name string
			Input                json.RawMessage
		} `json:"content"`
	}
	if json.Unmarshal(data, &out) != nil || out.Stop == "max_tokens" {
		return Message{}, errors.New("AI 回复无效或被截断，请拆分任务")
	}
	m := Message{Role: "assistant"}
	for _, b := range out.Content {
		if b.Type == "text" {
			m.Content += b.Text
		}
		if b.Type == "tool_use" {
			call := Call{ID: b.ID, Type: "function"}
			call.Function.Name = b.Name
			call.Function.Arguments = string(b.Input)
			m.ToolCalls = append(m.ToolCalls, call)
		}
	}
	if err := validateAnswer(m); err != nil {
		return Message{}, err
	}
	return m, nil
}

func (c *Client) Image(ctx context.Context, p Provider, prompt, size string) ([]byte, string, error) {
	if p.Capability != "image" {
		return nil, "", errors.New("当前模型未配置图片能力")
	}
	if size == "" {
		size = "1024x1024"
	}
	if size != "1024x1024" && size != "1536x1024" && size != "1024x1536" {
		return nil, "", errors.New("图片尺寸无效")
	}
	if p.Protocol == "dashscope" {
		body := map[string]any{"model": p.Model, "input": map[string]any{"messages": []map[string]any{{"role": "user", "content": []map[string]string{{"text": prompt}}}}}, "parameters": map[string]any{"size": strings.ReplaceAll(size, "x", "*"), "n": 1}}
		data, err := c.request(ctx, p, "POST", "/services/aigc/image-generation/generation", body, 2<<20, false)
		if err != nil {
			return nil, "", err
		}
		var out struct {
			Output struct {
				Choices []struct {
					Message struct {
						Content []struct {
							Image string `json:"image"`
						} `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			} `json:"output"`
		}
		if json.Unmarshal(data, &out) != nil || len(out.Output.Choices) != 1 || len(out.Output.Choices[0].Message.Content) == 0 || out.Output.Choices[0].Message.Content[0].Image == "" {
			return nil, "", errors.New("图片响应无效")
		}
		return nil, out.Output.Choices[0].Message.Content[0].Image, nil
	}
	body := map[string]any{"model": p.Model, "prompt": prompt, "n": 1, "size": size}
	if !strings.HasPrefix(p.Model, "gpt-image") {
		body["response_format"] = "b64_json"
	}
	data, err := c.request(ctx, p, "POST", "/images/generations", body, 32<<20, false)
	if err != nil {
		return nil, "", err
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &out) != nil || len(out.Data) != 1 {
		return nil, "", errors.New("图片响应无效")
	}
	if out.Data[0].B64 != "" {
		raw, e := base64.StdEncoding.DecodeString(out.Data[0].B64)
		if e != nil || len(raw) > 20<<20 {
			return nil, "", errors.New("图片内容无效")
		}
		return raw, "", nil
	}
	return nil, out.Data[0].URL, nil
}

var remoteIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

type Video struct{ ID, Status, URL string }

func (c *Client) Video(ctx context.Context, p Provider, id, prompt, size string, seconds int) (Video, error) {
	if p.Capability != "video" || p.Protocol != "dashscope" {
		return Video{}, errors.New("请配置通义万相视频模型")
	}
	method, path, async := "GET", "/tasks/"+id, false
	var body any
	if id == "" {
		method, path, async = "POST", "/services/aigc/video-generation/video-synthesis", true
		if seconds == 0 {
			seconds = 5
		}
		if seconds < 2 || seconds > 15 {
			return Video{}, errors.New("视频时长须为2至15秒")
		}
		if size == "" {
			size = "1280*720"
		}
		if size != "1280*720" && size != "720*1280" && size != "1920*1080" {
			return Video{}, errors.New("视频尺寸无效")
		}
		body = map[string]any{"model": p.Model, "input": map[string]any{"prompt": prompt}, "parameters": map[string]any{"size": size, "duration": seconds}}
	} else if !remoteIDPattern.MatchString(id) {
		return Video{}, errors.New("视频任务编号无效")
	}
	data, err := c.request(ctx, p, method, path, body, 1<<20, async)
	if err != nil {
		return Video{}, err
	}
	var out struct {
		Output struct {
			ID     string `json:"task_id"`
			Status string `json:"task_status"`
			URL    string `json:"video_url"`
		} `json:"output"`
	}
	if json.Unmarshal(data, &out) != nil || !remoteIDPattern.MatchString(out.Output.ID) {
		return Video{}, errors.New("视频任务响应无效")
	}
	return Video{out.Output.ID, out.Output.Status, out.Output.URL}, nil
}

// FetchMedia never forwards the API key to a provider-supplied download URL.
func (c *Client) FetchMedia(ctx context.Context, raw string, limit int64) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("生成文件地址无效")
	}
	query := u.RawQuery
	u.RawQuery = ""
	if err = ValidateURL(u.String()); err != nil {
		return nil, err
	}
	u.RawQuery = query
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, errors.New("生成文件地址无效")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("生成文件下载失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("生成文件尚不可下载")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(data) == 0 || int64(len(data)) > limit {
		return nil, errors.New("生成文件过大或读取失败")
	}
	return data, nil
}
