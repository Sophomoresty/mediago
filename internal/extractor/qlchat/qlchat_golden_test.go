package qlchat

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

func TestExtractMock(t *testing.T) {
	fixtures := loadFixtures(t)
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/member/memberInfo":
			writeFixture(t, w, fixtures, "member_info")
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/transfer/h5/live/purchaseCourse":
			if pageNo(t, r) == 1 {
				writeFixture(t, w, fixtures, "course_list")
			} else {
				writeFixture(t, w, fixtures, "empty_list")
			}
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/transfer/h5/topic/listRecentLearn":
			writeFixture(t, w, fixtures, "learn_list")
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/transfer/h5/interact/getCourseList":
			if pageNo(t, r) == 1 {
				writeFixture(t, w, fixtures, "course_items")
			} else {
				writeFixture(t, w, fixtures, "empty_data_list")
			}
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/topic/getMediaActualUrl":
			writeFixture(t, w, fixtures, "media_url")
		case r.Method == http.MethodGet && r.Host == "m.qlchat.com" && strings.HasPrefix(r.URL.Path, "/api/wechat/topic/media-url"):
			writeFixture(t, w, fixtures, "media_url")
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/channel/initIntro":
			writeFixture(t, w, fixtures, "purchased")
		case r.Method == http.MethodGet && r.Host == "m.qlchat.com" && strings.HasPrefix(r.URL.Path, "/wechat/page/topic-simple-video"):
			writeFixture(t, w, fixtures, "topic_page")
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/topic/getLivePlayUrl":
			writeFixture(t, w, fixtures, "live_url")
		case r.Method == http.MethodGet && r.Host == "m.qlchat.com" && strings.HasPrefix(r.URL.Path, "/api/wechat/transfer/h5/channel/getDiscountType"):
			writeFixture(t, w, fixtures, "price")
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/topic/getTopicSpeak":
			_, _ = w.Write([]byte(`{"data":{"liveSpeakViews":[]}}`))
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/topic/pptList":
			_, _ = w.Write([]byte(`{"data":{"files":[]}}`))
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/transfer/h5/article/get":
			_, _ = w.Write([]byte(`{"data":{"content":""}}`))
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()

	ext, err := extractor.Match("https://m.qlchat.com/wechat/page/channelPage/123.htm?channelId=123")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://m.qlchat.com/wechat/page/channelPage/123.htm?channelId=123", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("nil MediaInfo")
	}
	if info.Site != "qlchat" {
		t.Fatalf("site=%q", info.Site)
	}
	got := firstPlayableURL(info)
	if !strings.Contains(got, "cdn.example.com/qlchat.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestPickFormatRecognizesM3U8Forms(t *testing.T) {
	cases := []string{
		"#EXTM3U\n#EXTINF:1,\nseg.ts\n",
		"data:application/vnd.apple.mpegurl;charset=utf-8,%23EXTM3U",
		"https://cdn.example.com/path/master.m3u8?auth=1",
	}
	for _, raw := range cases {
		if got := pickFormat(raw); got != "m3u8" {
			t.Fatalf("pickFormat(%q) = %q, want m3u8", raw, got)
		}
	}
}

func TestResolveTopicJoinsFreeCourseBeforeH5Retry(t *testing.T) {
	var joined bool
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.Host == "m.qlchat.com" && strings.HasPrefix(r.URL.Path, "/wechat/page/topic-simple-video"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/topic/getMediaActualUrl":
			_, _ = w.Write([]byte(`{"state":{"code":0},"data":{"video":[]}}`))
		case r.Method == http.MethodGet && r.Host == "m.qlchat.com" && strings.HasPrefix(r.URL.Path, "/api/wechat/topic/media-url"):
			if !joined {
				_, _ = w.Write([]byte(`{"state":{"code":403,"msg":"请先报名"},"data":{"video":[]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"state":{"code":0},"data":{"video":[{"height":720,"width":1280,"playUrl":"https://cdn.example.com/free.m3u8?auth=1"}]}}`))
		case r.Method == http.MethodPost && r.Host == "m.qlchat.com" && r.URL.Path == "/api/wechat/transfer/h5/topic/joinFreeCourse":
			joined = true
			_, _ = w.Write([]byte(`{"state":{"code":0}}`))
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	info, err := resolveTopic(util.NewClient(), map[string]string{"Referer": referer}, "topic-1", "free topic")
	if err != nil {
		t.Fatalf("resolveTopic returned error: %v", err)
	}
	stream := info.Streams["best"]
	if !joined {
		t.Fatalf("joinFreeCourse was not called")
	}
	if got := firstPlayableURL(info); !strings.Contains(got, "free.m3u8") {
		t.Fatalf("playable URL = %q, want free.m3u8", got)
	}
	if stream.Format != "m3u8" || !stream.NeedMerge {
		t.Fatalf("stream format/merge = %q/%v, want m3u8/true", stream.Format, stream.NeedMerge)
	}
}
