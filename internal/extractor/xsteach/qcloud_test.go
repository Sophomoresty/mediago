package xsteach

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/util"
)

func TestRewriteQcloudM3U8AbsolutizesAndAppendsToken(t *testing.T) {
	text := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nseg-1.ts\n"
	got := rewriteQcloudM3U8(text, "https://cdn.example.com/path/index.m3u8", "tok")
	if !strings.Contains(got, `URI="https://cdn.example.com/path/key.bin?token=tok"`) {
		t.Fatalf("rewritten key missing token: %s", got)
	}
	if !strings.Contains(got, "https://cdn.example.com/path/seg-1.ts") {
		t.Fatalf("segment not absolutized: %s", got)
	}
}

func TestLoadFinalQcloudM3U8ReturnsDataURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100,RESOLUTION=640x360\nlow/index.m3u8\n"))
		case "/low/index.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nseg.ts\n"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := util.NewClient()
	u, text := loadFinalQcloudM3U8(c, qcloudPlayInfo{MasterURL: srv.URL + "/master.m3u8", DRMToken: "tok"}, 1080)
	if !strings.HasPrefix(u, "data:application/vnd.apple.mpegurl;base64,") {
		t.Fatalf("url = %q", u)
	}
	if !strings.Contains(text, srv.URL+"/low/key.bin?token=tok") || !strings.Contains(text, srv.URL+"/low/seg.ts") {
		t.Fatalf("rewritten text = %s", text)
	}
	encoded := strings.TrimPrefix(u, "data:application/vnd.apple.mpegurl;base64,")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(decoded) != text {
		t.Fatalf("data URL content mismatch: %v", err)
	}
}

func TestSelectQcloudVariantHonorsTargetQuality(t *testing.T) {
	master := "#EXTM3U\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1920x1080\n1080/index.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=1800000,RESOLUTION=1280x720\n720/index.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=854x480\n480/index.m3u8\n"
	base := "https://cdn.example.com/master.m3u8"

	got, _ := selectQcloudVariant(master, base, qcloudPlayInfo{}, 720)
	if got != "https://cdn.example.com/720/index.m3u8" {
		t.Fatalf("HD variant = %q, want 720p", got)
	}
	got, _ = selectQcloudVariant(master, base, qcloudPlayInfo{}, 480)
	if got != "https://cdn.example.com/480/index.m3u8" {
		t.Fatalf("SD variant = %q, want 480p", got)
	}
	got, _ = selectQcloudVariant(master, base, qcloudPlayInfo{}, 1080)
	if got != "https://cdn.example.com/1080/index.m3u8" {
		t.Fatalf("FHD variant = %q, want 1080p", got)
	}
}
