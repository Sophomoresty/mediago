package xiwang

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
		allowed := []string{"xiwang", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "xiwang" {
		t.Fatalf("Site = %q, want xiwang", media.Site)
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

	media, err := (&Xiwang{}).Extract("https://www.xiwang.com/course/detail/1001?courseId=1001", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	got := goldenFirstPlayableURL(media)
	if !strings.Contains(got, "https://media.example.com/xiwang/lesson-1.m3u8") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestOnlyFilesModeSkipsVideoAndPPTResolution(t *testing.T) {
	var planCalled, playbackCalled, pptCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, "/checkLogin"):
			_, _ = w.Write([]byte(`{"stat":1}`))
		case strings.Contains(r.URL.Path, "/stuCourseList"):
			_, _ = w.Write([]byte(`{"result":{"data":{"learningCourses":[{"courseId":"1001","courseName":"Test Course","stuCouId":"2001","type":"1"}],"endedCourses":[]}}}`))
		case strings.Contains(r.URL.Path, "/planListV2"):
			planCalled = true
			_, _ = w.Write([]byte(`{"result":{"data":{"list":[{"planId":"3001","planName":"Lesson 1","bizId":"3"}]}}}`))
		case strings.Contains(r.URL.Path, "/playback/enter"):
			playbackCalled = true
			_, _ = w.Write([]byte(`{"data":{"configs":{"appId":"demo","liveTypeId":"7","videoUrl":"https://media.example.com/lesson.m3u8"}}}`))
		case strings.Contains(r.URL.Path, "/note/getTeacherNoteListV2"):
			pptCalled = true
			_, _ = w.Write([]byte(`{"data":{"picData":[{"pic_url":"https://media.example.com/slide.jpg"}]}}`))
		case strings.Contains(r.URL.Path, "/getDatumListByType"):
			_, _ = w.Write([]byte(`{"result":{"data":[{"name":"资料","files":[{"name":"handout.pdf","url":"https://cdn.example.com/handout.pdf"}]}]}}`))
		case strings.Contains(r.URL.Path, "/mall/detail/1/"):
			_, _ = w.Write([]byte(`{"data":{"priceModule":{"price":9900}}}`))
		default:
			_, _ = w.Write([]byte(`{"stat":1,"data":{}}`))
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
	media, err := (&Xiwang{}).Extract("https://www.xiwang.com/course/detail/1001?courseId=1001", &extractor.ExtractOpts{Cookies: jar, Quality: "2"})
	if err != nil {
		t.Fatalf("Extract only-files mode: %v", err)
	}
	if planCalled || playbackCalled || pptCalled {
		t.Fatalf("only-files mode called video/PPT APIs: plan=%v playback=%v ppt=%v", planCalled, playbackCalled, pptCalled)
	}
	if media == nil || len(media.Entries) != 1 {
		t.Fatalf("entries = %#v, want exactly one courseware entry", media)
	}
	got := goldenFirstPlayableURL(media)
	if got != "https://cdn.example.com/handout.pdf" {
		t.Fatalf("only-files URL = %q, want handout PDF", got)
	}
	stream := media.Entries[0].Streams["default"]
	if stream.Format != "pdf" {
		t.Fatalf("courseware format = %q, want pdf", stream.Format)
	}
}
