package bilibili

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func TestExtractMock(t *testing.T) {
	fixture := readBilibiliGoldenFixture(t)
	srv := newBilibiliMockTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/web-interface/view":
			_, _ = w.Write(fixture["view"])
		case "/x/player/playurl":
			_, _ = w.Write(fixture["playurl"])
		default:
			http.NotFound(w, r)
		}
	}))
	_ = srv

	ext, err := extractor.Match("https://www.bilibili.com/video/BV1xx411c7mD")
	if err != nil {
		t.Fatalf("extractor pattern should match fixture URL: %v", err)
	}
	info, err := ext.Extract("https://www.bilibili.com/video/BV1xx411c7mD", nil)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if info.Site != "bilibili" {
		t.Fatalf("site = %q, want bilibili", info.Site)
	}
	if info.Title != "Bilibili Fixture Video" {
		t.Fatalf("title = %q", info.Title)
	}
	if len(info.Streams) == 0 {
		t.Fatalf("expected non-empty streams: %#v", info)
	}
}

func readBilibiliGoldenFixture(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("testdata/sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	for _, key := range []string{"view", "playurl"} {
		if len(fixture[key]) == 0 {
			t.Fatalf("fixture missing %s", key)
		}
	}
	return fixture
}

func newBilibiliMockTLSServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse mock URL: %v", err)
	}
	oldTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected default transport type %T", http.DefaultTransport)
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := oldTransport.Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, target.Host)
	}
	tr.Proxy = nil
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	http.DefaultTransport = tr
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
		tr.CloseIdleConnections()
		srv.Close()
	})
	return srv
}
