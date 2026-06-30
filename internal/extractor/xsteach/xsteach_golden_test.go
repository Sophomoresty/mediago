package xsteach

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
		allowed := []string{"xsteach", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "xsteach" {
		t.Fatalf("Site = %q, want xsteach", media.Site)
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
	setGoldenCookie(t, jar, "https://www.xsteach.com/", "xsteachID", "mock")

	media, err := (&Xsteach{}).Extract("https://www.xsteach.com/course/detail/1001", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	got := goldenFirstPlayableURL(media)
	if !strings.Contains(got, "https://media.example.com/xsteach/lesson-1.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestOnlyFilesModeSkipsVideoAndVideoAttachments(t *testing.T) {
	var videoPlayCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, "/api/user/my-course/list-v3"):
			_, _ = w.Write([]byte(`{"code":1,"body":{"records":[{"id":"1001","name":"Course","classScheduleId":"3001","lectureType":"video"}]}}`))
		case strings.Contains(r.URL.Path, "/api/common/my-course-combobox"):
			_, _ = w.Write([]byte(`{"code":1,"body":[{"id":"1001","name":"Course","classScheduleId":"3001","lectureType":"video"}]}`))
		case strings.Contains(r.URL.Path, "/api/course/course-detail"):
			_, _ = w.Write([]byte(`{"code":1,"body":{"id":"1001","name":"Course","classScheduleId":"3001","lectureType":"video"}}`))
		case strings.Contains(r.URL.Path, "/api/course/period"):
			_, _ = w.Write([]byte(`{"code":1,"body":{"periods":[{"id":"2001","name":"Lesson","periodStatus":1,"isHasVideo":true,"videoUrl":"https://media.example.com/video.m3u8","resourceUrl":[{"fileName":"handout.pdf","fileUrl":"https://cdn.example.com/handout.pdf","ext":"pdf"},{"fileName":"attached-video.mp4","fileUrl":"https://cdn.example.com/attached-video.mp4","ext":"mp4"}]}]}}`))
		case strings.Contains(r.URL.Path, "/api/period/get-period-list"):
			_, _ = w.Write([]byte(`{"code":1,"body":{}}`))
		case strings.Contains(r.URL.Path, "/api/vod/period/play") || strings.Contains(r.URL.Path, "/api/vod/teach-coach/play") || strings.Contains(r.URL.Path, "/api/live/enter/play") || strings.Contains(r.URL.Path, "/getplayinfo/"):
			videoPlayCalled = true
			_, _ = w.Write([]byte(`{"code":1,"body":{"url":"https://media.example.com/video.m3u8"}}`))
		default:
			_, _ = w.Write([]byte(`{"code":1,"body":{}}`))
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
	setGoldenCookie(t, jar, "https://www.xsteach.com/", "xsteachID", "mock")

	media, err := (&Xsteach{}).Extract("https://www.xsteach.com/course/detail/1001", &extractor.ExtractOpts{Cookies: jar, Quality: "4"})
	if err != nil {
		t.Fatalf("Extract returned error in only-files mode: %v", err)
	}
	if videoPlayCalled {
		t.Fatalf("only-files mode called video playback APIs")
	}
	if media == nil || len(media.Entries) != 1 {
		t.Fatalf("entries = %#v, want only PDF file entry", media)
	}
	got := goldenFirstPlayableURL(media)
	if got != "https://cdn.example.com/handout.pdf" {
		t.Fatalf("only-files URL = %q, want handout PDF", got)
	}
}
