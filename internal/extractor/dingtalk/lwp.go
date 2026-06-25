// Package dingtalk contains the LWP (Lightweight Protocol) WebSocket client
// for DingTalk live replay resolution.
//
// LWP is a JSON-over-WebSocket RPC layer. The protocol is:
//   1. Connect to wss://webalfa-cm3.dingtalk.com/long
//   2. Send registration: {"lwp":"/reg","headers":{"app-key":"...","token":"<cookie-token>","ua":"...","mid":"0 0"}}
//   3. Receive registration ack with code 200
//   4. Make RPC calls: {"lwp":"<uri>","headers":{"mid":"<N> 0"},"body":[...]}
//   5. Match response by mid header
//
// Ported from Dingtalk_Live_Client.pyc (LwpClient class, ~170 lines of core logic).
package dingtalk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsURL      = "wss://webalfa-cm3.dingtalk.com/long"
	liveAppKey = "5b46698304b45807569d343fcc5a2b61"
	pcUA       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.5359.125 Safari/537.36 dingtalk-win/1.0.0 nw(0.14.7) DingTalk(7.8.10-Release.250724002) Mojo/1.0.0 Native AppType(release) Channel/201200 Architecture/x86_64"
	referer    = "https://www.dingtalk.com"
)

// lwpMessage is the JSON envelope used in the LWP protocol.
type lwpMessage struct {
	LWP     string            `json:"lwp,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Code    *int              `json:"code,omitempty"`
	Status  *int              `json:"status,omitempty"`
	Reason  string            `json:"reason,omitempty"`
	Message string            `json:"message,omitempty"`
}

func (m *lwpMessage) statusCode() int {
	if m.Code != nil {
		return *m.Code
	}
	if m.Status != nil {
		return *m.Status
	}
	return 0
}

// lwpClient implements the DingTalk LWP WebSocket RPC client.
type lwpClient struct {
	conn       *websocket.Conn
	token      string
	cookie     string
	midCounter int
	mu         sync.Mutex
	timeout    time.Duration
}

// newLwpClient creates a new LWP client from cookie auth material.
func newLwpClient(cookie string) (*lwpClient, error) {
	token := extractTokenFromCookie(cookie)
	if token == "" {
		return nil, fmt.Errorf("missing account/access_token in DingTalk cookie")
	}
	deviceID := extractCookieValue(cookie, "deviceid")
	if deviceID == "" {
		return nil, fmt.Errorf("missing deviceid in DingTalk cookie")
	}
	return &lwpClient{
		token:      token,
		cookie:     cookie,
		midCounter: 5101000,
		timeout:    20 * time.Second,
	}, nil
}

// connect establishes the WebSocket connection and sends the /reg handshake.
func (c *lwpClient) connect() error {
	header := http.Header{}
	if c.cookie != "" {
		header.Set("Cookie", c.cookie)
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: c.timeout,
	}
	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("LWP websocket dial: %w", err)
	}
	c.conn = conn

	// Send /reg message
	regMsg := lwpMessage{
		LWP: "/reg",
		Headers: map[string]string{
			"app-key": liveAppKey,
			"token":   c.token,
			"ua":      pcUA,
			"mid":     "0 0",
		},
	}
	if err := c.sendJSON(regMsg); err != nil {
		c.close()
		return fmt.Errorf("LWP /reg send: %w", err)
	}

	// Wait for registration response
	resp, err := c.recvForMid("0", c.timeout)
	if err != nil {
		c.close()
		return fmt.Errorf("LWP /reg recv: %w", err)
	}
	code := resp.statusCode()
	if code != 0 && code != 200 {
		c.close()
		reason := resp.Reason
		if reason == "" {
			reason = resp.Message
		}
		return fmt.Errorf("LWP /reg failed: code=%d reason=%s", code, reason)
	}
	return nil
}

// call makes an LWP RPC call and waits for the matching response.
func (c *lwpClient) call(uri string, body any, timeout time.Duration) (map[string]any, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("LWP websocket is not connected")
	}
	if timeout == 0 {
		timeout = c.timeout
	}

	c.mu.Lock()
	c.midCounter++
	mid := fmt.Sprintf("%d", c.midCounter)
	c.mu.Unlock()

	msg := map[string]any{
		"lwp": uri,
		"headers": map[string]string{
			"mid": mid + " 0",
		},
	}
	if body != nil {
		msg["body"] = body
	}

	if err := c.sendJSONAny(msg); err != nil {
		return nil, fmt.Errorf("LWP call %s send: %w", uri, err)
	}

	resp, err := c.recvForMidRaw(mid, timeout)
	if err != nil {
		return nil, fmt.Errorf("LWP call %s recv: %w", uri, err)
	}

	// Check status code in response
	code := getIntField(resp, "code")
	if code == 0 {
		code = getIntField(resp, "status")
	}
	if code != 0 && code != 200 {
		reason := getStringField(resp, "reason")
		if reason == "" {
			reason = getStringField(resp, "message")
		}
		if reason == "" {
			reason = getStringField(resp, "msg")
		}
		return nil, fmt.Errorf("LWP call %s failed: code=%d reason=%s", uri, code, reason)
	}
	return resp, nil
}

// close shuts down the WebSocket connection.
func (c *lwpClient) close() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *lwpClient) sendJSON(msg lwpMessage) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, b)
}

func (c *lwpClient) sendJSONAny(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, b)
}

func (c *lwpClient) recvJSON(timeout time.Duration) (map[string]any, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	_, raw, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return map[string]any{"raw": string(raw)}, nil
	}
	return result, nil
}

func (c *lwpClient) recvForMid(mid string, timeout time.Duration) (*lwpMessage, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining < 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(remaining))
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		var msg lwpMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		respMid := normalizeMid(msg.Headers["mid"])
		if respMid == mid {
			return &msg, nil
		}
	}
	return nil, fmt.Errorf("timed out waiting for LWP mid=%s", mid)
}

func (c *lwpClient) recvForMidRaw(mid string, timeout time.Duration) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining < 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		msg, err := c.recvJSON(remaining)
		if err != nil {
			return nil, err
		}
		respMid := extractMessageMid(msg)
		if respMid == mid {
			return msg, nil
		}
	}
	return nil, fmt.Errorf("timed out waiting for LWP mid=%s", mid)
}

// ---------------------------------------------------------------------------
// Cookie / auth helpers
// ---------------------------------------------------------------------------

func extractTokenFromCookie(cookie string) string {
	// Try "account" first, then "access_token"
	if v := extractCookieValue(cookie, "account"); v != "" {
		decoded, err := url.QueryUnescape(v)
		if err == nil && decoded != "" {
			return decoded
		}
		return v
	}
	if v := extractCookieValue(cookie, "access_token"); v != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

func extractCookieValue(cookie, name string) string {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, "=") {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		if strings.TrimSpace(k) == name {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Message field helpers
// ---------------------------------------------------------------------------

func normalizeMid(mid string) string {
	mid = strings.TrimSpace(mid)
	if mid == "" {
		return ""
	}
	parts := strings.Fields(mid)
	return parts[0]
}

func extractMessageMid(msg map[string]any) string {
	if headers, ok := msg["headers"].(map[string]any); ok {
		if mid, ok := headers["mid"].(string); ok {
			return normalizeMid(mid)
		}
	}
	if mid, ok := msg["mid"].(string); ok {
		return normalizeMid(mid)
	}
	return ""
}

func getIntField(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		// Check nested body
		if body, ok := m["body"].(map[string]any); ok {
			v, ok = body[key]
			if !ok {
				return 0
			}
		} else {
			return 0
		}
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case json.Number:
		n, _ := val.Int64()
		return int(n)
	case string:
		var n int
		if _, err := fmt.Sscanf(val, "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

func getStringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	if body, ok := m["body"].(map[string]any); ok {
		if v, ok := body[key].(string); ok {
			return v
		}
	}
	return ""
}
