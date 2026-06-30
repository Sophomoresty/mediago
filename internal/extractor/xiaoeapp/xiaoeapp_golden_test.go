package xiaoeapp

import (
	"context"
	"crypto/tls"
	"encoding/base64"
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
		case r.Host == "xiaoeapp-server.xiaoeknow.com" && r.Method == http.MethodPost && r.URL.Path == "/app/xe.user.info/1.0.0":
			writeFixture(t, w, fixtures, "user_info")
		case r.Host == "xiaoeapp-server.xiaoeknow.com" && r.Method == http.MethodPost && r.URL.Path == "/app/my.all.course.lists.get/2.0.0":
			writeFixture(t, w, fixtures, "course_list")
		case r.Host == "xiaoeapp-server.xiaoeknow.com" && r.Method == http.MethodPost && r.URL.Path == "/app/goods/xe.goods.detail.get/1.0.3":
			writeFixture(t, w, fixtures, "video_detail")
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()
	setCookies(t, jar, "https://app.xiaoeknow.com/", &http.Cookie{Name: "token", Value: "xiaoe-token"})

	ext, err := extractor.Match("https://app.xiaoeknow.com/p/course/video/v_1001?resource_id=v_1001")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://app.xiaoeknow.com/p/course/video/v_1001?resource_id=v_1001", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("nil MediaInfo")
	}
	if info.Site != "xiaoeapp" {
		t.Fatalf("site=%q", info.Site)
	}
	got := firstPlayableURL(info)
	if !strings.Contains(got, "cdn.example.com/xiaoeapp.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestListOnlyExpandsClassroomChildren(t *testing.T) {
	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case r.Host == "xiaoeapp-server.xiaoeknow.com" && r.Method == http.MethodPost && r.URL.Path == "/app/xe.user.info/1.0.0":
			_, _ = w.Write([]byte(`{"code":0,"data":{"app_user_id":"app-user-1"}}`))
		case r.Host == "xiaoeapp-server.xiaoeknow.com" && r.Method == http.MethodPost && r.URL.Path == "/app/xe.user.h5login/1.0.0":
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"h5-token"}}`))
		case r.Host == "xiaoeapp-server.xiaoeknow.com" && r.Method == http.MethodPost && r.URL.Path == "/app/my.all.course.lists.get/2.0.0":
			body := readJSONBody(t, r)
			if rt, _ := body["resource_type"].(float64); int(rt) == 7 {
				_, _ = w.Write([]byte(`{"code":0,"data":{"total":1,"list":[{"resource_id":"classroom-1","resource_type":7,"title":"Classroom","app_id":"appabc123","c_user_id":"cu1","is_available":1}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":0,"list":[]}}`))
		case r.Host == "appabc123.h5.xiaoeknow.com" && r.Method == http.MethodPost && r.URL.Path == learnColumnAPI:
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":1,"is_finish":1,"list":[{"product_id":"course_1","resource_type":50,"title":"Nested Course","app_id":"appabc123","c_user_id":"cu1","is_available":1}]}}`))
		case r.Host == "appabc123.h5.xiaoeknow.com" && r.Method == http.MethodPost && r.URL.Path == learnTrainAPI:
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":0,"is_finish":1,"list":[]}}`))
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()
	setCookies(t, jar, "https://app.xiaoeknow.com/", &http.Cookie{Name: "token", Value: "xiaoe-token"})
	info, err := (&Xiaoeapp{}).Extract("https://app.xiaoeknow.com/", &extractor.ExtractOpts{Cookies: jar, ListOnly: true})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	found := false
	for _, entry := range info.Entries {
		if entry.Extra["resource_id"] == "course_1" && entry.Extra["resource_type"] == "ecourse" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("nested course_1 not found in list-only entries: %#v", info.Entries)
	}
}

func TestFirstURLInStringSkipsXiaoePageURL(t *testing.T) {
	got := firstURLInString(`{"jump":"https://appabc123.h5.xiaoeknow.com/p/course/video/v_1","media":"https://cdn.example.com/video.mp4"}`)
	if got != "https://cdn.example.com/video.mp4" {
		t.Fatalf("firstURLInString() = %q", got)
	}
}

func TestProtectedLiveURLDecodesPrivateM3U8AndInlinesKey(t *testing.T) {
	privateURL := "https://media.example.com/live/private.m3u8"
	encoded := base64.StdEncoding.EncodeToString([]byte(privateURL))
	encoded = strings.TrimRight(encoded, "=")
	encoded = strings.ReplaceAll(strings.ReplaceAll(encoded, "+", "-"), "/", "_")
	obfuscated := "__ba" + encoded
	keyBytes := []byte("0123456789abcdef")

	installMockTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "xiaoeapp-server.xiaoeknow.com" && r.Method == http.MethodPost && r.URL.Path == "/app/xe.user.h5login/1.0.0":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"h5-token"}}`))
		case r.Host == "appabc123.h5.xiaoeknow.com" && r.Method == http.MethodGet && r.URL.Path == "/_alive/v3/get_lookback_list":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"code":0,"data":{"list":[{"aliveVideoUrlEncrypt":"` + obfuscated + `"}]}}`))
		case r.Host == "media.example.com" && r.Method == http.MethodGet && r.URL.Path == "/live/private.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:1,\n#EXT-X-KEY:METHOD=AES-128,URI=\"/distribute.vod.pri.get/1.0.0?token=abc\"\nseg.ts\n"))
		case r.Host == "media.example.com" && r.Method == http.MethodGet && r.URL.Path == "/distribute.vod.pri.get/1.0.0":
			_, _ = w.Write(keyBytes)
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	got, extra := protectedLiveURL(util.NewClient(), xeSession{token: "xiaoe-token", appID: "appabc123"}, xeItem{id: "l_1", typ: "live", appID: "appabc123"})
	if !strings.HasPrefix(got, "data:application/vnd.apple.mpegurl;base64,") {
		t.Fatalf("protectedLiveURL() = %q, extra=%v", got, extra)
	}
	text, ok := extra["m3u8_text"].(string)
	if !ok {
		t.Fatalf("m3u8_text missing: %#v", extra)
	}
	if !strings.Contains(text, `URI="data:application/octet-stream;base64,`+base64.StdEncoding.EncodeToString(keyBytes)+`"`) {
		t.Fatalf("key was not inlined: %s", text)
	}
	if !strings.Contains(text, "https://media.example.com/live/seg.ts") {
		t.Fatalf("segment was not absolutized: %s", text)
	}
}
