package youzan

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

func containsAnyNeedle(s string, needles []string) bool {
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
		allowed := []string{"youzan", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAnyNeedle(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "youzan" {
		t.Fatalf("Site = %q, want youzan", media.Site)
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

	media, err := (&Youzan{}).Extract("https://shop.youzan.com/wscvis/course/detail/demo-alias?alias=demo-alias&kdt_id=1001", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	got := goldenFirstPlayableURL(media)
	if !strings.Contains(got, "https://media.example.com/youzan/lesson-1.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestHRefUAUsesUserAgentHeader(t *testing.T) {
	ctx := &yzContext{headers: map[string]string{"referer": "https://shop.youzan.com"}}
	headers := ctx.hRefUA("https://shop.youzan.com/detail", wechatMobileUA)

	if headers["User-Agent"] != wechatMobileUA {
		t.Fatalf("User-Agent header = %q, want wechat UA", headers["User-Agent"])
	}
	if _, ok := headers["User-ToolSearch"]; ok {
		t.Fatalf("unexpected User-ToolSearch header present: %#v", headers)
	}
}

func TestExtractMediaURLsIncludesQueryEncodedMedia(t *testing.T) {
	payload := map[string]any{
		"url": "https://cdn.example.com/play?id=1&format=m3u8",
		"nested": []any{
			"https:\\/\\/cdn.example.com\\/audio?id=2&type=mp4",
		},
	}

	urls := extractMediaURLs(payload)
	if !containsString(urls, "https://cdn.example.com/play?id=1&format=m3u8") {
		t.Fatalf("missing format=m3u8 URL: %#v", urls)
	}
	if !containsString(urls, "https://cdn.example.com/audio?id=2&type=mp4") {
		t.Fatalf("missing escaped type=mp4 URL: %#v", urls)
	}
}

func TestPickFormatRecognizesQueryMediaHints(t *testing.T) {
	tests := map[string]string{
		"https://cdn.example.com/play?id=1&format=m3u8": "m3u8",
		"https://cdn.example.com/play?id=1&type=mp4":    "mp4",
	}
	for raw, want := range tests {
		if got := pickFormat(raw, ""); got != want {
			t.Fatalf("pickFormat(%q) = %q, want %q", raw, got, want)
		}
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
