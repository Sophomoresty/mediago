package xuetang

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/util"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func loadGoldenFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile("testdata/sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !json.Valid(fixture) {
		t.Fatalf("fixture is not valid JSON: %s", fixture)
	}
	return fixture
}

func installMockTransport(t *testing.T, httpURL, httpsURL string) {
	t.Helper()
	httpTarget, err := url.Parse(httpURL)
	if err != nil {
		t.Fatalf("parse HTTP mock server URL: %v", err)
	}
	httpsTarget, err := url.Parse(httpsURL)
	if err != nil {
		t.Fatalf("parse HTTPS mock server URL: %v", err)
	}
	previous := http.DefaultTransport
	base, ok := previous.(*http.Transport)
	if !ok {
		t.Fatalf("default transport has unexpected type %T", previous)
	}
	tr := base.Clone()
	tr.Proxy = nil
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := &net.Dialer{}
		return d.DialContext(ctx, network, httpTarget.Host)
	}
	tr.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := &tls.Dialer{NetDialer: &net.Dialer{}, Config: &tls.Config{InsecureSkipVerify: true}}
		return d.DialContext(ctx, network, httpsTarget.Host)
	}
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = previous })
}

func containsAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func assertGoldenOutcome(t *testing.T, media *extractor.MediaInfo, err error) {
	t.Helper()
	if err != nil {
		msg := strings.ToLower(err.Error())
		allowed := []string{"xuetang", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "xuetang" {
		t.Fatalf("Site = %q, want xuetang", media.Site)
	}
	if len(media.Streams) == 0 && len(media.Entries) == 0 {
		t.Fatalf("MediaInfo has no streams or entries: %#v", media)
	}
}

func goldenFirstPlayableURL(mi *extractor.MediaInfo) string {
	if mi == nil {
		return ""
	}
	for _, stream := range mi.Streams {
		for _, u := range stream.URLs {
			if strings.TrimSpace(u) != "" {
				return strings.TrimSpace(u)
			}
		}
	}
	for _, entry := range mi.Entries {
		if u := goldenFirstPlayableURL(entry); u != "" {
			return u
		}
	}
	return ""
}

func goldenHasPlayableURL(mi *extractor.MediaInfo, want string) bool {
	if mi == nil {
		return false
	}
	for _, stream := range mi.Streams {
		for _, u := range stream.URLs {
			if strings.TrimSpace(u) == want {
				return true
			}
		}
	}
	for _, entry := range mi.Entries {
		if goldenHasPlayableURL(entry, want) {
			return true
		}
	}
	return false
}

func TestExtractMock(t *testing.T) {
	fixture := loadGoldenFixture(t)
	leafFixture := []byte(`{"data":{"leaf_data":{"name":"Leaf 1","content_info":{"media":{"ccid":"cc1"}}}}}`)
	playFixture := []byte(`{"data":{"sources":{"quality10":["https://media.example.com/xuetang/lesson-1.mp4"],"quality20":["https://media.example.com/xuetang/lesson-1-hd.mp4"]}}}`)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, "/api/v1/lms/learn/leaf_info/"):
			_, _ = w.Write(leafFixture)
			return
		case strings.Contains(r.URL.Path, "/api/v1/lms/service/playurl/"):
			_, _ = w.Write(playFixture)
			return
		}
		_, _ = w.Write(fixture)
	})
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()
	httpsSrv := httptest.NewTLSServer(handler)
	defer httpsSrv.Close()
	installMockTransport(t, httpSrv.URL, httpsSrv.URL)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}

	media, err := (&Xuetang{}).Extract("https://www.xuetangx.com/course/sign123/1001", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	wantURL := "https://media.example.com/xuetang/lesson-1-hd.mp4"
	if !goldenHasPlayableURL(media, wantURL) {
		t.Fatalf("playable URLs do not contain expected fixture URL %q: %#v", wantURL, media)
	}
}

func TestOnlyFilesModeSkipsPlayURLResolution(t *testing.T) {
	var playURLCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, "/api/v1/lms/learn/product/info"):
			_, _ = w.Write([]byte(`{"data":{"classroom_name":"Course"}}`))
		case strings.Contains(r.URL.Path, "/api/v1/lms/learn/course/chapter"):
			_, _ = w.Write([]byte(`{"data":{"content_data":[{"name":"Chapter","section_leaf_list":[{"name":"Lesson","leaf_list":[{"name":"Leaf","leaf_type":2,"id":11}]}]}]}}`))
		case strings.Contains(r.URL.Path, "/api/v1/lms/learn/leaf_info/"):
			_, _ = w.Write([]byte(`{"data":{"leaf_data":{"name":"Leaf","content_info":{"media":{"ccid":"cc1"},"files":[{"name":"handout.pdf","download_url":"https://cdn.example.com/handout.pdf"}]}}}}`))
		case strings.Contains(r.URL.Path, "/api/v1/lms/service/playurl/"):
			playURLCalled = true
			_, _ = w.Write([]byte(`{"data":{"sources":{"quality20":["https://media.example.com/xuetang/lesson-1-hd.mp4"]}}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	})
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()
	httpsSrv := httptest.NewTLSServer(handler)
	defer httpsSrv.Close()
	installMockTransport(t, httpSrv.URL, httpsSrv.URL)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}

	media, err := (&Xuetang{}).Extract("https://www.xuetangx.com/course/sign123/1001", &extractor.ExtractOpts{Cookies: jar, Quality: "3"})
	if err != nil {
		t.Fatalf("Extract returned error in only-files mode: %v", err)
	}
	if playURLCalled {
		t.Fatalf("only-files mode called playurl API")
	}
	if media == nil || len(media.Entries) != 1 {
		t.Fatalf("entries = %#v, want only PDF file entry", media)
	}
	got := goldenFirstPlayableURL(media)
	if got != "https://cdn.example.com/handout.pdf" {
		t.Fatalf("only-files URL = %q, want handout PDF", got)
	}
}

func TestParseURLSourceExamples(t *testing.T) {
	tests := []struct {
		raw        string
		kind       xuetangURLKind
		host       string
		sign       string
		cid        string
		tid        string
		wantOrigin string
	}{
		{
			raw:        "https://www.xuetangx.com/course/xjtu08301000528/12424483?channel=i.area.learn_title",
			kind:       xuetangURLCourse,
			host:       "www.xuetangx.com",
			sign:       "xjtu08301000528",
			cid:        "12424483",
			wantOrigin: "https://www.xuetangx.com",
		},
		{
			raw:        "https://next.xuetangx.com/course/szpt08071002217/26284632?channel=i.area.learn_title",
			kind:       xuetangURLCourse,
			host:       "next.xuetangx.com",
			sign:       "szpt08071002217",
			cid:        "26284632",
			wantOrigin: "https://www.xuetangx.com",
		},
		{
			raw:        "https://next.xuetangx.com/live/live20191205/live20191205001/1480012/1150601",
			kind:       xuetangURLLive,
			host:       "next.xuetangx.com",
			sign:       "live20191205",
			cid:        "1480012",
			tid:        "1150601",
			wantOrigin: "https://www.xuetangx.com",
		},
		{
			raw:        "https://next.xuetangx.com/live/live20200611M001/live20200611M001/4127460/5786325?fromArray=home_live_ad",
			kind:       xuetangURLLive,
			host:       "next.xuetangx.com",
			sign:       "live20200611M001",
			cid:        "4127460",
			tid:        "5786325",
			wantOrigin: "https://www.xuetangx.com",
		},
		{
			raw:        "https://www.xuetangx.com/training/NLP080910033761/16862187",
			kind:       xuetangURLTraining,
			host:       "www.xuetangx.com",
			sign:       "NLP080910033761",
			wantOrigin: "https://www.xuetangx.com",
		},
		{
			raw:        "https://www.cmgemooc.com/course/cmg123/456",
			kind:       xuetangURLCourse,
			host:       "www.cmgemooc.com",
			sign:       "cmg123",
			cid:        "456",
			wantOrigin: "https://www.xuetangx.com",
		},
		{
			raw:        "https://www.gradsmartedu.cn/course/grad123/789",
			kind:       xuetangURLCourse,
			host:       "www.gradsmartedu.cn",
			sign:       "grad123",
			cid:        "789",
			wantOrigin: "https://www.gradsmartedu.cn",
		},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got := parseURL(tt.raw)
			if got.kind != tt.kind || got.host != tt.host || got.sign != tt.sign || got.cid != tt.cid || got.tid != tt.tid {
				t.Fatalf("parseURL() = %#v, want kind=%s host=%s sign=%s cid=%s tid=%s", got, tt.kind, tt.host, tt.sign, tt.cid, tt.tid)
			}
			if origin := xuetangOrigin(got.host); origin != tt.wantOrigin {
				t.Fatalf("xuetangOrigin(%q) = %q, want %q", got.host, origin, tt.wantOrigin)
			}
		})
	}
}

func TestFileExtFallsBackWhenMimeTypeIsMalformed(t *testing.T) {
	if got := fileExt("https://example.com/download"); got != "bin" {
		t.Fatalf("fileExt without extension = %q, want bin", got)
	}
}

func TestFetchCourseChapterPayloadJoinsFreeSKU(t *testing.T) {
	var chapterCalls int
	var sawProductDetail bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, "/api/v1/lms/learn/course/chapter"):
			chapterCalls++
			if chapterCalls == 1 {
				http.Error(w, "locked", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"content_data":[{"name":"Chapter","section_leaf_list":[{"name":"Lesson","leaf_type":2,"id":42}]}]}}`))
		case strings.Contains(r.URL.Path, "/api/v1/lms/product/sku_pay_detail/"):
			if got := r.URL.Query().Get("cid"); got != "1001" {
				t.Fatalf("sku cid = %q, want 1001", got)
			}
			if got := r.URL.Query().Get("sign"); got != "sign123" {
				t.Fatalf("sku sign = %q, want sign123", got)
			}
			_, _ = w.Write([]byte(`{"data":{"product_id":88,"sku_info":[{"sku_id":99,"current_price":0}]}}`))
		case strings.Contains(r.URL.Path, "/api/v1/lms/order/entries_free_sku/88/"):
			if got := r.URL.Query().Get("sid"); got != "99" {
				t.Fatalf("join sid = %q, want 99", got)
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		case strings.Contains(r.URL.Path, "/api/v1/lms/product/get_course_detail/"):
			sawProductDetail = true
			_, _ = w.Write([]byte(`{"data":{}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	})
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()
	httpsSrv := httptest.NewTLSServer(handler)
	defer httpsSrv.Close()
	installMockTransport(t, httpSrv.URL, httpsSrv.URL)

	c := util.NewClient()
	root, err := fetchCourseChapterPayload(c, "https://www.xuetangx.com", map[string]string{}, "sign123", "1001")
	if err != nil {
		t.Fatalf("fetchCourseChapterPayload returned error: %v", err)
	}
	if leaves := extractCourseLeaves(root); len(leaves) != 1 || leaves[0].ID != "42" {
		t.Fatalf("leaves = %#v, want joined chapter leaf 42", leaves)
	}
	if chapterCalls != 2 {
		t.Fatalf("chapterCalls = %d, want first failure plus retry", chapterCalls)
	}
	if sawProductDetail {
		t.Fatalf("product detail fallback was called before free join retry succeeded")
	}
}
