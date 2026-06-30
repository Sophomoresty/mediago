package zhihuishu

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
		allowed := []string{"zhihuishu", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "zhihuishu" {
		t.Fatalf("Site = %q, want zhihuishu", media.Site)
	}
	if len(media.Streams) == 0 && len(media.Entries) == 0 {
		t.Fatalf("MediaInfo has no streams or entries: %#v", media)
	}
}

func TestExtractMock(t *testing.T) {
	fixture := loadGoldenFixture(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if strings.Contains(r.URL.Path, "/courseHome/") {
			_, _ = w.Write([]byte(`<html><script>var courseName = "Test Course"; var schoolName = "Test School"; var termId = 100; var recruitId = 999;</script></html>`))
			return
		}
		if strings.Contains(r.URL.Path, "/home/toNewInterestKeep/") {
			http.Redirect(w, r, "https://coursehome.zhihuishu.com/home?recruitAndCourseId=crid-1", http.StatusFound)
			return
		}
		if strings.Contains(r.URL.Path, "/video/changeVideoLine") {
			_, _ = w.Write([]byte(`{"result":"https://media.example.com/zhihuishu/lesson-1.mp4"}`))
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

	media, err := (&Zhihuishu{}).Extract("https://www.zhihuishu.com/courseHome/1001?ft=map", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	assertGoldenOutcome(t, media, err)
	if got := goldenFirstPlayableURL(media); got != "https://media.example.com/zhihuishu/lesson-1.mp4" {
		t.Fatalf("first playable URL = %q, want %q", got, "https://media.example.com/zhihuishu/lesson-1.mp4")
	}
}

func TestOnlyFilesModeSkipsCourseHomeVideoResolution(t *testing.T) {
	var videoAPICalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, "/courseHome/"):
			_, _ = w.Write([]byte(`<html><script>var courseName = "Test Course"; var schoolName = "Test School"; var termId = 100; var recruitId = 999;</script></html>`))
		case strings.Contains(r.URL.Path, "/home/toNewInterestKeep/"):
			http.Redirect(w, r, "https://coursehome.zhihuishu.com/home?recruitAndCourseId=crid-1", http.StatusFound)
		case strings.Contains(r.URL.Path, "/gateway/t/v1/learning/videolist"):
			_, _ = w.Write([]byte(`{"data":{"recruitId":999,"videoChapterDtos":[{"name":"Chapter","videoLessons":[{"name":"Lesson","videoId":12345}]}]}}`))
		case strings.Contains(r.URL.Path, "/video/initVideo"), strings.Contains(r.URL.Path, "/video/changeVideoLine"):
			videoAPICalled = true
			_, _ = w.Write([]byte(`{"result":{"uuid":"uuid-1","lines":[{"lineID":2}]}}`))
		case strings.Contains(r.URL.Path, "/home/resource/queryCourseResourceInfo"):
			_, _ = w.Write([]byte(`[{"dataType":"file","name":"handout.pdf","url":"https://cdn.example.com/handout.pdf","id":1}]`))
		default:
			_, _ = w.Write([]byte(`{"data":[]}`))
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

	media, err := (&Zhihuishu{}).Extract("https://www.zhihuishu.com/courseHome/1001?ft=map", &extractor.ExtractOpts{Cookies: jar, Quality: "3"})
	if err != nil {
		t.Fatalf("Extract only-files returned error: %v", err)
	}
	if videoAPICalled {
		t.Fatalf("only-files mode called video playback API")
	}
	if len(media.Entries) != 1 {
		t.Fatalf("entries = %d, want only resource entry", len(media.Entries))
	}
	if got := goldenFirstPlayableURL(media); got != "https://cdn.example.com/handout.pdf" {
		t.Fatalf("first playable URL = %q, want handout PDF", got)
	}
}

func goldenFirstPlayableURL(media *extractor.MediaInfo) string {
	if media == nil {
		return ""
	}
	for _, stream := range media.Streams {
		if len(stream.URLs) > 0 && strings.TrimSpace(stream.URLs[0]) != "" {
			return strings.TrimSpace(stream.URLs[0])
		}
	}
	for _, entry := range media.Entries {
		if got := goldenFirstPlayableURL(entry); got != "" {
			return got
		}
	}
	return ""
}
