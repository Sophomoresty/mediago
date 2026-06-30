package zlketang

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
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
		allowed := []string{"zlketang", "login", "cookie", "auth", "blocked", "rejected", "cannot parse", "parse", "invalid character", "no playable", "no media", "empty", "failed", "requires", "required", "not found", "missing", "token"}
		if !containsAny(msg, allowed) {
			t.Fatalf("unexpected extractor error: %v", err)
		}
		return
	}
	if media == nil {
		t.Fatalf("Extract returned nil MediaInfo without error")
	}
	if media.Site != "zlketang" {
		t.Fatalf("Site = %q, want zlketang", media.Site)
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

	media, err := (&Zlketang{}).Extract("https://www.zlketang.com/wxpub/page/zl_course/commodity.html?product_id=1001&course_id=2001", &extractor.ExtractOpts{Cookies: jar})
	assertGoldenOutcome(t, media, err)
	if err != nil {
		t.Fatalf("Extract returned error against golden fixture: %v", err)
	}
	got := goldenFirstPlayableURL(media)
	if !strings.Contains(got, "https://media.example.com/zlketang/lesson-1.m3u8") {
		t.Fatalf("playable URL %q does not contain expected fixture URL", got)
	}
}

func TestOnlyFilesModeSkipsVideoEntries(t *testing.T) {
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

	media, err := (&Zlketang{}).Extract("https://www.zlketang.com/wxpub/page/zl_course/commodity.html?product_id=1001&course_id=2001", &extractor.ExtractOpts{Cookies: jar, Quality: "2"})
	if err != nil {
		t.Fatalf("Extract only-files returned error: %v", err)
	}
	if len(media.Entries) != 1 {
		t.Fatalf("entries = %d, want only file entry", len(media.Entries))
	}
	got := goldenFirstPlayableURL(media)
	if got != "https://media.example.com/zlketang/handout.pdf" {
		t.Fatalf("playable URL = %q, want handout PDF", got)
	}
}

func TestLoadFinalQCloudM3U8RewritesEncryptedKey(t *testing.T) {
	overlayKey := "00112233445566778899aabbccddeeff"
	overlayIV := "0102030405060708090a0b0c0d0e0f10"
	clearKey := []byte("0123456789abcdef")
	encryptedKey := aesCBCEncryptNoPad(t, clearKey, overlayKey, overlayIV)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100\nvariant.m3u8\n"))
		case "/variant.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\n#EXTINF:1,\nseg.ts\n"))
		case "/key.bin":
			if got := r.URL.Query().Get("token"); got != "drm-token" {
				t.Errorf("key token = %q, want drm-token", got)
			}
			_, _ = w.Write(encryptedKey)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := &zlContext{c: util.NewClient(), headers: map[string]string{"User-Agent": "test-agent"}}
	variantURL, text := ctx.loadFinalQCloudM3U8(zlQCloudPlayInfo{
		MasterURL:  srv.URL + "/master.m3u8",
		DRMToken:   "drm-token",
		OverlayKey: overlayKey,
		OverlayIV:  overlayIV,
	})
	if variantURL != srv.URL+"/variant.m3u8" {
		t.Fatalf("variantURL = %q, want variant", variantURL)
	}
	wantKey := `URI="data:application/octet-stream;base64,` + base64.StdEncoding.EncodeToString(clearKey) + `"`
	if !strings.Contains(text, wantKey) {
		t.Fatalf("rewritten m3u8 missing decrypted key %q in:\n%s", wantKey, text)
	}
	if !strings.Contains(text, srv.URL+"/seg.ts") {
		t.Fatalf("rewritten m3u8 missing absolute segment:\n%s", text)
	}
	if !strings.HasPrefix(zlM3U8DataURL(text), "data:application/vnd.apple.mpegurl;base64,") {
		t.Fatalf("m3u8 text was not converted to data URL")
	}
}

func aesCBCEncryptNoPad(t *testing.T, plain []byte, keyHex, ivHex string) []byte {
	t.Helper()
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	iv, err := hex.DecodeString(ivHex)
	if err != nil {
		t.Fatalf("decode iv: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	if len(plain)%aes.BlockSize != 0 {
		t.Fatalf("plain length %d is not block aligned", len(plain))
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plain)
	return out
}
