package keqq

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func TestExtractMock(t *testing.T) {
	fixture := loadSampleFixture(t)
	assertValidJSONFixture(t, fixture)
	var compact bytes.Buffer
	if err := json.Compact(&compact, fixture); err != nil {
		t.Fatalf("compact sample fixture: %v", err)
	}

	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "ke.qq.com" && strings.HasPrefix(r.URL.Path, "/course/123456"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, `<!doctype html><html><head><title>腾讯课堂示例课程</title></head><body><script id="__NEXT_DATA__" type="application/json">%s</script></body></html>`, compact.String())
		case r.Host == "ke.qq.com" && strings.HasPrefix(r.URL.Path, "/cgi-bin/course/get_terms_detail"):
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{"result": map[string]any{"terms": []any{}}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Errorf("unexpected mock request: host=%s path=%s rawQuery=%s", r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	info, err := (&Keqq{}).Extract("https://ke.qq.com/course/123456#term_id=987654", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if info == nil {
		t.Fatal("Extract returned nil MediaInfo")
	}
	if info.Site != "keqq" {
		t.Fatalf("Site = %q, want keqq", info.Site)
	}
	if len(info.Entries) != 1 {
		t.Fatalf("expected one file entry, got %#v", info.Entries)
	}
	stream := info.Entries[0].Streams["best"]
	if stream.Format != "pdf" || len(stream.URLs) != 1 || !strings.Contains(stream.URLs[0], "/cgi-bin/file/download") {
		t.Fatalf("unexpected file stream: %#v", stream)
	}
}

func loadSampleFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sample.json"))
	if err != nil {
		t.Fatalf("read sample fixture: %v", err)
	}
	return data
}

func assertValidJSONFixture(t *testing.T, data []byte) {
	t.Helper()
	if !json.Valid(data) {
		t.Fatalf("sample fixture is not valid JSON: %s", data)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("write mock JSON: %v", err)
	}
}

func installMockHTTPSTransport(t *testing.T, handler http.Handler) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	old := http.DefaultTransport
	base, ok := old.(*http.Transport)
	if !ok {
		srv.Close()
		t.Fatalf("http.DefaultTransport has type %T, want *http.Transport", old)
	}
	transport := base.Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, srv.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = old
		srv.Close()
	})
}
