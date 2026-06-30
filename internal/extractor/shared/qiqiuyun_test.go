package shared

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/util"
)

func TestDecodeQiqiuyunKeyVectors(t *testing.T) {
	cases := []struct {
		version int
		in      string
		wantHex string
	}{
		{version: 1, in: "6f0c83bc105fd4430843", wantHex: "36663063383362633135666434333034"},
		{version: 1, in: "abcdefghijklmnopq", wantHex: "6a6b64656667686962636c6d6e6f7071"},
		{version: 2, in: "abcdefghijklmnopqrst", wantHex: "61626300666768006b6c6d6e00717200"},
		{version: 3, in: "abcdefghijklmnopqrst", wantHex: "6162636465006869006c6d0070710074"},
		{version: 4, in: "abcdefghijklmnopqrst0", wantHex: "656e746f616471736a636c676962666d"},
		{version: 10, in: "abcdefghijklmnopqrst0", wantHex: "6a6b6474626d6765736f717066637269"},
	}
	for _, tc := range cases {
		if got := hex.EncodeToString(DecodeQiqiuyunKey([]byte(tc.in), tc.version)); got != tc.wantHex {
			t.Fatalf("DecodeQiqiuyunKey(%q,%d)=%q, want %q", tc.in, tc.version, got, tc.wantHex)
		}
	}
}

func TestPrepareQiqiuyunM3U8RewritesVariantKeyAndSegments(t *testing.T) {
	oldProxy := util.DefaultProxy()
	if err := util.SetDefaultProxy(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = util.SetDefaultProxy(oldProxy) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=900\nhigh/index.m3u8\n"))
		case "/high/index.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\",IV=0x010203\n#EXTINF:4,\nseg.ts\n#EXT-X-ENDLIST\n"))
		case "/high/key.bin":
			_, _ = w.Write([]byte("6f0c83bc105fd4430843"))
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	result := PrepareQiqiuyunM3U8(util.NewClient(), srv.URL+"/master.m3u8", QiqiuyunM3U8Options{Referer: "https://wallstreets.cn/", Version: 1, Mode: 1})
	if result.Text == "" || !strings.HasPrefix(result.URL, "data:application/vnd.apple.mpegurl;base64,") {
		t.Fatalf("prepared m3u8 missing data URL/text: %#v", result)
	}
	if result.SourceURL != srv.URL+"/high/index.m3u8" {
		t.Fatalf("source URL=%q", result.SourceURL)
	}
	if !strings.Contains(result.Text, srv.URL+"/high/seg.ts") {
		t.Fatalf("segment URL was not absolutized: %s", result.Text)
	}
	if !strings.Contains(result.Text, "data:application/octet-stream;base64,NmYwYzgzYmMxNWZkNDMwNA==") {
		t.Fatalf("key URI was not decoded/inlined: %s", result.Text)
	}
	cryptor, ok := result.Meta["cryptor"].(map[string]any)
	if !ok {
		t.Fatalf("cryptor meta missing: %#v", result.Meta)
	}
	segments, ok := cryptor["segments"].(map[int]map[string]any)
	if !ok || len(segments) != 1 {
		t.Fatalf("segment map mismatch: %#v", cryptor["segments"])
	}
	if got := string(segments[0]["key"].([]byte)); got != "6f0c83bc15fd4304" {
		t.Fatalf("decoded key=%q", got)
	}
	iv := segments[0]["iv"].([]byte)
	if len(iv) != 16 || iv[13] != 1 || iv[14] != 2 || iv[15] != 3 {
		t.Fatalf("iv not left-padded: %#v", iv)
	}
}
