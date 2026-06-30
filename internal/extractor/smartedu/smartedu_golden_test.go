package smartedu

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type mockTransport struct{}

func installMockTransport(t *testing.T, handler http.Handler) {
	t.Helper()
	plain := httptest.NewServer(handler)
	tlsSrv := httptest.NewTLSServer(handler)

	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("unexpected default transport type")
	}
	oldTransport := baseTransport.Clone()
	oldProxy := util.DefaultProxy()
	if err := util.SetDefaultProxy(""); err != nil {
		t.Fatal(err)
	}

	plainURL, err := url.Parse(plain.URL)
	if err != nil {
		t.Fatal(err)
	}
	tlsURL, err := url.Parse(tlsSrv.URL)
	if err != nil {
		t.Fatal(err)
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := oldTransport.Clone()
	tr.Proxy = nil
	tr.ForceAttemptHTTP2 = false
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, plainURL.Host)
	}
	tr.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		raw, err := dialer.DialContext(ctx, network, tlsURL.Host)
		if err != nil {
			return nil, err
		}
		conn := tls.Client(raw, &tls.Config{InsecureSkipVerify: true})
		if err := conn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		return conn, nil
	}
	http.DefaultTransport = tr

	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
		_ = util.SetDefaultProxy(oldProxy)
		plain.Close()
		tlsSrv.Close()
	})
}

func loadFixtures(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("testdata/sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]json.RawMessage
	if err := json.Unmarshal(b, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func writeFixture(t *testing.T, w http.ResponseWriter, fixtures map[string]json.RawMessage, key string) {
	t.Helper()
	raw, ok := fixtures[key]
	if !ok {
		t.Fatalf("missing fixture %s", key)
	}
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(s))
		return
	}
	_, _ = w.Write(raw)
}

func readJSONBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if strings.TrimSpace(string(b)) == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func pageNo(t *testing.T, r *http.Request) int {
	t.Helper()
	body := readJSONBody(t, r)
	if body == nil {
		return 1
	}
	if page, ok := body["page"].(map[string]any); ok {
		if v, ok := page["page"].(float64); ok && v > 0 {
			return int(v)
		}
	}
	return 1
}

func newJar() http.CookieJar {
	jar, _ := cookiejar.New(nil)
	return jar
}

func setCookies(t *testing.T, jar http.CookieJar, raw string, cookies ...*http.Cookie) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(u, cookies)
}

func firstPlayableURL(mi *extractor.MediaInfo) string {
	if mi == nil {
		return ""
	}
	for _, stream := range mi.Streams {
		if len(stream.URLs) > 0 && strings.TrimSpace(stream.URLs[0]) != "" {
			return stream.URLs[0]
		}
	}
	for _, entry := range mi.Entries {
		if u := firstPlayableURL(entry); u != "" {
			return u
		}
	}
	return ""
}

func findEntryByTitle(mi *extractor.MediaInfo, titlePart string) *extractor.MediaInfo {
	if mi == nil {
		return nil
	}
	if strings.Contains(mi.Title, titlePart) {
		return mi
	}
	for _, entry := range mi.Entries {
		if got := findEntryByTitle(entry, titlePart); got != nil {
			return got
		}
	}
	return nil
}

func TestExtractMock(t *testing.T) {
	fixtures := loadFixtures(t)
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.Host, "ykt.cbern.com.cn") && r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/details/res-1.json"):
			writeFixture(t, w, fixtures, "activity_detail")
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()

	ext, err := extractor.Match("https://basic.smartedu.cn/syncClassroom?activityId=res-1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://basic.smartedu.cn/syncClassroom?activityId=res-1", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("nil MediaInfo")
	}
	if info.Site != "smartedu" {
		t.Fatalf("site=%q", info.Site)
	}
	got := firstPlayableURL(info)
	if !strings.Contains(got, "cdn.example.com/smartedu.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}

	fileEntry := findEntryByTitle(info, "Smartedu Private File")
	if fileEntry == nil {
		t.Fatalf("private file entry not found in %#v", info)
	}
	stream := fileEntry.Streams["default"]
	wantHosts := []string{
		"r1-ndr-private.ykt.cbern.com.cn",
		"r2-ndr-private.ykt.cbern.com.cn",
		"r3-ndr-private.ykt.cbern.com.cn",
	}
	if len(stream.URLs) != len(wantHosts) {
		t.Fatalf("private CDN URLs = %d, want %d: %#v", len(stream.URLs), len(wantHosts), stream.URLs)
	}
	for i, host := range wantHosts {
		if !strings.Contains(stream.URLs[i], host+"/edu_product/esp/assets/private.pdf") {
			t.Fatalf("private CDN URL[%d] = %q, want host %s", i, stream.URLs[i], host)
		}
		if !strings.Contains(stream.URLs[i], "token=abc") {
			t.Fatalf("private CDN URL[%d] lost query: %q", i, stream.URLs[i])
		}
	}
	if stream.Extra["url_mode"] != "mirror" || stream.Extra["cdn_nodes"] != true {
		t.Fatalf("stream mirror extra = %#v", stream.Extra)
	}
}

func TestNormalizeStorageExpandsSmarteduCDNNodes(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		hosts []string
	}{
		{
			name: "private cs_path",
			raw:  "cs_path:${ref-path}/edu_product/esp/assets/private.pdf",
			hosts: []string{
				"r1-ndr-private.ykt.cbern.com.cn",
				"r2-ndr-private.ykt.cbern.com.cn",
				"r3-ndr-private.ykt.cbern.com.cn",
			},
		},
		{
			name: "public r1 url",
			raw:  "https://r1-ndr.ykt.cbern.com.cn/edu_product/esp/assets/public.mp4?x=1",
			hosts: []string{
				"r1-ndr.ykt.cbern.com.cn",
				"r2-ndr.ykt.cbern.com.cn",
				"r3-ndr.ykt.cbern.com.cn",
			},
		},
		{
			name: "oversea r2 url",
			raw:  "https://r2-ndr-oversea.ykt.cbern.com.cn/edu_product/esp/assets/oversea.mp4",
			hosts: []string{
				"r1-ndr-oversea.ykt.cbern.com.cn",
				"r2-ndr-oversea.ykt.cbern.com.cn",
				"r3-ndr-oversea.ykt.cbern.com.cn",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			urls := normalizeStorageCandidates(tc.raw)
			if len(urls) != len(tc.hosts) {
				t.Fatalf("urls = %#v, want %d hosts", urls, len(tc.hosts))
			}
			for i, host := range tc.hosts {
				if !strings.Contains(urls[i], host) {
					t.Fatalf("url[%d] = %q, want host %s", i, urls[i], host)
				}
			}
		})
	}
}

func TestStrPreservesNumericIDsEndingInZero(t *testing.T) {
	cases := map[any]string{
		float64(10):   "10",
		float64(1000): "1000",
		float64(1200): "1200",
		float64(10.5): "10.5",
	}
	for input, want := range cases {
		if got := str(input); got != want {
			t.Fatalf("str(%v) = %q, want %q", input, got, want)
		}
	}
}
