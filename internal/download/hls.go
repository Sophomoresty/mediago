package download

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
)

func (e *Engine) downloadHLS(filename string, stream extractor.Stream) (string, error) {
	if len(stream.URLs) == 0 {
		return "", fmt.Errorf("no m3u8 URL")
	}

	m3u8URL := stream.URLs[0]
	outPath := filepath.Join(e.opts.OutputDir, filename+".mp4")

	if !e.opts.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return outPath, nil
		}
	}

	if e.ffmpeg != "" {
		return outPath, e.hlsViaFFmpeg(m3u8URL, outPath, stream.Headers)
	}

	segments, err := e.parseM3U8(m3u8URL, stream.Headers)
	if err != nil {
		return "", err
	}

	tsStream := extractor.Stream{
		URLs:    segments,
		Format:  "ts",
		Headers: stream.Headers,
	}
	tsPath := outPath + ".ts"
	_, err = e.downloadDirect(filename+".mp4", tsStream)
	if err != nil {
		return "", err
	}
	os.Rename(filepath.Join(e.opts.OutputDir, filename+".mp4.ts"), tsPath)
	os.Rename(tsPath, outPath)
	return outPath, nil
}

func (e *Engine) hlsViaFFmpeg(m3u8URL, outPath string, headers map[string]string) error {
	os.MkdirAll(filepath.Dir(outPath), 0o755)

	args := []string{"-y"}
	if len(headers) > 0 {
		var hdr []string
		for k, v := range headers {
			hdr = append(hdr, fmt.Sprintf("%s: %s", k, v))
		}
		args = append(args, "-headers", strings.Join(hdr, "\r\n"))
	}
	args = append(args, "-i", m3u8URL, "-c", "copy", outPath)

	cmd := exec.Command(e.ffmpeg, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (e *Engine) parseM3U8(m3u8URL string, headers map[string]string) ([]string, error) {
	body, err := e.client.GetString(m3u8URL, headers)
	if err != nil {
		return nil, err
	}

	baseURL := m3u8URL
	if idx := strings.LastIndex(m3u8URL, "/"); idx >= 0 {
		baseURL = m3u8URL[:idx+1]
	}
	var segments []string

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "http") {
			segments = append(segments, line)
		} else {
			segments = append(segments, baseURL+line)
		}
	}

	if len(segments) == 0 {
		return nil, fmt.Errorf("no segments found in m3u8")
	}
	return segments, nil
}

func (e *Engine) downloadDASH(filename string, stream extractor.Stream) (string, error) {
	if !e.HasFFmpeg() {
		return "", fmt.Errorf("ffmpeg required for DASH streams")
	}
	if len(stream.URLs) == 0 {
		return "", fmt.Errorf("no video URL for DASH")
	}

	outPath := filepath.Join(e.opts.OutputDir, filename+".mp4")
	if !e.opts.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return outPath, nil
		}
	}

	os.MkdirAll(filepath.Dir(outPath), 0o755)
	tmpDir, err := os.MkdirTemp("", "medigo-dash-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	videoPath := filepath.Join(tmpDir, "video.mp4")
	audioPath := filepath.Join(tmpDir, "audio.m4a")

	tmpEngine := &Engine{
		opts:   Opts{Concurrency: e.opts.Concurrency, OutputDir: tmpDir, Overwrite: true, Retries: e.opts.Retries},
		ffmpeg: e.ffmpeg,
		client: e.client,
		http:   e.http,
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

func (e *Engine) muxDASH(videoPath, audioPath, outPath string, hasAudio bool) error {
	args := []string{"-y", "-i", videoPath}
	if hasAudio {
		args = append(args, "-i", audioPath)
	}
	args = append(args, "-c", "copy", outPath)
	cmd := exec.Command(e.ffmpeg, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func SelectBestStream(streams map[string]extractor.Stream, preferred string) (string, extractor.Stream) {
	if preferred == "worst" {
		priorities := []string{"360p", "480p", "720p", "1080p"}
		for _, q := range priorities {
			for k, s := range streams {
				if s.Quality == q {
					return k, s
				}
			}
		}
		for k, s := range streams {
			return k, s
		}
		return "", extractor.Stream{}
	}

	if preferred != "" && preferred != "best" {
		for k, s := range streams {
			if s.Quality == preferred {
				return k, s
			}
		}
	}
	priorities := []string{"1080p", "720p", "480p", "360p"}
	for _, q := range priorities {
		for k, s := range streams {
			if s.Quality == q {
				return k, s
			}
		}
	}
	for k, s := range streams {
		return k, s
	}
	return "", extractor.Stream{}
}
