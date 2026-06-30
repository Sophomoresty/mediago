package download

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

func (e *Engine) downloadHLS(filename string, stream extractor.Stream) (string, error) {
	if len(stream.URLs) == 0 {
		return "", fmt.Errorf("no m3u8 URL")
	}
	if streamURLsAreMirrors(stream) {
		var last error
		perURLHeaders := streamURLHeaders(stream)
		for _, raw := range stream.URLs {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			single := stream
			single.URLs = []string{raw}
			single.Extra = nil
			if uh := perURLHeaders[raw]; len(uh) > 0 {
				single.Headers = uh
			}
			outPath, err := e.downloadHLS(filename, single)
			if err == nil {
				return outPath, nil
			}
			last = err
			if ctxErr := e.ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
		}
		if last != nil {
			return "", last
		}
		return "", fmt.Errorf("no m3u8 URL")
	}

	m3u8URL := stream.URLs[0]
	outPath := filepath.Join(e.opts.OutputDir, filename+e.outputExt())

	if !e.opts.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return outPath, nil
		}
	}

	if e.ffmpeg != "" {
		return outPath, e.hlsViaFFmpeg(m3u8URL, outPath, stream.Headers)
	}

	segments, err := e.parseM3U8Segments(m3u8URL, stream.Headers)
	if err != nil {
		return "", err
	}
	tsPath := outPath + ".ts"
	if hasEncryptedHLSSegments(segments) {
		if err := e.downloadHLSSegments(segments, tsPath, stream.Headers); err != nil {
			return "", err
		}
		if err := os.Rename(tsPath, outPath); err != nil {
			return "", err
		}
		return outPath, nil
	}

	urls := make([]string, 0, len(segments))
	for _, seg := range segments {
		urls = append(urls, seg.URL)
	}
	tsStream := extractor.Stream{
		URLs:    urls,
		Format:  "ts",
		Headers: stream.Headers,
	}
	_, err = e.downloadDirect(filename+".mp4", tsStream)
	if err != nil {
		return "", err
	}
	if err := os.Rename(filepath.Join(e.opts.OutputDir, filename+".mp4.ts"), tsPath); err != nil {
		return "", err
	}
	if err := os.Rename(tsPath, outPath); err != nil {
		return "", err
	}
	return outPath, nil
}

func (e *Engine) hlsViaFFmpeg(m3u8URL, outPath string, headers map[string]string) error {
	os.MkdirAll(filepath.Dir(outPath), 0o755)

	partPath := outPath + ".part"
	_ = os.Remove(partPath)

	args := []string{"-y", "-protocol_whitelist", "file,http,https,tcp,tls,crypto,data"}
	if proxy := ffmpegHTTPProxyURL(); proxy != "" {
		args = append(args, "-http_proxy", proxy)
	}
	if len(headers) > 0 {
		var hdr []string
		for k, v := range headers {
			hdr = append(hdr, fmt.Sprintf("%s: %s", k, v))
		}
		args = append(args, "-headers", strings.Join(hdr, "\r\n"))
	}
	args = append(args, "-i", m3u8URL, "-c", "copy", "-f", "mp4", partPath)

	cmd := exec.CommandContext(e.ctx, e.ffmpeg, args...)
	if env := ffmpegEnv(); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := runFFmpeg(cmd); err != nil {
		_ = os.Remove(partPath)
		return err
	}
	return os.Rename(partPath, outPath)
}

func (e *Engine) parseM3U8(m3u8URL string, headers map[string]string) ([]string, error) {
	segments, err := e.parseM3U8Segments(m3u8URL, headers)
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(segments))
	for _, seg := range segments {
		urls = append(urls, seg.URL)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no segments found in m3u8")
	}
	return urls, nil
}

type hlsSegment struct {
	URL string
	Key []byte
	IV  []byte
}

const maxM3U8Depth = 3

func (e *Engine) parseM3U8Segments(m3u8URL string, headers map[string]string) ([]hlsSegment, error) {
	return e.parseM3U8SegmentsAt(m3u8URL, headers, 0)
}

func (e *Engine) parseM3U8SegmentsAt(m3u8URL string, headers map[string]string, depth int) ([]hlsSegment, error) {
	if depth >= maxM3U8Depth {
		return nil, fmt.Errorf("m3u8 recursion depth exceeded (%d)", maxM3U8Depth)
	}

	body, err := e.readM3U8Text(m3u8URL, headers)
	if err != nil {
		return nil, err
	}
	baseURL := m3u8URL
	if strings.HasPrefix(strings.ToLower(m3u8URL), "data:") {
		baseURL = ""
	} else if idx := strings.LastIndex(m3u8URL, "/"); idx >= 0 {
		baseURL = m3u8URL[:idx+1]
	}
	var segments []hlsSegment
	var currentKey []byte
	var currentIV []byte
	segSeq := 0

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"))); err == nil {
				segSeq = n
			}
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			attrs := parseM3U8Attrs(strings.TrimPrefix(line, "#EXT-X-KEY:"))
			if strings.EqualFold(attrs["METHOD"], "NONE") {
				currentKey, currentIV = nil, nil
				continue
			}
			if strings.EqualFold(attrs["METHOD"], "AES-128") {
				key, err := e.resolveM3U8Key(attrs["URI"], baseURL, headers)
				if err != nil {
					return nil, err
				}
				currentKey = key
				currentIV = parseHLSIV(attrs["IV"])
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		iv := currentIV
		if len(currentKey) > 0 && len(iv) == 0 {
			iv = mediaSequenceIV(segSeq)
		}
		segments = append(segments, hlsSegment{URL: resolveM3U8URI(line, baseURL), Key: currentKey, IV: iv})
		segSeq++
	}

	if len(segments) == 0 {
		if strings.Contains(body, "#EXT-X-STREAM-INF") {
			variantURL, err := selectBestVariant(body, baseURL)
			if err != nil {
				return nil, err
			}
			return e.parseM3U8SegmentsAt(variantURL, headers, depth+1)
		}
		return nil, fmt.Errorf("no segments found in m3u8")
	}
	return segments, nil
}

// selectBestVariant parses a master playlist and returns the URL of the variant
// with the highest BANDWIDTH.
func selectBestVariant(body, baseURL string) (string, error) {
	var bestBW int64
	var bestURL string
	inStreamInf := false

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			attrs := parseM3U8Attrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			bw, _ := strconv.ParseInt(attrs["BANDWIDTH"], 10, 64)
			if bw >= bestBW || bestURL == "" {
				bestBW = bw
				inStreamInf = true
			} else {
				inStreamInf = false
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if inStreamInf {
			bestURL = resolveM3U8URI(line, baseURL)
			inStreamInf = false
		}
	}
	if bestURL == "" {
		return "", fmt.Errorf("no variant streams found in master playlist")
	}
	return bestURL, nil
}

func (e *Engine) readM3U8Text(raw string, headers map[string]string) (string, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:") {
		data, err := decodeDataURLBytes(raw)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return e.client.GetString(raw, headers)
}

func parseM3U8Attrs(raw string) map[string]string {
	out := map[string]string{}
	var parts []string
	var cur strings.Builder
	inQuote := false
	for _, r := range raw {
		switch r {
		case '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case ',':
			if inQuote {
				cur.WriteRune(r)
			} else {
				parts = append(parts, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	for _, part := range parts {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.ToUpper(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	return out
}

func (e *Engine) resolveM3U8Key(rawURI, baseURL string, headers map[string]string) ([]byte, error) {
	if rawURI == "" {
		return nil, fmt.Errorf("m3u8 AES-128 key URI missing")
	}
	if key, ok, err := decodeInlineHexKey(rawURI); ok || err != nil {
		return key, err
	}
	keyURL := resolveM3U8URI(rawURI, baseURL)
	if key, ok, err := decodeInlineHexKey(keyURL); ok || err != nil {
		return key, err
	}
	if strings.HasPrefix(strings.ToLower(keyURL), "data:") {
		return decodeDataURLBytes(keyURL)
	}
	req, err := http.NewRequest("GET", keyURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", util.RandomUA())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("key HTTP %d: %s", resp.StatusCode, keyURL)
	}
	return io.ReadAll(resp.Body)
}

func decodeInlineHexKey(raw string) ([]byte, bool, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "0x") {
		return nil, false, nil
	}
	if len(raw) <= 2 {
		return nil, true, fmt.Errorf("empty inline hex AES-128 key")
	}
	key, err := hex.DecodeString(strings.TrimSpace(raw[2:]))
	if err != nil {
		return nil, true, fmt.Errorf("invalid inline hex AES-128 key: %w", err)
	}
	return key, true, nil
}

func resolveM3U8URI(raw, baseURL string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") || strings.HasPrefix(strings.ToLower(raw), "0x") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if baseURL == "" {
		return raw
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		if strings.HasSuffix(baseURL, "/") {
			return baseURL + strings.TrimLeft(raw, "/")
		}
		return baseURL + "/" + strings.TrimLeft(raw, "/")
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func parseHLSIV(raw string) []byte {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "0x"))
	if raw == "" {
		return nil
	}
	iv, err := hex.DecodeString(raw)
	if err != nil {
		return nil
	}
	if len(iv) == 16 {
		return iv
	}
	if len(iv) > 16 {
		return iv[len(iv)-16:]
	}
	out := make([]byte, 16)
	copy(out[16-len(iv):], iv)
	return out
}

func mediaSequenceIV(seq int) []byte {
	iv := make([]byte, 16)
	binary.BigEndian.PutUint64(iv[8:], uint64(seq))
	return iv
}

func decodeDataURLBytes(raw string) ([]byte, error) {
	comma := strings.Index(raw, ",")
	if !strings.HasPrefix(strings.ToLower(raw), "data:") || comma < 0 {
		return nil, fmt.Errorf("invalid data URL")
	}
	meta, payload := raw[5:comma], raw[comma+1:]
	if strings.Contains(strings.ToLower(meta), ";base64") {
		return base64.StdEncoding.DecodeString(payload)
	}
	decoded, err := url.PathUnescape(payload)
	if err != nil {
		return nil, err
	}
	return []byte(decoded), nil
}

func hasEncryptedHLSSegments(segments []hlsSegment) bool {
	for _, seg := range segments {
		if len(seg.Key) > 0 {
			return true
		}
	}
	return false
}

func (e *Engine) downloadHLSSegments(segments []hlsSegment, outPath string, headers map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "mediago-hls-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()

	sem := make(chan struct{}, e.opts.Concurrency)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

downloadLoop:
	for i, seg := range segments {
		select {
		case <-ctx.Done():
			break downloadLoop
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(idx int, seg hlsSegment) {
			defer wg.Done()
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			segPath := filepath.Join(tmpDir, fmt.Sprintf("seg_%05d", idx))
			if err := e.downloadHLSSegment(ctx, seg, segPath, headers); err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}(i, seg)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	partPath := outPath + ".part"
	if err := concatFiles(tmpDir, partPath, len(segments)); err != nil {
		os.Remove(partPath)
		return err
	}
	return os.Rename(partPath, outPath)
}

func (e *Engine) downloadHLSSegment(ctx context.Context, seg hlsSegment, outPath string, headers map[string]string) error {
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

		if err := e.downloadHLSSegmentOnce(ctx, seg, outPath, headers); err != nil {
			lastErr = err
			os.Remove(outPath)
			os.Remove(outPath + ".part")
			continue
		}
		return nil
	}

	return fmt.Errorf("HLS segment download failed after %d attempts: %w", retries+1, lastErr)
}

func (e *Engine) downloadHLSSegmentOnce(ctx context.Context, seg hlsSegment, outPath string, headers map[string]string) error {
	var data []byte
	var err error
	if strings.HasPrefix(strings.ToLower(seg.URL), "data:") {
		data, err = decodeDataURLBytes(seg.URL)
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, "GET", seg.URL, nil)
		if reqErr != nil {
			return reqErr
		}
		req.Header.Set("User-Agent", util.RandomUA())
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, reqErr := e.http.Do(req)
		if reqErr != nil {
			return reqErr
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("segment HTTP %d: %s", resp.StatusCode, seg.URL)
		}
		data, err = io.ReadAll(resp.Body)
	}
	if err != nil {
		return err
	}
	if len(seg.Key) > 0 {
		data, err = decryptHLSSegment(data, seg.Key, seg.IV)
		if err != nil {
			return err
		}
	}
	partPath := outPath + ".part"
	if err := os.WriteFile(partPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(partPath, outPath)
}

func decryptHLSSegment(data, key, iv []byte) ([]byte, error) {
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("invalid AES IV length %d", len(iv))
	}
	if plain, err := util.AESDecryptCBC(data, key, iv); err == nil {
		return plain, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext is not a multiple of block size")
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	return out, nil
}

func (e *Engine) downloadDASH(filename string, stream extractor.Stream) (string, error) {
	if !e.HasFFmpeg() {
		return "", fmt.Errorf("ffmpeg required for DASH streams")
	}
	if len(stream.URLs) == 0 {
		return "", fmt.Errorf("no video URL for DASH")
	}

	outPath := filepath.Join(e.opts.OutputDir, filename+e.outputExt())
	if !e.opts.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return outPath, nil
		}
	}

	os.MkdirAll(filepath.Dir(outPath), 0o755)
	tmpDir, err := os.MkdirTemp("", "mediago-dash-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "video.mp4")
	audioPath := filepath.Join(tmpDir, "audio.m4a")

	tmpEngine := &Engine{
		opts:   Opts{Concurrency: e.opts.Concurrency, OutputDir: tmpDir, Overwrite: true, Retries: e.opts.Retries, NoProgress: e.opts.NoProgress},
		ffmpeg: e.ffmpeg,
		client: e.client,
		http:   e.http,
		ctx:    e.ctx,
	}

	videoStream := extractor.Stream{URLs: stream.URLs, Headers: stream.Headers, Format: "mp4"}
	if _, err := tmpEngine.downloadDirect("video", videoStream); err != nil {
		return "", fmt.Errorf("download video: %w", err)
	}
	os.Rename(filepath.Join(tmpDir, "video.mp4"), videoPath)

	hasAudio := stream.AudioURL != ""
	if hasAudio {
		audioStream := extractor.Stream{URLs: []string{stream.AudioURL}, Headers: stream.Headers, Format: "m4a"}
		if _, err := tmpEngine.downloadDirect("audio", audioStream); err != nil {
			return "", fmt.Errorf("download audio: %w", err)
		}
		os.Rename(filepath.Join(tmpDir, "audio.m4a"), audioPath)
	}

	return outPath, e.muxDASH(videoPath, audioPath, outPath, hasAudio)
}

func streamURLHeaders(stream extractor.Stream) map[string]map[string]string {
	if stream.Extra == nil {
		return nil
	}
	raw, ok := stream.Extra["url_headers"]
	if !ok {
		return nil
	}
	out := map[string]map[string]string{}
	switch v := raw.(type) {
	case map[string]map[string]string:
		for u, h := range v {
			out[u] = h
		}
	case map[string]any:
		for u, hv := range v {
			switch m := hv.(type) {
			case map[string]string:
				out[u] = m
			case map[string]any:
				h := map[string]string{}
				for k, val := range m {
					if s, ok := val.(string); ok {
						h[k] = s
					}
				}
				if len(h) > 0 {
					out[u] = h
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (e *Engine) muxDASH(videoPath, audioPath, outPath string, hasAudio bool) error {
	partPath := outPath + ".part"
	_ = os.Remove(partPath)

	args := []string{"-y", "-i", videoPath}
	if hasAudio {
		args = append(args, "-i", audioPath)
	}
	args = append(args, "-c", "copy", "-f", "mp4", partPath)
	cmd := exec.CommandContext(e.ctx, e.ffmpeg, args...)
	if env := ffmpegEnv(); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := runFFmpeg(cmd); err != nil {
		_ = os.Remove(partPath)
		return err
	}
	return os.Rename(partPath, outPath)
}

func SelectBestStream(streams map[string]extractor.Stream, preferred string) (string, extractor.Stream) {
	if len(streams) == 0 {
		return "", extractor.Stream{}
	}
	keys := make([]string, 0, len(streams))
	for k := range streams {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if preferred == "worst" {
		priorities := []string{"360p", "480p", "720p", "1080p"}
		for _, q := range priorities {
			for _, k := range keys {
				if streams[k].Quality == q {
					return k, streams[k]
				}
			}
		}
		return keys[0], streams[keys[0]]
	}

	if preferred != "" && preferred != "best" {
		for _, k := range keys {
			if streams[k].Quality == preferred {
				return k, streams[k]
			}
		}
	}
	priorities := []string{"1080p", "720p", "480p", "360p"}
	for _, q := range priorities {
		for _, k := range keys {
			if streams[k].Quality == q {
				return k, streams[k]
			}
		}
	}
	return keys[0], streams[keys[0]]
}
