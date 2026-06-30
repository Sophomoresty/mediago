package yangcong

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
	"github.com/Sophomoresty/mediago/internal/util"
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
		allowed := []string{"yangcong", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "yangcong" {
		t.Fatalf("Site = %q, want yangcong", media.Site)
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

	media, err := (&Yangcong{}).Extract("https://school.yangcongxueyuan.com/special-course/special-1?courseType=special", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	got := goldenFirstPlayableURL(media)
	if !strings.Contains(got, "https://media.example.com/yangcong/lesson-1.mp4") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestSelectAddressHonorsSourceQualityModes(t *testing.T) {
	addresses := []map[string]any{
		{"format": "mp4", "clarity": "fullHigh", "platform": "pc", "url": "https://cdn.example.com/full.mp4"},
		{"format": "mp4", "clarity": "high", "platform": "pc", "url": "https://cdn.example.com/high.mp4"},
		{"format": "mp4", "clarity": "low", "platform": "pc", "url": "https://cdn.example.com/low.mp4"},
	}
	if got := selectAddress(addresses, "mp4", "1"); got != "https://cdn.example.com/full.mp4" {
		t.Fatalf("FHD URL = %q", got)
	}
	if got := selectAddress(addresses, "mp4", "2"); got != "https://cdn.example.com/high.mp4" {
		t.Fatalf("HD URL = %q", got)
	}
	if got := selectAddress(addresses, "mp4", "3"); got != "https://cdn.example.com/low.mp4" {
		t.Fatalf("SD URL = %q", got)
	}
}

func TestRewriteHLSM3U8UsesInlineHexKeyAndDataURL(t *testing.T) {
	key := []byte("0123456789abcdef")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/master.m3u8"):
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"https://keys.example.com/key?id=key1\"\nseg.ts\n"))
		case strings.Contains(r.URL.Path, "/videoBase/getHlsEncryptSalt"):
			salt := base64.StdEncoding.EncodeToString([]byte("salt"))
			_, _ = w.Write([]byte(`{"data":{"salt":"` + salt + `"}}`))
		case strings.Contains(r.URL.Path, "/videoBase/getHlsEncryptKey"):
			_, _ = w.Write(key)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()
	httpsSrv := httptest.NewTLSServer(handler)
	defer httpsSrv.Close()
	installMockTransport(t, httpSrv.URL, httpsSrv.URL)

	c := util.NewClient()
	text := rewriteHLSM3U8(c, map[string]string{}, "https://cdn.example.com/master.m3u8", "video-1")
	wantKey := "0x" + strings.ToUpper(hex.EncodeToString(key))
	if !strings.Contains(text, `URI="`+wantKey+`"`) {
		t.Fatalf("rewritten m3u8 missing inline key %q: %s", wantKey, text)
	}
	if !strings.Contains(text, "https://cdn.example.com/seg.ts") {
		t.Fatalf("rewritten m3u8 did not absolutize segment: %s", text)
	}
	if !strings.HasPrefix(yangcongM3U8DataURL(text), "data:application/vnd.apple.mpegurl;base64,") {
		t.Fatalf("data URL prefix mismatch")
	}
}
