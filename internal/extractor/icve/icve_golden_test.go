package icve

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
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

	installMockHTTPSTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Host == "ai.icve.com.cn" && strings.Contains(r.URL.Path, "/course/courseInfo/getLatestInfoByCourseId"):
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": "info-1", "courseName": "ICVE示例课程", "schoolName": "示例学院"}})
		case r.Host == "ai.icve.com.cn" && strings.Contains(r.URL.Path, "/course/courseDesign/getDesignList"):
			_, _ = w.Write(fixture)
		case r.Host == "ai.icve.com.cn" && strings.Contains(r.URL.Path, "/course/courseDesign/getCellList"):
			writeJSON(t, w, map[string]any{"data": []any{}})
		default:
			http.Error(w, "unexpected mock request", http.StatusNotFound)
			t.Errorf("unexpected mock request: host=%s path=%s rawQuery=%s", r.Host, r.URL.Path, r.URL.RawQuery)
		}
	}))

	info, err := (&Icve{}).Extract("https://ai.icve.com.cn/course/detail/icve-demo", &extractor.ExtractOpts{Quality: "hd"})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if info == nil {
		t.Fatal("Extract returned nil MediaInfo")
	}
	if info.Site != "icve" {
		t.Fatalf("Site = %q, want icve", info.Site)
	}
	if len(info.Streams) == 0 {
		t.Fatalf("expected stream entry, got %#v", info)
	}
	stream := info.Streams["mp4"]
	if len(stream.URLs) != 1 || !strings.Contains(stream.URLs[0], "icve-demo.mp4") {
		t.Fatalf("unexpected stream URLs: %#v", stream.URLs)
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
