package util

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

type Client struct {
	http    *http.Client
	retries int
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		retries: 3,
	}
}

func (c *Client) SetTimeout(d time.Duration) {
	c.http.Timeout = d
}

func (c *Client) SetCookieJar(jar http.CookieJar) {
	c.http.Jar = jar
}

func RandomUA() string {
	return userAgents[rand.Intn(len(userAgents))]
}

func (c *Client) Get(url string, headers map[string]string) (*http.Response, error) {
	return c.do("GET", url, nil, headers)
}

func (c *Client) GetBytes(url string, headers map[string]string) ([]byte, error) {
	resp, err := c.Get(url, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
	var lastErr error
	for i := 0; i <= c.retries; i++ {
		req, err := http.NewRequest(method, url, body)
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
