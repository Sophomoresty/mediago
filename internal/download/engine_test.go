package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
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

func TestDownloadSingleCancelRemovesPart(t *testing.T) {
	dir := t.TempDir()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		flusher, _ := w.(http.Flusher)
		close(started)
		chunk := make([]byte, 8192)
		for i := 0; i < 128; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	engine := New(Opts{OutputDir: dir, Overwrite: true, NoProgress: true, Context: ctx})
	outPath := filepath.Join(dir, "video.mp4")
	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.downloadSingle(server.URL, outPath, nil, 0)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server did not receive request")
	}
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("downloadSingle returned nil error after cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("downloadSingle did not return after cancellation")
	}
	if _, err := os.Stat(outPath + ".part"); !os.IsNotExist(err) {
		t.Fatalf("part file still exists or stat failed unexpectedly: %v", err)
	}
}

func TestDownloadSegmentsCancelRemovesTempDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 128; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := w.Write([]byte(strings.Repeat("x", 8192))); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	engine := New(Opts{OutputDir: root, Overwrite: true, NoProgress: true, Context: ctx})
	outPath := filepath.Join(root, "video.mp4")
	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.downloadSegments([]string{server.URL, server.URL}, outPath, nil, 0)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server did not receive request")
	}
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("downloadSegments returned nil error after cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("downloadSegments did not return after cancellation")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir root: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "medigo-seg-") {
			t.Fatalf("temporary directory still present: %s", entry.Name())
		}
	}
}
