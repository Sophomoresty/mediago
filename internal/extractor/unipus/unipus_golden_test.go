package unipus

import (
	"context"
	"crypto/tls"
	"encoding/hex"
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
		case r.Host == "moocs.unipus.cn" && r.Method == http.MethodGet && r.URL.Path == "/course/1001":
			writeFixture(t, w, fixtures, "course_page")
		case r.Host == "moocs.unipus.cn" && r.URL.Path == "/course/1001/buy":
			writeFixture(t, w, fixtures, "join")
		case r.Host == "moocs.unipus.cn" && r.Method == http.MethodGet && r.URL.Path == "/course/1001/tasks":
			writeFixture(t, w, fixtures, "task_list")
		case r.Host == "moocs.unipus.cn" && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/course/1001/task/2001/"):
			writeFixture(t, w, fixtures, "task_content")
		default:
			t.Errorf("unexpected request: %s %s%s", r.Method, r.Host, r.URL.String())
			http.NotFound(w, r)
		}
	}))

	jar := newJar()

	ext, err := extractor.Match("https://moocs.unipus.cn/course/1001")
	if err != nil {
		t.Fatal(err)
	}
	info, err := ext.Extract("https://moocs.unipus.cn/course/1001", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("nil MediaInfo")
	}
	if info.Site != "Unipus" {
		t.Fatalf("site=%q", info.Site)
	}
	got := firstPlayableURL(info)
	if !strings.Contains(got, "cdn.example.com/unipus.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestParseCIDVariants(t *testing.T) {
	cases := map[string]string{
		"https://moocs.unipus.cn/courses/1002":                 "1002",
		"https://moocs.unipus.cn/course/1003":                  "1003",
		"https://moocs.unipus.cn/#/course/1004?from=learning":  "1004",
		"https://moocs.unipus.cn/?courseId=1005&from=learning": "1005",
		"https://moocs.unipus.cn/?cid=1006":                    "1006",
	}
	for raw, want := range cases {
		if got := parseCID(raw); got != want {
			t.Fatalf("parseCID(%q)=%q, want %q", raw, got, want)
		}
	}
}

func TestTaskPreviewURLFallsBackToDataURL(t *testing.T) {
	fragment := `<li class="task-item videoclass" id="task_id_2001"><a class="title" href="javascript:;" data-url="/course/1001/task/2001/content/preview">视频课时 Lesson</a></li>`
	got := taskPreviewURL(fragment, "1001", "2001")
	want := "https://moocs.unipus.cn/course/1001/task/2001/content/preview"
	if got != want {
		t.Fatalf("taskPreviewURL=%q, want %q", got, want)
	}
}

func TestDecodeQiqiuyunKeyVectors(t *testing.T) {
	cases := []struct {
		version int
		in      string
		wantHex string
	}{
		{version: 1, in: "6f0c83bc105fd4430843", wantHex: "36663063383362633135666434333034"},
		{version: 1, in: "abcdefghijklmnopq", wantHex: "6a6b64656667686962636c6d6e6f7071"},
		{version: 2, in: "abcdefghijklmnopqrst", wantHex: "61626300666768006b6c6d6e00717200"},
		{version: 3, in: "abcdefghijklmnopqrst", wantHex: "6162636465006869006c6d0070710074"},
		{version: 4, in: "abcdefghijklmnopqrst0", wantHex: "656e746f616471736a636c676962666d"},
		{version: 10, in: "abcdefghijklmnopqrst0", wantHex: "6a6b6474626d6765736f717066637269"},
	}
	for _, tc := range cases {
		if got := hex.EncodeToString(decodeQiqiuyunKey([]byte(tc.in), tc.version)); got != tc.wantHex {
			t.Fatalf("decodeQiqiuyunKey(%q,%d)=%q, want %q", tc.in, tc.version, got, tc.wantHex)
		}
	}
}

func TestPrepareQiqiuyunM3U8RewritesVariantKeyAndSegments(t *testing.T) {
	oldProxy := util.DefaultProxy()
	if err := util.SetDefaultProxy(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = util.SetDefaultProxy(oldProxy) })

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=900\nhigh/index.m3u8\n"))
		case "/high/index.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\",IV=0x010203\n#EXTINF:4,\nseg.ts\n#EXT-X-ENDLIST\n"))
		case "/high/key.bin":
			_, _ = w.Write([]byte("6f0c83bc105fd4430843"))
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := util.NewClient()
	prepared, meta := prepareQiqiuyunM3U8(c, srv.URL+"/master.m3u8", referer, "")
	if prepared == "" {
		t.Fatal("prepared m3u8 is empty")
	}
	if !strings.Contains(prepared, srv.URL+"/high/seg.ts") {
		t.Fatalf("segment URL was not absolutized: %s", prepared)
	}
	if !strings.Contains(prepared, "data:application/octet-stream;base64,NmYwYzgzYmMxNWZkNDMwNA==") {
		t.Fatalf("key URI was not decoded/inlined: %s", prepared)
	}
	cryptor, ok := meta["cryptor"].(map[string]any)
	if !ok {
		t.Fatalf("cryptor meta missing: %#v", meta)
	}
	segments, ok := cryptor["segments"].(map[int]map[string]any)
	if !ok || len(segments) != 1 {
		t.Fatalf("segment map mismatch: %#v", cryptor["segments"])
	}
	if got := string(segments[0]["key"].([]byte)); got != "6f0c83bc15fd4304" {
		t.Fatalf("decoded key=%q", got)
	}
	iv := segments[0]["iv"].([]byte)
	if len(iv) != 16 || iv[13] != 1 || iv[14] != 2 || iv[15] != 3 {
		t.Fatalf("iv not left-padded: %#v", iv)
	}
}

func TestMakeEntryM3U8NeedMergeAndDataURL(t *testing.T) {
	oldProxy := util.DefaultProxy()
	if err := util.SetDefaultProxy(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = util.SetDefaultProxy(oldProxy) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:1,\nseg.ts\n#EXT-X-ENDLIST\n"))
	}))
	defer srv.Close()

	entry := makeEntry(util.NewClient(), taskItem{TaskID: "2001", TaskName: "Lesson", Kind: "video", PreviewURL: "https://moocs.unipus.cn/course/1001/task/2001/content/preview"}, source{URL: srv.URL + "/plain.m3u8", Title: "Lesson"}, "")
	stream := entry.Streams["best"]
	if stream.Format != "m3u8" || !stream.NeedMerge {
		t.Fatalf("stream format/merge = %q/%v, want m3u8/true", stream.Format, stream.NeedMerge)
	}
	if len(stream.URLs) == 0 || !strings.HasPrefix(stream.URLs[0], "data:application/vnd.apple.mpegurl;base64,") {
		t.Fatalf("m3u8 stream should be prepared as data URL: %#v", stream.URLs)
	}
	if entry.Extra["source_type"] != "m3u8_text" {
		t.Fatalf("source_type=%#v, want m3u8_text", entry.Extra["source_type"])
	}
}
