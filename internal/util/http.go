package util

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

var defaultProxy atomic.Value // string

type Client struct {
	http    *http.Client
	retries int
}

func NewClient() *Client {
	client, err := NewClientWithProxy("")
	if err != nil {
		// The package-level proxy is validated by SetDefaultProxy. This fallback
		// keeps callers safe if an external test mutates state unexpectedly.
		return &Client{http: &http.Client{Timeout: 30 * time.Second}, retries: 3}
	}
	return client
}

func NewClientWithProxy(proxy string) (*Client, error) {
	hc, err := NewHTTPClient(30*time.Second, proxy)
	if err != nil {
		return nil, err
	}
	return &Client{http: hc, retries: 3}, nil
}

func NewHTTPClient(timeout time.Duration, proxy string) (*http.Client, error) {
	transport, err := NewProxyTransport(proxy)
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func NewProxyTransport(proxy string) (*http.Transport, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyURL, err := ParseProxyURL(firstNonEmptyProxy(proxy, DefaultProxy()))
	if err != nil {
		return nil, err
	}
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return transport, nil
}

func SetDefaultProxy(proxy string) error {
	proxy = strings.TrimSpace(proxy)
	if _, err := ParseProxyURL(proxy); err != nil {
		return err
	}
	defaultProxy.Store(proxy)
	return nil
}

func DefaultProxy() string {
	if v := defaultProxy.Load(); v != nil {
		return v.(string)
	}
	return ""
}

func ParseProxyURL(proxy string) (*url.URL, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return nil, nil
	}
	if !strings.Contains(proxy, "://") {
		proxy = "http://" + proxy
	}
	parsed, err := url.Parse(proxy)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL %q: %w", proxy, err)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("invalid proxy URL %q: missing host", proxy)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5":
	case "socks5h":
		parsed.Scheme = "socks5"
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (use http, https, socks5, or socks5h)", parsed.Scheme)
	}
	return parsed, nil
}

func (c *Client) SetTimeout(d time.Duration) {
	c.http.Timeout = d
}

func (c *Client) SetCookieJar(jar http.CookieJar) {
	c.http.Jar = jar
}

func (c *Client) SetProxy(proxy string) error {
	transport, err := NewProxyTransport(proxy)
	if err != nil {
		return err
	}
	c.http.Transport = transport
	return nil
}

func RandomUA() string {
	return userAgents[rand.Intn(len(userAgents))]
}

func (c *Client) Get(url string, headers map[string]string) (*http.Response, error) {
	return c.do("GET", url, nil, headers)
}

func (c *Client) GetBytes(u string, headers map[string]string) ([]byte, error) {
	resp, err := c.Get(u, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) GetString(url string, headers map[string]string) (string, error) {
	b, err := c.GetBytes(url, headers)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Client) Post(url string, body io.Reader, headers map[string]string) (*http.Response, error) {
	return c.do("POST", url, body, headers)
}

// PostForm sends an application/x-www-form-urlencoded POST and returns the body
// as a string. This matches the Python source's request_post(), which encodes
// data via urllib.parse.urlencode — used by every DWR/RPC-based site.
func (c *Client) PostForm(u string, data map[string]string, headers map[string]string) (string, error) {
	form := url.Values{}
	for k, v := range data {
		form.Set(k, v)
	}
	h := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	for k, v := range headers {
		h[k] = v
	}
	resp, err := c.Post(u, strings.NewReader(form.Encode()), h)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Client) do(method, url string, body io.Reader, headers map[string]string) (*http.Response, error) {
	// Buffer the body so it can be replayed on retries. An io.Reader is
	// consumed after the first http.NewRequest read, so subsequent retry
	// iterations would send an empty body without this.
	var buf []byte
	if body != nil {
		var err error
		buf, err = io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("failed to buffer request body: %w", err)
		}
	}

	var lastErr error
	for i := 0; i <= c.retries; i++ {
		var bodyReader io.Reader
		if buf != nil {
			bodyReader = bytes.NewReader(buf)
		}
		req, err := http.NewRequest(method, url, bodyReader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", RandomUA())
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("request failed after %d retries: %w", c.retries, lastErr)
}

func firstNonEmptyProxy(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
