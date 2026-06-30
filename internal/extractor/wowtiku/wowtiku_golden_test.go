package wowtiku

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

func TestFirstCourseIDRejectsEmptyCourseList(t *testing.T) {
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case r.Host == "new.wowtiku.net" && r.URL.Path == "/goods/buy_lists":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Host == "new.wowtiku.net" && r.URL.Path == "/platform/lists":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))

	got, err := firstCourseID(util.NewClient(), wtSession{token: "token"})
	if err == nil {
		t.Fatalf("firstCourseID error = nil, got course %q", got)
	}
	if !strings.Contains(err.Error(), "purchased course list is empty") {
		t.Fatalf("firstCourseID error = %v, want purchased course list is empty", err)
	}
}

func TestExtractMock(t *testing.T) {
	fixtures := loadFixtures(t)
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "www.wowtiku.net" && r.Method == http.MethodGet && r.URL.Path == "/question_bank/user/user_info":
			writeFixture(t, w, fixtures, "user_info")
		case r.Host == "new.wowtiku.net" && r.Method == http.MethodGet && r.URL.Path == "/goods/sg_detail":
			writeFixture(t, w, fixtures, "detail")
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()
	setCookies(t, jar, "https://www.wowtiku.com/", &http.Cookie{Name: "token", Value: "wowtiku-token"})

	ext, err := extractor.Match("https://www.wowtiku.com/#/course?id=1001")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://www.wowtiku.com/#/course?id=1001", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("nil MediaInfo")
	}
	if info.Site != "wowtiku" {
		t.Fatalf("site=%q", info.Site)
	}
	got := firstPlayableURL(info)
	if !strings.Contains(got, "cdn.example.com/wowtiku.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestExtractOnlyPDFSkipsVideosAndKeepsFiles(t *testing.T) {
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case r.Host == "www.wowtiku.net" && r.Method == http.MethodGet && r.URL.Path == "/question_bank/user/user_info":
			_, _ = w.Write([]byte(`{"code":0,"data":{"user_id":"u-1"}}`))
		case r.Host == "new.wowtiku.net" && r.Method == http.MethodGet && r.URL.Path == "/goods/sg_detail":
			_, _ = w.Write([]byte(`{"code":0,"data":{"name":"Only PDF Course","videos":[{"title":"Should Skip","video_url":"https://cdn.example.com/skip.mp4"}],"docs":[{"name":"Keep Doc","file_url":"https://cdn.example.com/keep.pdf"}]}}`))
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()
	setCookies(t, jar, "https://www.wowtiku.com/", &http.Cookie{Name: "token", Value: "wowtiku-token"})

	info, err := (&Wowtiku{}).Extract("https://www.wowtiku.com/#/course?id=1001", &extractor.ExtractOpts{Cookies: jar, Quality: "4"})
	if err != nil {
		t.Fatal(err)
	}
	urls := allPlayableURLs(info)
	if containsSubstring(urls, "skip.mp4") {
		t.Fatalf("ONLY_PDF resolved video URL unexpectedly: %#v", urls)
	}
	if !containsSubstring(urls, "keep.pdf") {
		t.Fatalf("ONLY_PDF URLs missing courseware: %#v", urls)
	}
}

func TestMediaFormatKeepsDirectExtension(t *testing.T) {
	tests := map[string]string{
		"https://cdn.example.com/audio.mp3": "mp3",
		"https://cdn.example.com/movie.avi": "avi",
		"https://cdn.example.com/live.m3u8": "m3u8",
		"https://cdn.example.com/noext":     "mp4",
	}
	for raw, want := range tests {
		if got := mediaFormat(raw); got != want {
			t.Fatalf("mediaFormat(%q)=%q, want %q", raw, got, want)
		}
	}
}

func allPlayableURLs(mi *extractor.MediaInfo) []string {
	if mi == nil {
		return nil
	}
	var out []string
	for _, stream := range mi.Streams {
		for _, u := range stream.URLs {
			if strings.TrimSpace(u) != "" {
				out = append(out, strings.TrimSpace(u))
			}
		}
	}
	for _, entry := range mi.Entries {
		out = append(out, allPlayableURLs(entry)...)
	}
	return out
}

func containsSubstring(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
