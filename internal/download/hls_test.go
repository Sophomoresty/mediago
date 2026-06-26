package download

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func TestDownloadHLSAES128Fallback(t *testing.T) {
	dir := t.TempDir()
	engine := New(Opts{OutputDir: dir, Overwrite: true, Concurrency: 2, Retries: 1})
	engine.ffmpeg = ""

	key := []byte("0123456789abcdef")
	iv := []byte("abcdef0123456789")
	seg1 := encryptHLSSegment(t, []byte("segment-one"), key, iv)
	seg2 := encryptHLSSegment(t, []byte("segment-two"), key, iv)

	playlist := fmt.Sprintf(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-KEY:METHOD=AES-128,URI="data:application/octet-stream;base64,%s",IV=0x%s
#EXTINF:10,
%s
#EXTINF:10,
%s
#EXT-X-ENDLIST
`, base64.StdEncoding.EncodeToString(key), fmt.Sprintf("%x", iv), dataURLBase64("application/octet-stream", seg1), dataURLBase64("application/octet-stream", seg2))

	outPath := filepath.Join(dir, "video.mp4")
	_, err := engine.downloadHLS("video", extractor.Stream{URLs: []string{dataURLText("application/vnd.apple.mpegurl", playlist)}, Format: "m3u8"})
	if err != nil {
		t.Fatalf("downloadHLS returned error: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "segment-onesegment-two" {
		t.Fatalf("output = %q", string(data))
	}
}

func encryptHLSSegment(t *testing.T, plain, key, iv []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	buf := append(append([]byte{}, plain...), bytesRepeat(byte(pad), pad)...)
	dst := make([]byte, len(buf))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(dst, buf)
	return dst
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func dataURLText(mime, content string) string {
	return "data:" + mime + ";charset=utf-8," + url.PathEscape(content)
}

func dataURLBase64(mime string, content []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(content)
}
