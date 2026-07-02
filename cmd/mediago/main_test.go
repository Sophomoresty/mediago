package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Sophomoresty/mediago/internal/download"
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

type stubSingleExtractor struct{}

func (s *stubSingleExtractor) Patterns() []string {
	return []string{`example\.com/stub-single`}
}

func (s *stubSingleExtractor) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	return &extractor.MediaInfo{
		Site:  "Example",
		Title: "Stub Single",
		Streams: map[string]extractor.Stream{
			"1080p": {
				Quality: "1080p",
				URLs:    []string{"https://media.example.com/video-1080.mp4"},
				Format:  "mp4",
				Size:    1024,
			},
		},
	}, nil
}

func TestProcessURLPlaylistOutputsExtractionAndDownloadCounts(t *testing.T) {
	extractor.Register(&stubPlaylistExtractor{}, extractor.SiteInfo{Name: "Example", URL: "example.com"})

	oldSimulate := simulate
	oldDumpJSON := dumpJSON
	oldListFormats := listFormats
	oldDownloadOne := downloadOneWithArchiveFn
	oldDownloadAll := downloadAll
	simulate = false
	dumpJSON = false
	listFormats = false
	downloadAll = true
	downloadOneWithArchiveFn = func(ctx context.Context, info *extractor.MediaInfo, archive *download.Archive) error { return nil }
	t.Cleanup(func() {
		simulate = oldSimulate
		dumpJSON = oldDumpJSON
		listFormats = oldListFormats
		downloadOneWithArchiveFn = oldDownloadOne
		downloadAll = oldDownloadAll
	})

	stdout, stderr := captureStdStreams(t, func() {
		if err := processURL(context.Background(), "https://example.com/stub-playlist", nil); err != nil {
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

func TestProcessURLDumpJSONUsesExtractorResult(t *testing.T) {
	extractor.Register(&stubSingleExtractor{}, extractor.SiteInfo{Name: "ExampleSingle", URL: "example.com/stub-single"})

	oldSimulate := simulate
	oldDumpJSON := dumpJSON
	oldListFormats := listFormats
	oldFormatSpec := formatSpec
	simulate = false
	dumpJSON = true
	listFormats = false
	formatSpec = "best"
	t.Cleanup(func() {
		simulate = oldSimulate
		dumpJSON = oldDumpJSON
		listFormats = oldListFormats
		formatSpec = oldFormatSpec
	})

	stdout, stderr := captureStdStreams(t, func() {
		if err := processURL(context.Background(), "https://example.com/stub-single", nil); err != nil {
			t.Fatalf("processURL returned error: %v", err)
		}
	})

	var got extractor.MediaInfo
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not MediaInfo JSON: %v\n%s", err, stdout)
	}
	if got.Site != "Example" || got.Title != "Stub Single" {
		t.Fatalf("unexpected JSON media: %#v", got)
	}
	if !strings.Contains(stderr, "[info] Extracting: ExampleSingle https://example.com/stub-single") {
		t.Fatalf("stderr missing extracting line: %q", stderr)
	}
}

func TestProcessURLListFormatsPrintsFormatTable(t *testing.T) {
	extractor.Register(&stubSingleExtractor{}, extractor.SiteInfo{Name: "ExampleFormats", URL: "example.com/stub-single"})

	oldSimulate := simulate
	oldDumpJSON := dumpJSON
	oldListFormats := listFormats
	oldFormatSpec := formatSpec
	simulate = false
	dumpJSON = false
	listFormats = true
	formatSpec = "best"
	t.Cleanup(func() {
		simulate = oldSimulate
		dumpJSON = oldDumpJSON
		listFormats = oldListFormats
		formatSpec = oldFormatSpec
	})

	stdout, _ := captureStdStreams(t, func() {
		if err := processURL(context.Background(), "https://example.com/stub-single", nil); err != nil {
			t.Fatalf("processURL returned error: %v", err)
		}
	})

	for _, want := range []string{"QUALITY", "1080p", "mp4", "1.0KiB"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %s", want, stdout)
		}
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
