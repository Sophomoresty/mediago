package douyin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func TestExtractMock(t *testing.T) {
	fixture := mustReadFixture(t, "testdata/sample.json")
	html := []byte("<!doctype html><script>window._ROUTER_DATA = " + string(fixture) + ";</script>")

	shareServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Host, "v.douyin.com") {
			t.Errorf("share request host = %q, want v.douyin.com", r.Host)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	}))
	defer shareServer.Close()

	apiServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.Host, "ttwid.bytedance.com"):
			http.SetCookie(w, &http.Cookie{Name: "ttwid", Value: "mock-ttwid", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case strings.Contains(r.Host, "aweme.snssdk.com"):
			if r.Header.Get("Range") == "" {
				t.Errorf("play request missing Range header")
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", "bytes 0-1/12345")
			w.Header().Set("Content-Length", "2")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("OK"))
		default:
			t.Errorf("unexpected TLS request host=%q path=%q", r.Host, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()

	restoreTransport := redirectDefaultTransport(t, shareServer.Listener.Addr().String(), apiServer.Listener.Addr().String())
	defer restoreTransport()

	testURL := "http://v.douyin.com/mock/"
	if _, err := extractor.Match(testURL); err != nil {
		t.Fatalf("test URL should match extractor pattern: %v", err)
	}

	info, err := (&Douyin{}).Extract(testURL, &extractor.ExtractOpts{})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if info.Site != "douyin" {
		t.Fatalf("site = %q, want douyin", info.Site)
	}
	if info.Title != "Douyin Sample" {
		t.Fatalf("title = %q, want Douyin Sample", info.Title)
	}
	if info.Artist != "Douyin Tester" {
		t.Fatalf("artist = %q, want Douyin Tester", info.Artist)
	}
	stream, ok := info.Streams["original"]
	if !ok {
		t.Fatalf("original stream missing: %#v", info.Streams)
	}
	if len(stream.URLs) != 1 || !strings.Contains(stream.URLs[0], "aweme/v1/play") {
		t.Fatalf("stream URLs = %#v, want aweme play URL", stream.URLs)
	}
	if stream.Size != 12345 {
		t.Fatalf("stream size = %d, want 12345", stream.Size)
	}
}

func redirectDefaultTransport(t *testing.T, httpAddr, httpsAddr string) func() {
	t.Helper()
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport has type %T, want *http.Transport", http.DefaultTransport)
	}
	transport.CloseIdleConnections()
	origDial := transport.DialContext
	origProxy := transport.Proxy
	origTLS := transport.TLSClientConfig
	dialer := &net.Dialer{}
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err == nil && port == "443" {
			return dialer.DialContext(ctx, network, httpsAddr)
		}
		return dialer.DialContext(ctx, network, httpAddr)
	}
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return func() {
		transport.CloseIdleConnections()
		transport.DialContext = origDial
		transport.Proxy = origProxy
		transport.TLSClientConfig = origTLS
		transport.CloseIdleConnections()
	}
}

func mustReadFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !json.Valid(b) {
		t.Fatalf("fixture %s is not valid json", path)
	}
	return b
}
