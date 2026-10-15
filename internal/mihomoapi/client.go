package mihomoapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTimeout  = 5 * time.Second
	DefaultMaxBytes = 256 * 1024
)

type Client struct {
	Controller string
	Secret     string
	HTTPClient *http.Client
}

type RequestOptions struct {
	Method    string         `json:"method"`
	Path      string         `json:"path"`
	Query     map[string]any `json:"query,omitempty"`
	Body      any            `json:"body,omitempty"`
	Timeout   time.Duration  `json:"-"`
	MaxBytes  int64          `json:"max_bytes,omitempty"`
	Config    string         `json:"config,omitempty"`
	Secret    string         `json:"-"`
	Transport http.RoundTripper
}

type Response struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	StatusCode int    `json:"status_code"`
	Truncated  bool   `json:"truncated,omitempty"`
	Bytes      int    `json:"bytes"`
	JSON       any    `json:"json,omitempty"`
	Body       string `json:"body,omitempty"`
}

func New(controller, secret string) (*Client, error) {
	controller = normalizeController(controller)
	if controller == "" {
		return nil, errors.New("mihomo external-controller is required")
	}
	return &Client{Controller: controller, Secret: strings.TrimSpace(secret)}, nil
}

func NewFromConfig(configPath string) (*Client, error) {
	endpoints, err := ReadConfigEndpoints(configPath)
	if err != nil {
		return nil, err
	}
	return New(endpoints.ExternalController, endpoints.Secret)
}

// ReadJSON decodes a complete controller document into a typed destination.
// Unlike Request's tool-output envelope, semantic readback must also work for
// large proxy inventories; unused fields are discarded rather than published.
func (c *Client) ReadJSON(ctx context.Context, path string, target any) error {
	path, err := validateAPIPath(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.requestURL(path, nil), nil)
	if err != nil {
		return err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("mihomo api request failed with status %d", response.StatusCode)
	}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode controller JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("controller response is not one complete JSON document")
	} else if err != io.EOF {
		return fmt.Errorf("complete controller JSON response: %w", err)
	}
	return nil
}

func (c *Client) Request(ctx context.Context, opts RequestOptions) (Response, error) {
	if c == nil {
		return Response{}, errors.New("mihomo api client is nil")
	}
	method, err := normalizeMethod(opts.Method)
	if err != nil {
		return Response{}, err
	}
	path, err := validateAPIPath(opts.Path)
	if err != nil {
		return Response{}, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	body, err := encodeBody(opts.Body)
	if err != nil {
		return Response{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, c.requestURL(path, opts.Query), body)
	if err != nil {
		return Response{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Transport: opts.Transport}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	data, truncated, err := readBounded(resp.Body, maxBytes)
	if err != nil {
		return Response{}, err
	}
	result := Response{Method: method, Path: path, StatusCode: resp.StatusCode, Truncated: truncated, Bytes: len(data)}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 {
		var decoded any
		if err := json.Unmarshal(trimmed, &decoded); err == nil {
			result.JSON = decoded
		} else {
			result.Body = string(trimmed)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("mihomo api request failed with status %d", resp.StatusCode)
	}
	return result, nil
}

func (c *Client) requestURL(path string, query map[string]any) string {
	u := url.URL{Scheme: "http", Host: c.Controller, Path: path}
	q := url.Values{}
	for key, raw := range query {
		if raw == nil {
			continue
		}
		switch value := raw.(type) {
		case []string:
			for _, item := range value {
				q.Add(key, item)
			}
		case []any:
			for _, item := range value {
				q.Add(key, fmt.Sprint(item))
			}
		default:
			q.Set(key, fmt.Sprint(value))
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) openWebSocket(ctx context.Context, path string, query url.Values, errorPrefix string) (net.Conn, *bufio.Reader, error) {
	host := c.Controller
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = conn.Close()
		}
	}()
	key, err := randomWebSocketKey()
	if err != nil {
		return nil, nil, err
	}
	if c.Secret != "" {
		query.Set("token", c.Secret)
	}
	u := url.URL{Path: path, RawQuery: query.Encode()}
	request := "GET " + u.RequestURI() + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		return nil, nil, err
	}
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, nil, err
	}
	status, err := parseHTTPStatus(strings.TrimSpace(statusLine))
	if err != nil {
		return nil, nil, err
	}
	headers := http.Header{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		headerKey, value, ok := strings.Cut(line, ":")
		if ok {
			headers.Add(strings.TrimSpace(headerKey), strings.TrimSpace(value))
		}
	}
	if status != http.StatusSwitchingProtocols {
		return nil, nil, fmt.Errorf("%s websocket handshake failed with status %d", errorPrefix, status)
	}
	if got := headers.Get("Sec-WebSocket-Accept"); got != webSocketAccept(key) {
		return nil, nil, fmt.Errorf("%s websocket handshake returned invalid accept header", errorPrefix)
	}
	closeOnError = false
	return conn, reader, nil
}

func encodeBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	switch value := body.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil, nil
		}
		return strings.NewReader(value), nil
	case []byte:
		if len(value) == 0 {
			return nil, nil
		}
		return bytes.NewReader(value), nil
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(data), nil
	}
}

func readBounded(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	var buf bytes.Buffer
	limit := maxBytes + 1
	if _, err := io.Copy(&buf, io.LimitReader(r, limit)); err != nil {
		return nil, false, err
	}
	data := buf.Bytes()
	if int64(len(data)) > maxBytes {
		return data[:maxBytes], true, nil
	}
	return data, false, nil
}

func normalizeMethod(method string) (string, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return method, nil
	default:
		return "", fmt.Errorf("mihomo api method %q is not supported", method)
	}
}

func validateAPIPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("mihomo api path is required")
	}
	if strings.Contains(path, "://") {
		return "", errors.New("mihomo api path must not be a full URL")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", errors.New("mihomo api path must be an absolute API path")
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("mihomo api path is invalid: %w", err)
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return "", errors.New("mihomo api path must not include scheme or host")
	}
	if parsed.RawQuery != "" {
		return "", errors.New("mihomo api path query must be passed through query")
	}
	return parsed.EscapedPath(), nil
}

func normalizeController(controller string) string {
	controller = strings.TrimSpace(controller)
	controller = strings.TrimPrefix(controller, "http://")
	controller = strings.TrimPrefix(controller, "ws://")
	controller = strings.TrimSuffix(controller, "/")
	host, port, err := net.SplitHostPort(controller)
	if err == nil {
		if host == "" || host == "0.0.0.0" || host == "::" || host == "*" {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, port)
	}
	if strings.HasPrefix(controller, "0.0.0.0:") {
		return "127.0.0.1:" + strings.TrimPrefix(controller, "0.0.0.0:")
	}
	if strings.HasPrefix(controller, "*:") {
		return "127.0.0.1:" + strings.TrimPrefix(controller, "*:")
	}
	return controller
}

type ConfigEndpoints struct {
	ExternalController string
	ExternalUI         string
	Secret             string
}

func ReadConfigEndpoints(path string) (ConfigEndpoints, error) {
	file, err := os.Open(path)
	if err != nil {
		return ConfigEndpoints{}, err
	}
	defer file.Close()
	var endpoints ConfigEndpoints
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		key, value, ok := splitTopLevelYAMLScalar(line)
		if !ok {
			continue
		}
		switch key {
		case "external-controller":
			endpoints.ExternalController = value
		case "external-ui":
			endpoints.ExternalUI = value
		case "secret":
			endpoints.Secret = value
		}
	}
	if err := scanner.Err(); err != nil {
		return ConfigEndpoints{}, err
	}
	if endpoints.ExternalController == "" {
		return ConfigEndpoints{}, fmt.Errorf("mihomo config %q has no external-controller", path)
	}
	return endpoints, nil
}

func splitTopLevelYAMLScalar(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	idx := strings.Index(trimmed, ":")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(trimmed[:idx])
	value := strings.TrimSpace(stripInlineYAMLComment(trimmed[idx+1:]))
	value = strings.Trim(value, `"'`)
	return key, value, true
}

func stripInlineYAMLComment(value string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
				return value[:i]
			}
		}
	}
	return value
}

func randomWebSocketKey() (string, error) {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key[:]), nil
}

func webSocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func readWebSocketFrame(r *bufio.Reader) (byte, []byte, error) {
	header, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	opcode := header & 0x0f
	second, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	masked := second&0x80 != 0
	length := uint64(second & 0x7f)
	switch length {
	case 126:
		var buf [2]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(buf[:]))
	case 127:
		var buf [8]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(buf[:])
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	if length > 8*1024*1024 {
		return 0, nil, fmt.Errorf("websocket frame too large: %d bytes", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

func parseHTTPStatus(line string) (int, error) {
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid HTTP status line %q", line)
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid HTTP status line %q", line)
	}
	return code, nil
}
