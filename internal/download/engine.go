package download

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/progressbar/v3"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

type Opts struct {
	Concurrency int
	OutputDir   string
	Overwrite   bool
	Retries     int
}

type Engine struct {
	opts   Opts
	ffmpeg string
	client *util.Client
	http   *http.Client
}

func New(opts Opts) *Engine {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 10
	}
	if opts.Retries <= 0 {
		opts.Retries = 3
	}
	ffmpeg, _ := exec.LookPath("ffmpeg")
	return &Engine{
		opts:   opts,
		ffmpeg: ffmpeg,
		client: util.NewClient(),
		http: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (e *Engine) HasFFmpeg() bool {
	return e.ffmpeg != ""
}

func (e *Engine) Download(info *extractor.MediaInfo, stream extractor.Stream) (string, error) {
	filename := util.SanitizeFilename(info.Title)
	switch stream.Format {
	case "mp4", "flv", "mp3", "m4a":
		return e.downloadDirect(filename, stream)
	case "m3u8":
		return e.downloadHLS(filename, stream)
	case "dash":
		return e.downloadDASH(filename, stream)
	default:
		return e.downloadDirect(filename, stream)
	}
}

func (e *Engine) downloadDirect(filename string, stream extractor.Stream) (string, error) {
	if len(stream.URLs) == 0 {
		return "", fmt.Errorf("no URLs in stream")
	}

	ext := ".mp4"
	if stream.Format != "" {
		ext = "." + stream.Format
	}
	outPath := filepath.Join(e.opts.OutputDir, filename+ext)

	if !e.opts.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return outPath, nil
		}
	}

	if len(stream.URLs) == 1 {
		return outPath, e.downloadSingle(stream.URLs[0], outPath, stream.Headers, stream.Size)
	}

	return outPath, e.downloadSegments(stream.URLs, outPath, stream.Headers, stream.Size)
}

func (e *Engine) downloadSingle(url, outPath string, headers map[string]string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", util.RandomUA())
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	if size <= 0 {
		size = resp.ContentLength
	}

	partPath := outPath + ".part"
	f, err := os.Create(partPath)
	if err != nil {
		return err
	}

	bar := progressbar.DefaultBytes(size, filepath.Base(outPath))
	_, copyErr := io.Copy(io.MultiWriter(f, bar), resp.Body)
	closeErr := f.Close()

	if copyErr != nil {
		os.Remove(partPath)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(partPath)
		return closeErr
	}

	return os.Rename(partPath, outPath)
}

func (e *Engine) downloadSegments(urls []string, outPath string, headers map[string]string, totalSize int64) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "medigo-seg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	bar := progressbar.DefaultBytes(totalSize, filepath.Base(outPath))
	var downloaded atomic.Int64

	sem := make(chan struct{}, e.opts.Concurrency)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for i, u := range urls {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, url string) {
			defer wg.Done()
			defer func() { <-sem }()

			segPath := filepath.Join(tmpDir, fmt.Sprintf("seg_%05d", idx))
			err := e.downloadSeg(url, segPath, headers)
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				return
			}
			info, _ := os.Stat(segPath)
			if info != nil {
				n := info.Size()
				downloaded.Add(n)
				bar.Add64(n)
			}
		}(i, u)
	}
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}

	partPath := outPath + ".part"
	if err := concatFiles(tmpDir, partPath, len(urls)); err != nil {
		os.Remove(partPath)
		return err
	}
	return os.Rename(partPath, outPath)
}

func (e *Engine) downloadSeg(url, path string, headers map[string]string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", util.RandomUA())
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("segment HTTP %d: %s", resp.StatusCode, url)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func concatFiles(dir, outPath string, count int) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	for i := 0; i < count; i++ {
		segPath := filepath.Join(dir, fmt.Sprintf("seg_%05d", i))
		seg, err := os.Open(segPath)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, seg)
		seg.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
