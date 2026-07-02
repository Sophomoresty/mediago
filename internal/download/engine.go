package download

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type Opts struct {
	Concurrency       int
	OutputDir         string
	Overwrite         bool
	Retries           int
	NoProgress        bool
	Proxy             string
	Context           context.Context
	MergeOutputFormat string
	LimitRate         string
	Verbose           bool
}

// ParseRate parses a rate string like "1M", "500K", "2.5M" into bytes per second.
func ParseRate(rate string) int64 {
	if rate == "" {
		return 0
	}
	rate = strings.TrimSpace(rate)
	var multiplier int64 = 1
	upper := strings.ToUpper(rate)
	switch {
	case strings.HasSuffix(upper, "M"):
		multiplier = 1024 * 1024
		rate = rate[:len(rate)-1]
	case strings.HasSuffix(upper, "K"):
		multiplier = 1024
		rate = rate[:len(rate)-1]
	case strings.HasSuffix(upper, "G"):
		multiplier = 1024 * 1024 * 1024
		rate = rate[:len(rate)-1]
	}
	val := 0.0
	fmt.Sscanf(rate, "%f", &val)
	if val <= 0 {
		return 0
	}
	return int64(val * float64(multiplier))
}

// rateLimitedReader wraps a reader and limits read speed to bytesPerSec.
type rateLimitedReader struct {
	r            io.Reader
	bytesPerSec int64
	read         int64
	start        time.Time
}

func newRateLimitedReader(r io.Reader, bytesPerSec int64) io.Reader {
	if bytesPerSec <= 0 {
		return r
	}
	return &rateLimitedReader{
		r:            r,
		bytesPerSec: bytesPerSec,
		start:        time.Now(),
	}
}

func (r *rateLimitedReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.read += int64(n)
		elapsed := time.Since(r.start)
		expectedDuration := time.Duration(float64(r.read) / float64(r.bytesPerSec) * float64(time.Second))
		if sleepTime := expectedDuration - elapsed; sleepTime > 0 {
			time.Sleep(sleepTime)
		}
	}
	return n, err
}

type Engine struct {
	opts      Opts
	ffmpeg    string
	client    *util.Client
	http      *http.Client
	ctx       context.Context
	limitRate int64
}

func New(opts Opts) *Engine {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 10
	}
	if opts.Retries <= 0 {
		opts.Retries = 3
	}
	ffmpeg, _ := exec.LookPath("ffmpeg")
	httpClient, err := util.NewHTTPClient(5*time.Minute, opts.Proxy)
	if err != nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	client := util.NewClient()
	if opts.Proxy != "" {
		if pc, pcErr := util.NewClientWithProxy(opts.Proxy); pcErr == nil {
			client = pc
		}
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	limitRate := ParseRate(opts.LimitRate)
	if opts.Verbose && limitRate > 0 {
		fmt.Fprintf(os.Stderr, "[debug] Rate limit: %d bytes/sec\n", limitRate)
	}
	return &Engine{
		opts:      opts,
		ffmpeg:    ffmpeg,
		client:    client,
		http:      httpClient,
		ctx:       ctx,
		limitRate: limitRate,
	}
}

func (e *Engine) HasFFmpeg() bool {
	return e.ffmpeg != ""
}

func (e *Engine) outputExt() string {
	if e.opts.MergeOutputFormat != "" {
		return "." + e.opts.MergeOutputFormat
	}
	return ".mp4"
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

func (e *Engine) DownloadSubtitles(info *extractor.MediaInfo, videoPath string) ([]string, error) {
	if info == nil || len(info.Subtitles) == 0 {
		return nil, nil
	}
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	var paths []string
	for i, sub := range info.Subtitles {
		if strings.TrimSpace(sub.URL) == "" {
			continue
		}
		lang := util.SanitizeFilename(firstNonEmpty(sub.Language, "und"))
		ext := subtitleExt(sub)
		outPath := fmt.Sprintf("%s.%s.%s", base, lang, ext)
		if i > 0 && containsPath(paths, outPath) {
			outPath = fmt.Sprintf("%s.%s-%d.%s", base, lang, i+1, ext)
		}
		if !e.opts.Overwrite {
			if _, err := os.Stat(outPath); err == nil {
				paths = append(paths, outPath)
				continue
			}
		}
		if err := e.downloadSingle(sub.URL, outPath, nil, 0); err != nil {
			return paths, fmt.Errorf("%s: %w", sub.URL, err)
		}
		paths = append(paths, outPath)
	}
	return paths, nil
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
	if streamURLsAreMirrors(stream) {
		return outPath, e.downloadMirrorsWithHeaders(stream.URLs, outPath, stream.Headers, streamURLHeaders(stream), stream.Size)
	}

	return outPath, e.downloadSegments(stream.URLs, outPath, stream.Headers, stream.Size)
}

func streamURLsAreMirrors(stream extractor.Stream) bool {
	if len(stream.URLs) <= 1 || stream.Extra == nil {
		return false
	}
	if mode, ok := stream.Extra["url_mode"].(string); ok && strings.EqualFold(mode, "mirror") {
		return true
	}
	if v, ok := stream.Extra["cdn_nodes"].(bool); ok && v {
		return true
	}
	return false
}

func (e *Engine) downloadMirrors(urls []string, outPath string, headers map[string]string, size int64) error {
	return e.downloadMirrorsWithHeaders(urls, outPath, headers, nil, size)
}

func (e *Engine) downloadMirrorsWithHeaders(urls []string, outPath string, headers map[string]string, perURLHeaders map[string]map[string]string, size int64) error {
	var last error
	for _, raw := range urls {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		_ = os.Remove(outPath + ".part")
		h := headers
		if perURLHeaders != nil {
			if uh := perURLHeaders[raw]; len(uh) > 0 {
				h = uh
			}
		}
		if err := e.downloadSingle(raw, outPath, h, size); err != nil {
			last = err
			if ctxErr := e.ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			continue
		}
		return nil
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("no URLs in stream")
}

func (e *Engine) downloadSingle(url, outPath string, headers map[string]string, size int64) error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(url)), "data:") {
		return writeDataURL(url, outPath)
	}

	partPath := outPath + ".part"

	// Check for existing .part file for resume
	var resumeOffset int64
	if fi, err := os.Stat(partPath); err == nil && fi.Size() > 0 {
		resumeOffset = fi.Size()
	}

	req, err := http.NewRequestWithContext(e.ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", util.RandomUA())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if resumeOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeOffset))
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if e.opts.Verbose {
		fmt.Fprintf(os.Stderr, "[debug] GET %s -> %d (Content-Length: %d)\n", url, resp.StatusCode, resp.ContentLength)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	// Handle resume: if server returns 200 (full response), truncate and restart
	// If server returns 206 (partial), append to existing .part file
	var f *os.File
	if resumeOffset > 0 && resp.StatusCode == http.StatusPartialContent {
		// Resume: open in append mode
		f, err = os.OpenFile(partPath, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		if size <= 0 && resp.ContentLength > 0 {
			size = resumeOffset + resp.ContentLength
		}
	} else {
		// Full download: server returned 200 or no resume
		resumeOffset = 0
		f, err = os.Create(partPath)
		if err != nil {
			return err
		}
		if size <= 0 {
			size = resp.ContentLength
		}
	}

	// Wrap body with rate limiter if configured
	var body io.Reader = resp.Body
	if e.limitRate > 0 {
		body = newRateLimitedReader(body, e.limitRate)
	}

	var w io.Writer = f
	if !e.opts.NoProgress {
		bar := progressbar.DefaultBytes(size, filepath.Base(outPath))
		if resumeOffset > 0 {
			bar.Add64(resumeOffset)
		}
		w = io.MultiWriter(f, bar)
	}
	_, copyErr := io.Copy(w, body)
	closeErr := f.Close()

	if copyErr != nil {
		// Keep .part file for future resume
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}

	return os.Rename(partPath, outPath)
}

func writeDataURL(raw, outPath string) error {
	comma := strings.Index(raw, ",")
	if !strings.HasPrefix(strings.ToLower(raw), "data:") || comma < 0 {
		return fmt.Errorf("invalid data URL")
	}
	meta, payload := raw[5:comma], raw[comma+1:]
	var data []byte
	if strings.Contains(strings.ToLower(meta), ";base64") {
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return err
		}
		data = decoded
	} else {
		decoded, err := url.PathUnescape(payload)
		if err != nil {
			return err
		}
		data = []byte(decoded)
	}
	partPath := outPath + ".part"
	if err := os.WriteFile(partPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(partPath, outPath)
}

func (e *Engine) downloadSegments(urls []string, outPath string, headers map[string]string, totalSize int64) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "mediago-seg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	var bar *progressbar.ProgressBar
	if !e.opts.NoProgress {
		bar = progressbar.DefaultBytes(totalSize, filepath.Base(outPath))
	}

	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()

	sem := make(chan struct{}, e.opts.Concurrency)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

downloadLoop:
	for i, u := range urls {
		select {
		case <-ctx.Done():
			break downloadLoop
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			segPath := filepath.Join(tmpDir, fmt.Sprintf("seg_%05d", idx))
			err := e.downloadSeg(ctx, url, segPath, headers)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			info, _ := os.Stat(segPath)
			if info != nil {
				if bar != nil {
					bar.Add64(info.Size())
				}
			}
		}(i, u)
	}
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	partPath := outPath + ".part"
	if err := concatFiles(tmpDir, partPath, len(urls)); err != nil {
		os.Remove(partPath)
		return err
	}
	return os.Rename(partPath, outPath)
}

func (e *Engine) downloadSeg(ctx context.Context, url, path string, headers map[string]string) error {
	retries := e.opts.Retries
	if retries <= 0 {
		retries = 3
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if ctx.Err() != nil {
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		}
		if attempt > 0 {
			time.Sleep(time.Duration(1<<(attempt-1)) * time.Second)
		}

		if err := e.downloadSegOnce(ctx, url, path, headers); err != nil {
			lastErr = err
			os.Remove(path)
			os.Remove(path + ".part")
			continue
		}
		return nil
	}

	return fmt.Errorf("segment download failed after %d attempts: %w", retries+1, lastErr)
}

func (e *Engine) downloadSegOnce(ctx context.Context, url, path string, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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

	if e.opts.Verbose {
		fmt.Fprintf(os.Stderr, "[debug] GET %s -> %d (Content-Length: %d)\n", url, resp.StatusCode, resp.ContentLength)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("segment HTTP %d: %s", resp.StatusCode, url)
	}

	partPath := path + ".part"
	f, err := os.Create(partPath)
	if err != nil {
		return err
	}

	var body io.Reader = resp.Body
	if e.limitRate > 0 {
		body = newRateLimitedReader(body, e.limitRate)
	}

	_, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(partPath)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(partPath)
		return closeErr
	}

	return os.Rename(partPath, path)
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

func subtitleExt(sub extractor.Subtitle) string {
	format := strings.Trim(strings.TrimSpace(sub.Format), ".")
	if format == "" {
		if u, err := url.Parse(sub.URL); err == nil {
			format = strings.TrimPrefix(filepath.Ext(u.Path), ".")
		}
	}
	if format == "" {
		format = "srt"
	}
	return util.SanitizeFilename(format)
}

func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
