package youdao

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
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
		allowed := []string{"youdao", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "youdao" {
		t.Fatalf("Site = %q, want youdao", media.Site)
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

	media, err := (&Youdao{}).Extract("https://www.ydshengxue.com/after-sale/1001?courseId=1001", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	got := goldenFirstPlayableURL(media)
	if !strings.Contains(got, "https://media.example.com/youdao/lesson-1.m3u8") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestExtractRewritesM3U8ToInlineKeyDataURL(t *testing.T) {
	key := []byte("0123456789abcdef")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/user_status.jsonp"):
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"success":true}`))
		case strings.Contains(r.URL.Path, "/ai-product/api/app/v2/products/after-sale/1001"):
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"success":true,"data":{"title":"Course","videoPackageTab":[{"title":"Lesson","downloadUrl":"https://media.example.com/youdao/lesson.m3u8","id":"v1","cardPackageId":"c1","liveCenterId":"l1"}]}}`))
		case strings.Contains(r.URL.Path, "/youdao/lesson.m3u8"):
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:1,\nseg.ts\n"))
		case strings.Contains(r.URL.Path, "/hikari-live/api/consumer/v1/key"):
			_, _ = w.Write(key)
		default:
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
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
	media, err := (&Youdao{}).Extract("https://www.ydshengxue.com/after-sale/1001?courseId=1001", &extractor.ExtractOpts{Cookies: jar})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(media.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(media.Entries))
	}
	stream := media.Entries[0].Streams["default"]
	if stream.Format != "m3u8" || !stream.NeedMerge {
		t.Fatalf("stream format/merge = %q/%v, want m3u8/true", stream.Format, stream.NeedMerge)
	}
	const prefix = "data:application/vnd.apple.mpegurl;base64,"
	if len(stream.URLs) != 1 || !strings.HasPrefix(stream.URLs[0], prefix) {
		t.Fatalf("stream URL = %#v, want m3u8 data URL", stream.URLs)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stream.URLs[0], prefix))
	if err != nil {
		t.Fatalf("decode data URL: %v", err)
	}
	text := string(decoded)
	wantKey := "URI=\"0x" + strings.ToUpper(hex.EncodeToString(key)) + "\""
	if !strings.Contains(text, wantKey) {
		t.Fatalf("m3u8 text missing inline key %q: %s", wantKey, text)
	}
	if !strings.Contains(text, "https://media.example.com/youdao/seg.ts") {
		t.Fatalf("m3u8 text did not absolutize segment: %s", text)
	}
	if media.Entries[0].Extra["source_type"] != "m3u8_text" {
		t.Fatalf("source_type=%#v, want m3u8_text", media.Entries[0].Extra["source_type"])
	}
}
