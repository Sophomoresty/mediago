package wallstreets

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

func firstStream(mi *extractor.MediaInfo) (extractor.Stream, *extractor.MediaInfo, bool) {
	if mi == nil {
		return extractor.Stream{}, nil, false
	}
	for _, stream := range mi.Streams {
		if len(stream.URLs) > 0 && strings.TrimSpace(stream.URLs[0]) != "" {
			return stream, mi, true
		}
	}
	for _, entry := range mi.Entries {
		if stream, owner, ok := firstStream(entry); ok {
			return stream, owner, true
		}
	}
	return extractor.Stream{}, nil, false
}

func TestExtractMock(t *testing.T) {
	fixtures := loadFixtures(t)
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/":
			writeFixture(t, w, fixtures, "home")
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/api/me/courses":
			writeFixture(t, w, fixtures, "course_list")
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && (r.URL.Path == "/my/classrooms" || r.URL.Path == "/esbar/my/classroom"):
			writeFixture(t, w, fixtures, "classrooms")
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/my/course/1001":
			writeFixture(t, w, fixtures, "course_page")
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/course/1001/task/list/render/default":
			writeFixture(t, w, fixtures, "task_list")
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/my/course/1001/material":
			writeFixture(t, w, fixtures, "material")
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/course/1001/task/2001/activity_show":
			writeFixture(t, w, fixtures, "activity_show")
		case r.Host == "play.qiqiuyun.net" && r.Method == http.MethodGet && r.URL.Path == "/sdk_api/play":
			writeFixture(t, w, fixtures, "play")
		case r.Host == "cdn.example.com" && r.Method == http.MethodGet && r.URL.Path == "/wallstreets/master.m3u8":
			writeFixture(t, w, fixtures, "master")
		case r.Host == "cdn.example.com" && r.Method == http.MethodGet && r.URL.Path == "/wallstreets/720.m3u8":
			writeFixture(t, w, fixtures, "variant")
		case r.Host == "cdn.example.com" && r.Method == http.MethodGet && r.URL.Path == "/wallstreets/key.bin":
			writeFixture(t, w, fixtures, "key")
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()

	ext, err := extractor.Match("https://wallstreets.cn/my/course/1001")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://wallstreets.cn/my/course/1001", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("nil MediaInfo")
	}
	if info.Site != "Wallstreets" {
		t.Fatalf("site=%q", info.Site)
	}
	stream, entry, ok := firstStream(info)
	if !ok {
		t.Fatal("no stream returned")
	}
	if stream.Format != "m3u8" || !stream.NeedMerge {
		t.Fatalf("stream format/merge = %q/%v, want m3u8/true", stream.Format, stream.NeedMerge)
	}
	if !strings.HasPrefix(stream.URLs[0], "data:application/vnd.apple.mpegurl;base64,") {
		t.Fatalf("m3u8 stream should be a prepared data URL: %#v", stream.URLs)
	}
	if entry.Extra["source_type"] != "m3u8_text" {
		t.Fatalf("source_type=%#v, want m3u8_text", entry.Extra["source_type"])
	}
	m3u8Text, _ := entry.Extra["m3u8_text"].(string)
	if !strings.Contains(m3u8Text, "https://cdn.example.com/wallstreets/seg-1.ts") {
		t.Fatalf("segment URL was not absolutized: %s", m3u8Text)
	}
	if !strings.Contains(m3u8Text, "data:application/octet-stream;base64,NmYwYzgzMDg0NWZkNGJjMQ==") {
		t.Fatalf("key URI was not decoded/inlined: %s", m3u8Text)
	}
	if _, ok := entry.Extra["m3u8_meta"].(map[string]any); !ok {
		t.Fatalf("m3u8_meta missing: %#v", entry.Extra)
	}
}

func TestExtractOnlyPDFSkipsVideoResolution(t *testing.T) {
	var videoHits int32
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/":
			_, _ = w.Write([]byte("<html><body>退出登录</body></html>"))
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/api/me/courses":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && (r.URL.Path == "/my/classrooms" || r.URL.Path == "/esbar/my/classroom"):
			_, _ = w.Write([]byte("<html><body></body></html>"))
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/my/course/1001":
			_, _ = w.Write([]byte("<html><head><title>Wallstreets Mock Course - 华尔街学堂 - mock</title></head><body></body></html>"))
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/course/1001/task/list/render/default":
			_, _ = w.Write([]byte(`{"title":"Wallstreets Mock Lesson","taskId":"2001","type":"video"}`))
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/my/course/1001/material":
			_, _ = w.Write([]byte(`<html><body><a href="/course/1001/material/3001/download">Slide.pdf</a></body></html>`))
		case r.Host == "wallstreets.cn" && r.Method == http.MethodGet && r.URL.Path == "/course/1001/task/2001/activity_show":
			atomic.AddInt32(&videoHits, 1)
			http.NotFound(w, r)
		case r.Host == "play.qiqiuyun.net":
			atomic.AddInt32(&videoHits, 1)
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	ext, err := extractor.Match("https://wallstreets.cn/my/course/1001")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://wallstreets.cn/my/course/1001", &extractor.ExtractOpts{Cookies: newJar(), Quality: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&videoHits); got != 0 {
		t.Fatalf("only-pdf mode should not resolve videos, got %d video requests", got)
	}
	if len(info.Entries) != 1 {
		t.Fatalf("entry count=%d, want only the material entry", len(info.Entries))
	}
	stream := info.Entries[0].Streams["file"]
	if stream.Format != "pdf" || stream.NeedMerge {
		t.Fatalf("file stream format/merge = %q/%v, want pdf/false", stream.Format, stream.NeedMerge)
	}
	got := firstPlayableURL(info)
	want := "https://wallstreets.cn/course/1001/material/3001/download"
	if got != want {
		t.Fatalf("only-pdf URL=%q, want %q", got, want)
	}
}
