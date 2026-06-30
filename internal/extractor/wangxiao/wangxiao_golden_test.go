package wangxiao

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
	"sync/atomic"
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

func TestFirstStreamURLHandlesSparseStreams(t *testing.T) {
	got := firstStreamURL(&extractor.MediaInfo{
		Streams: map[string]extractor.Stream{
			"default": {URLs: nil},
			"file":    {URLs: []string{"", " https://cdn.example.com/file.pdf "}},
		},
	})
	if got != "https://cdn.example.com/file.pdf" {
		t.Fatalf("firstStreamURL=%q", got)
	}
	if got := firstStreamURL(&extractor.MediaInfo{Streams: map[string]extractor.Stream{"default": {URLs: nil}}}); got != "" {
		t.Fatalf("firstStreamURL for empty default=%q", got)
	}
}

func TestExtractMock(t *testing.T) {
	fixtures := loadFixtures(t)
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "k.wangxiao.cn" && r.Method == http.MethodGet && r.URL.Path == "/play":
			writeFixture(t, w, fixtures, "play_page")
		case r.Host == "k.wangxiao.cn" && r.Method == http.MethodGet && r.URL.Path == "/Course/ProductsDirectory":
			_, _ = w.Write([]byte(`<html></html>`))
		case r.Host == "live.wangxiao.cn" && r.Method == http.MethodGet && r.URL.Path == "/LiveActivity/DownHandOut/":
			http.NotFound(w, r)
		case r.Host == "p.bokecc.com" && r.Method == http.MethodGet && r.URL.Path == "/servlet/getvideofile":
			writeFixture(t, w, fixtures, "bokecc")
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()

	ext, err := extractor.Match("https://k.wangxiao.cn/play?activityid=1001&productsid=2001")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://k.wangxiao.cn/play?activityid=1001&productsid=2001", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("nil MediaInfo")
	}
	if info.Site != "wangxiao" {
		t.Fatalf("site=%q", info.Site)
	}
	got := firstPlayableURL(info)
	if !strings.Contains(got, "cdn.example.com/wangxiao.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestExtractM3U8IsPreparedAsDataURL(t *testing.T) {
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "k.wangxiao.cn" && r.Method == http.MethodGet && r.URL.Path == "/play":
			_, _ = w.Write([]byte(`<html><head><title>Wangxiao Mock Course</title></head><body><span class="course-title">Wangxiao Mock Course</span><script>var cc_vid="VID123";var siteid="SITE123";</script></body></html>`))
		case r.Host == "k.wangxiao.cn" && r.Method == http.MethodGet && r.URL.Path == "/Course/ProductsDirectory":
			_, _ = w.Write([]byte(`<html></html>`))
		case r.Host == "live.wangxiao.cn" && r.Method == http.MethodGet && r.URL.Path == "/LiveActivity/DownHandOut/":
			_, _ = w.Write([]byte(``))
		case r.Host == "p.bokecc.com" && r.Method == http.MethodGet && r.URL.Path == "/servlet/getvideofile":
			_, _ = w.Write([]byte(`<video>
<copy><quality>30</quality><playurl>https://cdn.example.com/wangxiao/fhd.mp4</playurl></copy>
<copy><quality>20</quality><playurl>https://cdn.example.com/wangxiao/hd.m3u8</playurl></copy>
<copy><quality>10</quality><playurl>https://cdn.example.com/wangxiao/sd.mp4</playurl></copy>
</video>`))
		case r.Host == "cdn.example.com" && r.Method == http.MethodGet && r.URL.Path == "/wangxiao/hd.m3u8":
			_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-KEY:METHOD=AES-128,URI="key.bin",IV=0x010203
#EXTINF:4,
seg.ts
#EXT-X-ENDLIST
`))
		case r.Host == "cdn.example.com" && r.Method == http.MethodGet && r.URL.Path == "/wangxiao/key.bin":
			_, _ = w.Write([]byte(`abcdefghijklmnopqrst`))
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	ext, err := extractor.Match("https://k.wangxiao.cn/play?activityid=1001&productsid=2001")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://k.wangxiao.cn/play?activityid=1001&productsid=2001", &extractor.ExtractOpts{Cookies: newJar(), Quality: "2"})
	if err != nil {
		t.Fatal(err)
	}
	var entry *extractor.MediaInfo
	for _, candidate := range info.Entries {
		if candidate != nil && candidate.Extra["source_type"] == "m3u8_text" {
			entry = candidate
			break
		}
	}
	if entry == nil {
		t.Fatalf("m3u8_text entry not found: %#v", info.Entries)
	}
	stream := entry.Streams["default"]
	if stream.Format != "m3u8" || !stream.NeedMerge {
		t.Fatalf("stream format/merge = %q/%v, want m3u8/true", stream.Format, stream.NeedMerge)
	}
	if len(stream.URLs) == 0 || !strings.HasPrefix(stream.URLs[0], "data:application/vnd.apple.mpegurl;base64,") {
		t.Fatalf("m3u8 stream should be prepared as data URL: %#v", stream.URLs)
	}
	if entry.Extra["source_type"] != "m3u8_text" {
		t.Fatalf("source_type=%#v, want m3u8_text", entry.Extra["source_type"])
	}
	text, _ := entry.Extra["m3u8_text"].(string)
	if !strings.Contains(text, "https://cdn.example.com/wangxiao/seg.ts") {
		t.Fatalf("segment URL was not absolutized: %s", text)
	}
	if !strings.Contains(text, "data:application/octet-stream;base64,YWJjZGVmZ2hpamtsbW5vcA==") {
		t.Fatalf("key URI was not inlined as data URL: %s", text)
	}
}

func TestExtractOnlyPDFSkipsBokeCCResolution(t *testing.T) {
	var bokeHits int32
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "k.wangxiao.cn" && r.Method == http.MethodGet && r.URL.Path == "/play":
			_, _ = w.Write([]byte(`<html><head><title>Wangxiao Mock Course</title></head><body><span class="course-title">Wangxiao Mock Course</span><script>var cc_vid="VID123";var siteid="SITE123";</script></body></html>`))
		case r.Host == "k.wangxiao.cn" && r.Method == http.MethodGet && r.URL.Path == "/Course/ProductsDirectory":
			_, _ = w.Write([]byte(`<html></html>`))
		case r.Host == "live.wangxiao.cn" && r.Method == http.MethodGet && r.URL.Path == "/LiveActivity/DownHandOut/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"url":"https://cdn.example.com/wangxiao/handout.pdf"}`))
		case r.Host == "p.bokecc.com":
			atomic.AddInt32(&bokeHits, 1)
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	ext, err := extractor.Match("https://k.wangxiao.cn/play?activityid=1001&productsid=2001")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://k.wangxiao.cn/play?activityid=1001&productsid=2001", &extractor.ExtractOpts{Cookies: newJar(), Quality: "4"})
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&bokeHits); got != 0 {
		t.Fatalf("only-pdf mode should not call BokeCC, got %d calls", got)
	}
	if len(info.Entries) != 1 {
		t.Fatalf("entry count=%d, want only handout", len(info.Entries))
	}
	stream := info.Entries[0].Streams["default"]
	if stream.Format != "pdf" || stream.NeedMerge {
		t.Fatalf("file stream format/merge = %q/%v, want pdf/false", stream.Format, stream.NeedMerge)
	}
	if got := firstPlayableURL(info); got != "https://cdn.example.com/wangxiao/handout.pdf" {
		t.Fatalf("only-pdf URL=%q", got)
	}
}
