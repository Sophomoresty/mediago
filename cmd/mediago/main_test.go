package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

type stubPlaylistExtractor struct{}

func (s *stubPlaylistExtractor) Patterns() []string {
	return []string{`example\.com/stub-playlist`}
}

func (s *stubPlaylistExtractor) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	return &extractor.MediaInfo{
		Site:  "Example",
		Title: "Stub Playlist",
		Entries: []*extractor.MediaInfo{
			{Title: "Entry 1"},
			{Title: "Entry 2"},
		},
	}, nil
}

func TestProcessURLPlaylistOutputsExtractionAndDownloadCounts(t *testing.T) {
	extractor.Register(&stubPlaylistExtractor{}, extractor.SiteInfo{Name: "Example", URL: "example.com"})

	oldSimulate := simulate
	oldDumpJSON := dumpJSON
	oldListFormats := listFormats
	oldDownloadOne := downloadOneFn
	oldDownloadAll := downloadAll
	simulate = false
	dumpJSON = false
	listFormats = false
	downloadAll = true
	downloadOneFn = func(ctx context.Context, info *extractor.MediaInfo) error { return nil }
	t.Cleanup(func() {
		simulate = oldSimulate
		dumpJSON = oldDumpJSON
		listFormats = oldListFormats
		downloadOneFn = oldDownloadOne
		downloadAll = oldDownloadAll
	})

	stdout, stderr := captureStdStreams(t, func() {
		if err := processURL(context.Background(), "https://example.com/stub-playlist"); err != nil {
			t.Fatalf("processURL returned error: %v", err)
		}
	})

	if !strings.Contains(stderr, "[info] Extracting: Example https://example.com/stub-playlist") {
		t.Fatalf("stderr missing extracting line: %q", stderr)
	}
	if !strings.Contains(stderr, "[info] Playlist: Stub Playlist (2 items)") {
		t.Fatalf("stderr missing playlist line: %q", stderr)
	}
	if !strings.Contains(stderr, "[download] Downloading item 1 of 2: Entry 1") {
		t.Fatalf("stderr missing item 1 line: %q", stderr)
	}
	if !strings.Contains(stderr, "[download] Downloading item 2 of 2: Entry 2") {
		t.Fatalf("stderr missing item 2 line: %q", stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
}

func captureStdStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()

	oldOut := os.Stdout
	oldErr := os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout = outW
	os.Stderr = errW
	defer func() {
		os.Stdout = oldOut
		os.Stderr = oldErr
	}()

	fn()
	if err := outW.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	if err := errW.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	outBytes, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	errBytes, err := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(outBytes), string(errBytes)
}
