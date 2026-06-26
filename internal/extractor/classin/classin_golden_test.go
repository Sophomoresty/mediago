package classin

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
	fixture := readClassinGoldenFixture(t)
	srv := newClassinMockTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture["record_get"])
	}))
	_ = srv

	testURL := "https://www.eeo.cn/replay?SID=1001&courseId=2002&activityId=3003&clientClassId=4004"
	if _, err := extractor.Match(testURL); err != nil {
		t.Fatalf("extractor pattern should match fixture URL: %v", err)
	}
	info, err := (&Classin{}).Extract(testURL, nil)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if info.Site != "classin" {
		t.Fatalf("site = %q, want classin", info.Site)
	}
	if len(info.Streams) == 0 && len(info.Entries) == 0 {
		t.Fatalf("expected streams or entries: %#v", info)
	}
}

func readClassinGoldenFixture(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("testdata/sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture map[string]json.RawMessage
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(fixture["record_get"]) == 0 {
		t.Fatalf("fixture missing record_get")
	}
	return fixture
}

func newClassinMockTLSServer(t *testing.T, h http.Handler) *httptest.Server {
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
