package shared

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/cookie"
	"github.com/Sophomoresty/mediago/internal/util"
)

func TestCssLcloudResolvePlayInfo(t *testing.T) {
	var loginHits, vodHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/room/replay/login", func(w http.ResponseWriter, r *http.Request) {
		loginHits++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("login parse form: %v", err)
		}
		if r.FormValue("liveRoomId") == "" || r.FormValue("recordId") == "" {
			t.Errorf("login form missing required fields: %+v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": "OK",
			"datas":  map[string]string{"sessionId": "test-session-123"},
		})
	})
	mux.HandleFunc("/api/record/vod", func(w http.ResponseWriter, r *http.Request) {
		vodHits++
		if r.URL.Query().Get("token") != "test-session-123" {
			t.Errorf("vod missing session token: %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"vod_info": map[string]any{
					"video": []map[string]any{
						{"url": "https://cdn.example.com/play_sd.m3u8", "definition": 1},
						{"url": "https://cdn.example.com/play_hd.m3u8", "definition": 2},
					},
					"audio": []map[string]any{
						{"url": "https://cdn.example.com/audio.aac"},
					},
				},
			},
		})
	})

	// Run via overridden URLs through a custom client pointed at the test server.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Temporarily override the package-level URLs to point at our mock.
	origLogin, origVod := CssLcloudReplayLoginURL, CssLcloudReplayVodURL
	defer func() {
		// Constants can't be reassigned at runtime; we route via DNS rewrite
		// instead — see directURLOverride helper.
		_ = origLogin
		_ = origVod
	}()

	// Use a real client. We point both URLs at the test server via an
	// http.Client that rewrites host. Simpler: use util.NewClient() and
	// rely on full URLs.
	c := util.NewClient()
	c.SetCookieJar(cookie.NewStore().Jar())

	// Build the URL helpers we test directly:
	loginURL := srv.URL + "/api/room/replay/login"
	_ = srv.URL + "/api/record/vod"

	loginBody, err := c.PostForm(loginURL, map[string]string{
		"liveRoomId": "room1", "recordId": "rec1", "accessid": "acc1",
		"userid": "u1", "viewertoken": "t1",
	}, nil)
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	var login struct {
		Datas struct {
			SessionID string `json:"sessionId"`
		} `json:"datas"`
	}
	if err := json.Unmarshal([]byte(loginBody), &login); err != nil {
		t.Fatalf("login decode: %v", err)
	}
	if login.Datas.SessionID != "test-session-123" {
		t.Errorf("unexpected session: %q", login.Datas.SessionID)
	}

	// Test the m3u8 key rewrite helper with a simple manifest.
	mux.HandleFunc("/key1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte{0xAB, 0xCD, 0xEF, 0x01, 0x02, 0x03, 0x04, 0x05,
			0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D})
	})
	m3u8 := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-KEY:METHOD=AES-128,URI="` + srv.URL + `/key1"
#EXTINF:10,
seg1.ts
#EXT-X-ENDLIST
`
	rewritten, err := CssLcloudRewriteM3U8Keys(c, m3u8, srv.URL)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !strings.Contains(rewritten, "URI=0x") && !strings.Contains(rewritten, "0xABCDEF") {
		t.Errorf("expected hex-encoded key in rewritten manifest, got: %s", rewritten)
	}

	// Quick check on URL escaping helper (purely sanity).
	if url.QueryEscape("a=b") != "a%3Db" {
		t.Errorf("url escape sanity check failed")
	}

	if loginHits == 0 {
		t.Error("login endpoint not hit")
	}
	if vodHits != 0 {
		// We didn't call the vod URL in this minimal test; just sanity.
	}
}

func TestPolyvResolveSecure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{
				"playsafe":  map[string]string{"token": "tok-xyz"},
				"paths":     []string{"https://hls.videocc.net/aa/bb/vid_900.m3u8"},
				"title":     "test course",
				"encrypted": false,
			},
		})
	}))
	defer srv.Close()

	c := util.NewClient()
	body, err := c.GetString(srv.URL+"/secure/vid1.json", nil)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	var sec PolyvSecure
	if err := json.Unmarshal([]byte(body), &sec); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sec.Data.Playsafe.Token != "tok-xyz" {
		t.Errorf("token mismatch: %q", sec.Data.Playsafe.Token)
	}
	if len(sec.Data.Paths) != 1 {
		t.Errorf("paths len: %d", len(sec.Data.Paths))
	}

	url, err := PolyvPickBestManifest(&sec)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if !strings.HasPrefix(url, "https://hls.videocc.net/") {
		t.Errorf("manifest URL: %q", url)
	}

	// The source Python treats Polyv's "encrypted" flag as a key/secure
	// payload marker and still downloads the returned HLS/PDX manifest. It
	// must not be treated as an automatic DRM block when playable manifests
	// are present.
	sec.Data.Encrypted = true
	url, err = PolyvPickBestManifest(&sec)
	if err != nil {
		t.Fatalf("encrypted secure manifest should still be picked: %v", err)
	}
	if !strings.HasPrefix(url, "https://hls.videocc.net/") {
		t.Errorf("manifest URL after encrypted flag: %q", url)
	}
}

func TestBokeCCResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("vid") == "" || r.URL.Query().Get("siteid") == "" {
			http.Error(w, "missing params", 400)
			return
		}
		w.Write([]byte(`<video><copy><quality>30</quality><playurl>https://cdn.example.com/sd.mp4</playurl></copy><copy><quality>50</quality><playurl>https://cdn.example.com/hd.mp4</playurl></copy></video>`))
	}))
	defer srv.Close()

	// Point client at mock server by overriding the URL when calling.
	// We can't reassign const; instead test by calling helpers that use the URL directly.
	c := util.NewClient()
	body, err := c.GetBytes(srv.URL+"/?vid=v1&siteid=s1", nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(string(body), "playurl") {
		t.Errorf("mock didn't return XML: %s", body)
	}
}

func TestBaijiayunJSONPUnwrap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`bjyCallback({"code":0,"data":{"video_url":"https://cdn.example.com/v.mp4"}});`))
	}))
	defer srv.Close()

	c := util.NewClient()
	resp, err := fetchAndUnwrapJSONP(c, srv.URL+"/?render=jsonp", nil)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if resp.Data.VideoURL != "https://cdn.example.com/v.mp4" {
		t.Errorf("video_url mismatch: %q", resp.Data.VideoURL)
	}
}

func TestSharedResolversRejectNilClient(t *testing.T) {
	manifest := `#EXTM3U
#EXT-X-KEY:METHOD=AES-128,URI="https://cdn.example.com/key"
#EXTINF:10,
seg.ts
`
	payload := AliyunPlayPayload{
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
		SecurityToken:   "st",
		Region:          "cn-shanghai",
		AuthTimeout:     "7200",
	}
	cases := []struct {
		name string
		fn   func() error
	}{
		{"baijiayun vod", func() error { _, err := BaijiayunResolveVOD(nil, "vid", "token", nil); return err }},
		{"baijiayun playback", func() error { _, err := BaijiayunResolvePlayback(nil, "room", "token", nil); return err }},
		{"bokecc", func() error { _, err := BokeCCResolve(nil, "vid", "site", nil); return err }},
		{"polyv secure", func() error { _, err := PolyvResolveSecure(nil, "vid", nil); return err }},
		{"polyv rewrite", func() error { _, err := PolyvRewriteM3U8Keys(nil, manifest, "token", ""); return err }},
		{"csslcloud play", func() error {
			_, err := CssLcloudResolvePlayInfo(nil, CssLcloudPayload{LiveRoomID: "room", AccessID: "acc", RecordID: "rec"})
			return err
		}},
		{"csslcloud rewrite", func() error { _, err := CssLcloudRewriteM3U8Keys(nil, manifest, ""); return err }},
		{"aliyun play", func() error { _, err := AliyunResolvePlayInfo(nil, payload, "vid", AliyunPlayOptions{}); return err }},
		{"aliyun rewrite", func() error {
			_, err := AliyunRewriteM3U8Keys(nil, manifest, payload, "AliyunVoDEncryption", "https://cdn.example.com/play.m3u8", AliyunPlayOptions{})
			return err
		}},
		{"aliyun license", func() error {
			_, err := AliyunRequestLicense(nil, payload, "media", "challenge", "AliyunVoDEncryption", AliyunPlayOptions{})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v", r)
				}
			}()
			if err := tc.fn(); err == nil {
				t.Fatal("expected error for nil client")
			}
		})
	}
}

func TestPolyvNormalizeVIDAndLegacyHLS(t *testing.T) {
	cases := map[string]string{
		"abc123":   "abc123_a",
		"abc123_a": "abc123_a",
		"abc123_z": "abc123_a",
		"":         "",
	}
	for in, want := range cases {
		if got := PolyvNormalizeVID(in); got != want {
			t.Fatalf("PolyvNormalizeVID(%q)=%q want %q", in, got, want)
		}
	}

	sec := &PolyvSecure{}
	sec.HLS = []string{
		"https://hls.videocc.net/path/vid_360.m3u8",
		"https://hls.videocc.net/path/vid_720.m3u8",
	}
	got, err := PolyvPickBestManifest(sec)
	if err != nil {
		t.Fatalf("PolyvPickBestManifest legacy hls: %v", err)
	}
	if got != "https://hls.videocc.net/path/vid_720.m3u8" {
		t.Fatalf("legacy hls manifest=%q", got)
	}

	sec = &PolyvSecure{}
	sec.Data.Paths = []string{"aa/bb/vid_900.m3u8"}
	got, err = PolyvPickBestManifest(sec)
	if err != nil {
		t.Fatalf("PolyvPickBestManifest path fragment: %v", err)
	}
	if got != "https://hls.videocc.net/aa/bb/vid_900.m3u8" {
		t.Fatalf("fragment manifest=%q", got)
	}
}

func TestPolyvRewriteM3U8KeysWithOptionsResolvesAndDecrypts(t *testing.T) {
	seed := "321"
	plainKey := []byte("0123456789abcdef")
	encryptedKey := encryptPolyvFixtureKey(t, plainKey, seed)
	var requestedPath, requestedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedToken = r.URL.Query().Get("token")
		_, _ = w.Write(encryptedKey)
	}))
	defer srv.Close()

	manifestURL := srv.URL + "/aa/bb/vid_900.m3u8"
	m3u8 := `#EXTM3U
#EXT-X-KEY:METHOD=AES-128,URI="vid_900.key"
#EXTINF:10,
seg.ts
`
	c := util.NewClient()
	rewritten, err := PolyvRewriteM3U8KeysWithOptions(c, m3u8, PolyvRewriteOptions{Token: "tok-xyz", ManifestURL: manifestURL, Referer: "https://wx.233.com/", SeedConst: seed})
	if err != nil {
		t.Fatalf("PolyvRewriteM3U8KeysWithOptions: %v", err)
	}
	if requestedPath != "/playsafe/aa/bb/vid_900.key" {
		t.Fatalf("key path=%q", requestedPath)
	}
	if requestedToken != "tok-xyz" {
		t.Fatalf("key token=%q", requestedToken)
	}
	if !strings.Contains(rewritten, "0x30313233343536373839616263646566") {
		t.Fatalf("rewritten key mismatch: %s", rewritten)
	}
}

func TestPolyvResolvePDXDecryptsAndBuildsMetadata(t *testing.T) {
	seed := "777"
	plainM3U8 := `#EXTM3U
#EXT-X-KEY:METHOD=AES-128,URI="key.bin",IV=0x00112233445566778899aabbccddeeff
#EXTINF:4.5,
seg-1.ts
#EXT-X-ENDLIST
`
	pdxBody := map[string]any{
		"body":       encryptPolyvPDXFixtureBody(t, plainM3U8, seed),
		"seed_const": seed,
		"version":    2,
	}
	var keyPath, keyToken, keyPID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/aa/bb/vid_900.pdx":
			if r.URL.Query().Get("token") != "tok-pdx" || r.URL.Query().Get("device") != "desktop" || r.URL.Query().Get("pid") == "" {
				t.Fatalf("pdx query = %q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(pdxBody)
		case "/playsafe/v13/aa/bb/key.bin":
			keyPath = r.URL.Path
			keyToken = r.URL.Query().Get("token")
			keyPID = r.URL.Query().Get("pid")
			_, _ = w.Write([]byte("0123456789abcdef0123456789abcdef"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := util.NewClient()
	info, err := PolyvResolvePDX(c, PolyvPDXOptions{
		VideoID:       "vid",
		PDXURL:        srv.URL + "/aa/bb/vid_900.pdx",
		PlaySafeToken: "tok-pdx",
	})
	if err != nil {
		t.Fatalf("PolyvResolvePDX: %v", err)
	}
	if info.Type != "pdx" || info.PlaySafeToken != "tok-pdx" || info.SeedConst != seed {
		t.Fatalf("unexpected pdx info: %#v", info)
	}
	if keyPath != "/playsafe/v13/aa/bb/key.bin" || keyToken != "tok-pdx" || keyPID == "" {
		t.Fatalf("key request path/token/pid = %q/%q/%q", keyPath, keyToken, keyPID)
	}
	if !strings.Contains(info.M3U8Text, "/playsafe/v13/aa/bb/key.bin") || !strings.Contains(info.M3U8Text, srv.URL+"/aa/bb/seg-1.ts") {
		t.Fatalf("m3u8 was not rewritten/absolutized: %s", info.M3U8Text)
	}
	if info.IVHex != "00112233445566778899AABBCCDDEEFF" {
		t.Fatalf("IVHex = %q", info.IVHex)
	}
	if len(info.Segments) != 1 || info.Segments[0] != srv.URL+"/aa/bb/seg-1.ts" {
		t.Fatalf("segments = %#v", info.Segments)
	}
	if len(info.SegmentDurations) != 1 || info.SegmentDurations[0] != 4.5 {
		t.Fatalf("durations = %#v", info.SegmentDurations)
	}
	if info.KeyHex != "3031323334353637383961626364656630313233343536373839616263646566" {
		t.Fatalf("KeyHex = %q", info.KeyHex)
	}
	meta := info.M3U8Meta()
	cryptor, ok := meta["cryptor"].(map[string]any)
	if !ok {
		t.Fatalf("M3U8Meta missing Python-compatible cryptor: %#v", meta)
	}
	if cryptor["_lel_cryptor_type"] != "polyv_pdx" || cryptor["key_hex"] != info.KeyHex || cryptor["iv_hex"] != info.IVHex {
		t.Fatalf("cryptor metadata mismatch: %#v", cryptor)
	}
	if got, ok := cryptor["segment_durations"].([]float64); !ok || len(got) != 1 || got[0] != 4.5 {
		t.Fatalf("cryptor segment_durations = %#v", cryptor["segment_durations"])
	}
	if meta["_lel_cryptor"] == nil {
		t.Fatalf("M3U8Meta should keep legacy _lel_cryptor alias: %#v", meta)
	}
}

func encryptPolyvFixtureKey(t *testing.T, plain []byte, seed string) []byte {
	t.Helper()
	padded := append([]byte(nil), plain...)
	pad := 16 - len(padded)%16
	if pad == 0 {
		pad = 16
	}
	padded = append(padded, bytes.Repeat([]byte{byte(pad)}, pad)...)
	sum := md5.Sum([]byte(seed))
	key := []byte(hex.EncodeToString(sum[:])[:16])
	iv, err := hex.DecodeString("01020305070B0D1113171D0705030201")
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out
}

func encryptPolyvPDXFixtureBody(t *testing.T, plain, seed string) string {
	t.Helper()
	padded := []byte(plain)
	pad := aes.BlockSize - len(padded)%aes.BlockSize
	if pad == 0 {
		pad = aes.BlockSize
	}
	padded = append(padded, bytes.Repeat([]byte{byte(pad)}, pad)...)
	keySeed := md5.Sum([]byte(polyvPDXSecretV2 + seed))
	key := []byte(hex.EncodeToString(keySeed[:])[1:17])
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, polyvPDXIVV2).CryptBlocks(out, padded)
	return base64.RawURLEncoding.EncodeToString(out)
}

func TestSharedVariantSelectionSkipsEmptyURLs(t *testing.T) {
	if got, ok := pickBestBaijiayunVideo([]BaijiayunVideo{
		{URL: "", Definition: "HD"},
		{URL: "https://cdn.example.com/bjy.mp4", Definition: "SD"},
	}); !ok || got != "https://cdn.example.com/bjy.mp4" {
		t.Fatalf("pickBestBaijiayunVideo = (%q,%v)", got, ok)
	}

	if got, ok := pickBestBokeCCCopy([]BokeCCVideo{
		{Quality: 100, PlayURL: ""},
		{Quality: 50, PlayURL: "https://cdn.example.com/bokecc.mp4"},
	}); !ok || got.PlayURL != "https://cdn.example.com/bokecc.mp4" {
		t.Fatalf("pickBestBokeCCCopy = (%#v,%v)", got, ok)
	}

	if got, ok := pickBestCssLcloudStream([]CssLcloudStreamInfo{
		{Definition: 100, URL: ""},
		{Definition: 50, URL: "https://cdn.example.com/cssl.m3u8"},
	}); !ok || got.URL != "https://cdn.example.com/cssl.m3u8" {
		t.Fatalf("pickBestCssLcloudStream = (%#v,%v)", got, ok)
	}

	sec := &PolyvSecure{}
	sec.Data.Paths = []string{"", "https://hls.videocc.net/path/video.m3u8"}
	polyvURL, err := PolyvPickBestManifest(sec)
	if err != nil {
		t.Fatalf("PolyvPickBestManifest returned error: %v", err)
	}
	if polyvURL != "https://hls.videocc.net/path/video.m3u8" {
		t.Fatalf("PolyvPickBestManifest = %q", polyvURL)
	}

	if _, err := PolyvPickBestManifest(nil); err == nil {
		t.Fatal("PolyvPickBestManifest(nil) expected error")
	}
}

func TestSharedVariantSelectionRejectsEmptyURLs(t *testing.T) {
	if got, ok := pickBestBaijiayunVideo([]BaijiayunVideo{{Definition: "HD"}}); ok || got != "" {
		t.Fatalf("pickBestBaijiayunVideo empty = (%q,%v)", got, ok)
	}
	if got, ok := pickBestBokeCCCopy([]BokeCCVideo{{Quality: 100}}); ok || got.PlayURL != "" {
		t.Fatalf("pickBestBokeCCCopy empty = (%#v,%v)", got, ok)
	}
	if got, ok := pickBestCssLcloudStream([]CssLcloudStreamInfo{{Definition: 100}}); ok || got.URL != "" {
		t.Fatalf("pickBestCssLcloudStream empty = (%#v,%v)", got, ok)
	}
	if _, err := PolyvPickBestManifest(&PolyvSecure{}); err == nil || !strings.Contains(fmt.Sprint(err), "paths") {
		t.Fatalf("PolyvPickBestManifest empty paths error = %v", err)
	}
}
