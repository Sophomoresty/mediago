package wangxiao233

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

func TestExtractMock(t *testing.T) {
	fixtures := loadFixtures(t)
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "japi.233.com" && r.Method == http.MethodGet && r.URL.Path == "/ess-ucs-api/doz/members/userInfo":
			writeFixture(t, w, fixtures, "user_info")
		case r.Host == "japi.233.com" && r.Method == http.MethodGet && r.URL.Path == "/ess-study-api/learn/do/get-class-tag":
			writeFixture(t, w, fixtures, "tag")
		case r.Host == "japi.233.com" && r.Method == http.MethodGet && r.URL.Path == "/ess-study-api/learn/do/list-version":
			writeFixture(t, w, fixtures, "version")
		case r.Host == "japi.233.com" && r.Method == http.MethodGet && r.URL.Path == "/ess-study-api/learn/do/list-chapter-by-version-id":
			writeFixture(t, w, fixtures, "chapter")
		case r.Host == "japi.233.com" && r.Method == http.MethodPost && r.URL.Path == "/ess-study-api/datum-api/page-list":
			writeFixture(t, w, fixtures, "datum")
		case r.Host == "japi.233.com" && r.Method == http.MethodPost && r.URL.Path == "/ess-study-api/datum-api/do/page-list":
			writeFixture(t, w, fixtures, "datum")
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()
	setCookies(t, jar, "https://wx.233.com/", &http.Cookie{Name: "clientauthentication", Value: "wx233-token"})

	ext, err := extractor.Match("https://wx.233.com/study?productId=1001&childProductId=1002&teacherId=3001&domain=aq")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://wx.233.com/study?productId=1001&childProductId=1002&teacherId=3001&domain=aq", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("nil MediaInfo")
	}
	if info.Site != "wangxiao233" {
		t.Fatalf("site=%q", info.Site)
	}
	got := firstPlayableURL(info)
	if !strings.Contains(got, "cdn.example.com/wangxiao233.mp3") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestMediaUsesPreparedM3U8DataURL(t *testing.T) {
	raw := "https://cdn.example.com/lesson/index.m3u8"
	text := "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:1,\nhttps://cdn.example.com/seg.ts\n"
	entry := media("Lesson", raw, "m3u8", map[string]any{"m3u8_text": text})
	if entry == nil {
		t.Fatal("nil media")
	}
	stream := entry.Streams["default"]
	if len(stream.URLs) != 1 {
		t.Fatalf("stream URLs=%v", stream.URLs)
	}
	if !strings.HasPrefix(stream.URLs[0], "data:application/vnd.apple.mpegurl;base64,") {
		t.Fatalf("stream URL was not m3u8 data URL: %q", stream.URLs[0])
	}
	if !stream.NeedMerge {
		t.Fatal("m3u8 stream NeedMerge=false")
	}
	if got, _ := entry.Extra["m3u8_url"].(string); got != raw {
		t.Fatalf("m3u8_url=%q, want %q", got, raw)
	}
	if got, _ := entry.Extra["source_type"].(string); got != "m3u8_text" {
		t.Fatalf("source_type=%q, want m3u8_text", got)
	}
}

func TestQualityFromOptsMapsNumericModes(t *testing.T) {
	cases := map[string]string{
		"1":        "fhd",
		"2":        "hd",
		"3":        "sd",
		"4":        "",
		"pdf":      "",
		"only_pdf": "",
		"":         "fhd",
	}
	for q, want := range cases {
		got := qualityFromOpts(&extractor.ExtractOpts{Quality: q})
		if got != want {
			t.Fatalf("qualityFromOpts(%q)=%q, want %q", q, got, want)
		}
	}
}

func TestExtractOnlyPDFSkipsVideos(t *testing.T) {
	var videoHits atomic.Int32
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "japi.233.com" && r.Method == http.MethodGet && r.URL.Path == "/ess-ucs-api/doz/members/userInfo":
			_, _ = w.Write([]byte(`{"code":0,"data":{"userId":"u1"}}`))
		case r.Host == "japi.233.com" && r.Method == http.MethodGet && r.URL.Path == "/ess-study-api/learn/do/get-class-tag":
			_, _ = w.Write([]byte(`{"code":0,"data":{"productId":"1001","currentProductId":"1002","childProductId":"1002","versionProductId":"1002","versionId":"v1","currentTeacherId":"3001","teacherId":"3001","className":"Course","isBuy":1,"isCanLearn":1}}`))
		case r.Host == "japi.233.com" && r.Method == http.MethodGet && r.URL.Path == "/ess-study-api/learn/do/list-chapter-by-version-id":
			_, _ = w.Write([]byte(`{"code":0,"data":{"courseChapterRspList":[{"chapterName":"Chapter","chapterDetailRspList":[{"detailName":"Video Lesson","detailId":"d1","mp3Url":"https://cdn.example.com/video.mp3","lectureId":"lec1","title":"Handout.pdf","fileType":"1","detailLectureSize":"1MB"}]}]}}`))
		case r.Host == "japi.233.com" && r.Method == http.MethodGet && r.URL.Path == "/ess-study-api/learn/do/get-lecture-url":
			_, _ = w.Write([]byte(`{"code":0,"data":"https://cdn.example.com/handout.pdf"}`))
		case r.Host == "japi.233.com" && r.Method == http.MethodGet && (r.URL.Path == "/ess-study-api/user-course/product-info" || r.URL.Path == "/ess-study-api/vkt-course/product"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"price":199}}`))
		case r.Host == "japi.233.com" && r.Method == http.MethodPost && (r.URL.Path == "/ess-study-api/datum-api/page-list" || r.URL.Path == "/ess-study-api/datum-api/do/page-list"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"list":[]}}`))
		case r.Host == "japi.233.com" && (strings.HasPrefix(r.URL.Path, "/ess-bms-api/vod-play") || strings.HasPrefix(r.URL.Path, "/ess-open-api/vod")):
			videoHits.Add(1)
			http.Error(w, "video endpoint must not be requested in only-pdf mode", http.StatusTeapot)
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()
	setCookies(t, jar, "https://wx.233.com/", &http.Cookie{Name: "clientauthentication", Value: "wx233-token"})

	ext, err := extractor.Match("https://wx.233.com/study?productId=1001&childProductId=1002&teacherId=3001&domain=aq")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://wx.233.com/study?productId=1001&childProductId=1002&teacherId=3001&domain=aq", &extractor.ExtractOpts{Cookies: jar, Quality: "4"})
	if err != nil {
		t.Fatal(err)
	}
	if hits := videoHits.Load(); hits != 0 {
		t.Fatalf("video endpoint hits=%d, want 0", hits)
	}
	got := firstPlayableURL(info)
	if !strings.Contains(got, "cdn.example.com/handout.pdf") {
		t.Fatalf("playable URL %q does not contain only-pdf handout", got)
	}
	if strings.Contains(got, "video.mp3") {
		t.Fatalf("only-pdf returned video URL: %q", got)
	}
}
