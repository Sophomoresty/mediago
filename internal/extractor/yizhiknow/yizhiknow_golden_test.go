package yizhiknow

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
		allowed := []string{"yizhiknow", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "yizhiknow" {
		t.Fatalf("Site = %q, want yizhiknow", media.Site)
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

func setGoldenCookie(t *testing.T, jar http.CookieJar, rawURL, name, value string) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse cookie URL %q: %v", rawURL, err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value}})
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
	setGoldenCookie(t, jar, "https://user.yizhiknow.com/", "Access-Token", "mock-token")
	setGoldenCookie(t, jar, "https://curriculum-api.yizhiknow.com/", "Access-Token", "mock-token")

	media, err := (&Yizhiknow{}).Extract("https://www.yizhiknow.com/course/video/1001?curriculum_id=1001", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	got := goldenFirstPlayableURL(media)
	if !strings.Contains(got, "https://media.example.com/yizhiknow/lesson-1.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestOnlyFilesModeSkipsMediaResourceAPIs(t *testing.T) {
	var mediaAPICalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, listPath):
			_, _ = w.Write([]byte(`{"code":0,"data":{"list":[]}}`))
		case strings.Contains(r.URL.Path, detailPath):
			_, _ = w.Write([]byte(`{"code":0,"data":{"curriculum_detail":{"title":"Course"},"lesson_list":[{"lesson_id":"11","lesson_title":"Lesson","type":"1","media_url":"https://media.example.com/lesson.mp4","study_material":[{"name":"handout.pdf","url":"https://cdn.example.com/handout.pdf"}]}]}}`))
		case strings.Contains(r.URL.Path, statusPath):
			_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
		case strings.Contains(r.URL.Path, lessonResourcePath), strings.Contains(r.URL.Path, liveResourcePath):
			mediaAPICalled = true
			_, _ = w.Write([]byte(`{"code":0,"data":{"url":"https://media.example.com/resource.mp4"}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
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
	setGoldenCookie(t, jar, "https://user.yizhiknow.com/", "Access-Token", "mock-token")
	setGoldenCookie(t, jar, "https://curriculum-api.yizhiknow.com/", "Access-Token", "mock-token")

	media, err := (&Yizhiknow{}).Extract("https://www.yizhiknow.com/course/video/1001?curriculum_id=1001", &extractor.ExtractOpts{Cookies: jar, Quality: "2"})
	if err != nil {
		t.Fatalf("Extract only-files returned error: %v", err)
	}
	if mediaAPICalled {
		t.Fatalf("only-files mode called media resource API")
	}
	if len(media.Entries) != 1 {
		t.Fatalf("entries = %d, want only material entry", len(media.Entries))
	}
	got := goldenFirstPlayableURL(media)
	if got != "https://cdn.example.com/handout.pdf" {
		t.Fatalf("playable URL = %q, want material PDF", got)
	}
}
