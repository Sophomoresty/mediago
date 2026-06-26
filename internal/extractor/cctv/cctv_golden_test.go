package cctv

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
	fixture := readCCTVGoldenFixture(t)
	srv := newCCTVMockTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/getHttpVideoInfo.do" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture["video_info"])
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fixtureString(t, fixture, "page_html")))
	}))

	testURL := srv.URL + "/cctv.com/2026/06/24/VIDEfixture.shtml"
	if _, err := extractor.Match(testURL); err != nil {
		t.Fatalf("extractor pattern should match fixture URL: %v", err)
	}
	info, err := (&CCTV{}).Extract(testURL, nil)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if info.Site != "cctv" {
		t.Fatalf("site = %q, want cctv", info.Site)
	}
	if info.Title != "CCTV Fixture Video" {
		t.Fatalf("title = %q", info.Title)
	}
	if len(info.Streams) == 0 {
		t.Fatalf("expected non-empty streams: %#v", info)
	}
}

func readCCTVGoldenFixture(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("testdata/sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	for _, key := range []string{"page_html", "video_info"} {
		if len(fixture[key]) == 0 {
			t.Fatalf("fixture missing %s", key)
		}
	}
	return fixture
}

func fixtureString(t *testing.T, fixture map[string]json.RawMessage, key string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(fixture[key], &s); err != nil {
		t.Fatalf("fixture %s should be a JSON string: %v", key, err)
	}
	return s
}

func newCCTVMockTLSServer(t *testing.T, h http.Handler) *httptest.Server {
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
