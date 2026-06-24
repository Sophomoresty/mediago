package download

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nichuanfang/medigo/internal/extractor"
)

func TestDownloadSubtitlesWritesDataURL(t *testing.T) {
	dir := t.TempDir()
	engine := New(Opts{OutputDir: dir, Overwrite: true})
	info := &extractor.MediaInfo{
		Title: "video",
		Subtitles: []extractor.Subtitle{
			{Language: "zh-CN", URL: "data:text/vtt;charset=utf-8,WEBVTT%0A%0A00:00.000%20--%3E%2000:01.000%0A%E4%BD%A0%E5%A5%BD", Format: "vtt"},
		},
	}
	paths, err := engine.DownloadSubtitles(info, filepath.Join(dir, "video.mp4"))
	if err != nil {
		t.Fatalf("DownloadSubtitles returned error: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %d, want 1", len(paths))
	}
	if filepath.Base(paths[0]) != "video.zh-CN.vtt" {
		t.Fatalf("subtitle path = %q, want video.zh-CN.vtt", paths[0])
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("read subtitle: %v", err)
	}
	if string(data) != "WEBVTT\n\n00:00.000 --> 00:01.000\n你好" {
		t.Fatalf("subtitle data = %q", string(data))
	}
}
