package zhaozhao

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
		allowed := []string{"zhaozhao", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "zhaozhao" {
		t.Fatalf("Site = %q, want zhaozhao", media.Site)
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

func TestExtractMock(t *testing.T) {
	fixture := loadGoldenFixture(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
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

	media, err := (&Zhaozhao{}).Extract("https://www.yikao88.com/course?productId=1001&courseId=2001", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	got := goldenFirstPlayableURL(media)
	if !strings.Contains(got, "https://media.example.com/zhaozhao/lesson-1.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestExtractOnlyFilesModeSkipsVideosAndKeepsCourseware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, "/api-shop/course/pc/v5/selectDetail"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"courseName":"资料课","courseChapterList":[{"chapterName":"第一章","childVideoList":[{"childName":"第一课","videoId":"abcdef123456","coursewareUrl":"https://media.example.com/zhaozhao/handout.pdf"}]}]}}`))
		case strings.Contains(r.URL.Path, "/api-order/order/pc/v5/myBuyProductList"):
			_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
		case strings.Contains(r.URL.Path, "/api-shop/product/pc/v5/selectPcProductById"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"productName":"资料课"}}`))
		case strings.Contains(r.URL.Path, "/api-shop/course/pc/v5/getPackagelistByProduct"):
			_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
		case strings.Contains(r.URL.Path, "/api-shop/learningPackage/pc/v5/getChildIdToAllZiliaoInfo"):
			_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
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

	media, err := (&Zhaozhao{}).Extract("https://www.yikao88.com/course?productId=1001&courseId=2001", &extractor.ExtractOpts{Cookies: jar, Quality: "2"})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	urls := allPlayableURLs(media)
	if containsURLSubstring(urls, "abcdef123456") {
		t.Fatalf("ONLY_PDF resolved video unexpectedly: %#v", urls)
	}
	if !containsURLSubstring(urls, "handout.pdf") {
		t.Fatalf("ONLY_PDF missing courseware URL: %#v", urls)
	}
}

func TestOnlyFilesModeAliases(t *testing.T) {
	for _, q := range []string{"2", "pdf", "only-pdf", "资料"} {
		if !onlyFilesMode(&extractor.ExtractOpts{Quality: q}) {
			t.Fatalf("onlyFilesMode(%q) = false, want true", q)
		}
	}
	if onlyFilesMode(&extractor.ExtractOpts{Quality: "1"}) {
		t.Fatalf("onlyFilesMode(1) = true, want false")
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

func containsURLSubstring(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
