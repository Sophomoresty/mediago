package xuelang

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
		allowed := []string{"xuelang", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "xuelang" {
		t.Fatalf("Site = %q, want xuelang", media.Site)
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

	media, err := (&Xuelang{}).Extract("https://www.iyincaishijiao.com/course/1001?course_id=1001", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	got := goldenFirstPlayableURL(media)
	if !strings.Contains(got, "https://media.example.com/xuelang/lesson-1.m3u8") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestOnlyFilesModeSkipsLessonPlaybackAndVideoAttachments(t *testing.T) {
	var lessonPlaybackCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.Contains(r.URL.Path, "/ep/user/profile/"):
			_, _ = w.Write([]byte(`{"status_code":0}`))
		case strings.Contains(r.URL.Path, "/ep/student/learn_data_v2/"):
			_, _ = w.Write([]byte(`{"data":{"student_course":{"data":[{"course_info":{"course_id":"1001","title":"Course"}}]}}}`))
		case strings.Contains(r.URL.Path, "/ep/study_pc/course/lessons/") || strings.Contains(r.URL.Path, "/classroom/playback/") || strings.Contains(r.URL.Host, "vod.bytedanceapi.com"):
			lessonPlaybackCalled = true
			_, _ = w.Write([]byte(`{"data":{"data":[]}}`))
		case strings.Contains(r.URL.Path, "/ep/student/course_resource/"):
			_, _ = w.Write([]byte(`{"data":{"node_list":["doc","video"],"object_map":{"doc":{"obj_type":1,"obj_name":"handout.pdf","token":"doc-token"},"video":{"obj_type":1,"obj_name":"attached.mp4","token":"video-token"}}}}`))
		case strings.Contains(r.URL.Path, "/ep/student/preview_course_resource/"):
			switch r.URL.Query().Get("token") {
			case "doc-token":
				_, _ = w.Write([]byte(`{"data":{"preview_url":"https://cdn.example.com/handout.pdf","file_ext":"pdf"}}`))
			case "video-token":
				_, _ = w.Write([]byte(`{"data":{"preview_url":"https://cdn.example.com/attached.mp4","file_ext":"mp4"}}`))
			default:
				_, _ = w.Write([]byte(`{"data":{}}`))
			}
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

	media, err := (&Xuelang{}).Extract("https://www.iyincaishijiao.com/course/1001?course_id=1001", &extractor.ExtractOpts{Cookies: jar, Quality: "4"})
	if err != nil {
		t.Fatalf("Extract returned error in only-files mode: %v", err)
	}
	if lessonPlaybackCalled {
		t.Fatalf("only-files mode called lesson/playback APIs")
	}
	if media == nil || len(media.Entries) != 1 {
		t.Fatalf("entries = %#v, want only PDF file entry", media)
	}
	got := goldenFirstPlayableURL(media)
	if got != "https://cdn.example.com/handout.pdf" {
		t.Fatalf("only-files URL = %q, want handout PDF", got)
	}
}
