package youyuan

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
		allowed := []string{"youyuan", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "youyuan" {
		t.Fatalf("Site = %q, want youyuan", media.Site)
	}
	if len(media.Streams) == 0 && len(media.Entries) == 0 {
		t.Fatalf("MediaInfo has no streams or entries: %#v", media)
	}
}

func TestExtractItemsNilReturnsEmpty(t *testing.T) {
	if got := extractItems(nil); len(got) != 0 {
		t.Fatalf("extractItems(nil) len=%d, want 0", len(got))
	}
}

func TestParseCIDHandlesQueryAndFragment(t *testing.T) {
	tests := map[string]string{
		"https://www.yijiayk.com/course?courseId=1001":                         "1001",
		"https://h.yijiayk.com/#/course/detail?courseId=abc-123":               "abc-123",
		"https://m.yijiayk.com/course-api/app/course/getByCourseId?courseId=x": "x",
	}
	for raw, want := range tests {
		if got := parseCID(raw); got != want {
			t.Fatalf("parseCID(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestHeadersFromJarUsesAccessTokenCaseInsensitive(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}
	u, err := url.Parse(refererURL)
	if err != nil {
		t.Fatalf("parse refererURL: %v", err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "accesstoken", Value: "lower-token"}})

	headers := headersFromJar(jar)
	if headers["accessToken"] != "lower-token" {
		t.Fatalf("accessToken header = %q, want lower-token", headers["accessToken"])
	}
	if headers["authorization"] != "lower-token" {
		t.Fatalf("authorization header = %q, want lower-token", headers["authorization"])
	}
}

func TestCollectLessonsPrefersChapterIDForTokenAPI(t *testing.T) {
	root := map[string]any{
		"data": []any{
			map[string]any{
				"chapterName": "第一章",
				"courseLessonList": []any{
					map[string]any{
						"id":         "lesson-row-id",
						"chapterId":  "chapter-token-id",
						"lessonName": "第一课",
					},
				},
			},
		},
	}

	lessons := collectLessons(root)
	if len(lessons) != 1 {
		t.Fatalf("collectLessons len = %d, want 1: %#v", len(lessons), lessons)
	}
	if lessons[0].ID != "chapter-token-id" {
		t.Fatalf("lesson ID = %q, want chapter-token-id", lessons[0].ID)
	}
}

func TestExtractMock(t *testing.T) {
	fixture := loadGoldenFixture(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if strings.Contains(r.URL.Path, "/course-api/app/course/getByCourseId") {
			_, _ = w.Write([]byte(`{"data":{"courseName":"Test Course"}}`))
			return
		}
		if strings.Contains(r.URL.Path, "/course-api/app/courseVideo/getToken") {
			_, _ = w.Write([]byte(`{"data":{"videoId":"bjy-video-1","token":"bjy-token-1"}}`))
			return
		}
		if strings.Contains(r.URL.Path, "/vod/video/getPlayUrl") {
			_, _ = w.Write([]byte(`callback({"code":0,"data":{"video_url":"https://media.example.com/youyuan/lesson-1.mp4"}});`))
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

	media, err := (&Youyuan{}).Extract("https://www.yijiayk.com/course?courseId=1001", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	assertGoldenOutcome(t, media, err)
	if got := goldenFirstPlayableURL(media); got != "https://media.example.com/youyuan/lesson-1.mp4" {
		t.Fatalf("first playable URL = %q, want %q", got, "https://media.example.com/youyuan/lesson-1.mp4")
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
