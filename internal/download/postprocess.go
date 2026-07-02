package download

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

// PostProcessOpts controls post-processing steps after download.
type PostProcessOpts struct {
	EmbedSubs      bool
	EmbedThumbnail bool
	EmbedMetadata  bool
	RecodeVideo    string // target format: "mp4", "mkv", "webm"
}

// PostProcess runs enabled post-processing steps on a downloaded video file.
// It modifies the file in-place (using .tmp intermediaries) and returns the
// final output path (which may differ if recoding changed the extension).
func (e *Engine) PostProcess(videoPath string, info *extractor.MediaInfo, subtitlePaths []string, opts PostProcessOpts) (string, error) {
	if !e.HasFFmpeg() {
		return videoPath, nil
	}

	var err error

	if opts.EmbedSubs && len(subtitlePaths) > 0 {
		videoPath, err = e.embedSubtitles(videoPath, subtitlePaths)
		if err != nil {
			return videoPath, fmt.Errorf("embed subtitles: %w", err)
		}
	}

	if opts.EmbedThumbnail {
		thumbURL := thumbnailURL(info)
		if thumbURL != "" {
			videoPath, err = e.embedThumbnail(videoPath, thumbURL)
			if err != nil {
				return videoPath, fmt.Errorf("embed thumbnail: %w", err)
			}
		}
	}

	if opts.EmbedMetadata {
		videoPath, err = e.embedMetadata(videoPath, info)
		if err != nil {
			return videoPath, fmt.Errorf("embed metadata: %w", err)
		}
	}

	if opts.RecodeVideo != "" {
		videoPath, err = e.recodeVideo(videoPath, opts.RecodeVideo)
		if err != nil {
			return videoPath, fmt.Errorf("recode video: %w", err)
		}
	}

	return videoPath, nil
}

// embedSubtitles muxes subtitle files into the video container.
func (e *Engine) embedSubtitles(videoPath string, subtitlePaths []string) (string, error) {
	tmpPath := videoPath + ".tmp"
	defer os.Remove(tmpPath)

	args := []string{"-y", "-i", videoPath}
	for _, sp := range subtitlePaths {
		args = append(args, "-i", sp)
	}
	args = append(args, "-c", "copy", "-c:s", "mov_text")
	for i, sp := range subtitlePaths {
		lang := langFromSubtitlePath(sp)
		args = append(args, fmt.Sprintf("-metadata:s:s:%d", i), fmt.Sprintf("language=%s", lang))
	}
	args = append(args, tmpPath)

	cmd := exec.CommandContext(e.ctx, e.ffmpeg, args...)
	if env := ffmpegEnv(); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := runFFmpeg(cmd); err != nil {
		return videoPath, err
	}
	if err := os.Rename(tmpPath, videoPath); err != nil {
		return videoPath, err
	}
	return videoPath, nil
}

// embedThumbnail downloads the thumbnail and attaches it as cover art.
func (e *Engine) embedThumbnail(videoPath string, thumbURL string) (string, error) {
	// Download thumbnail to temp file
	thumbPath := videoPath + ".thumb.jpg"
	defer os.Remove(thumbPath)

	if err := e.downloadSingle(thumbURL, thumbPath, nil, 0); err != nil {
		// Non-fatal: skip thumbnail embedding if download fails
		return videoPath, nil
	}

	tmpPath := videoPath + ".tmp"
	defer os.Remove(tmpPath)

	args := []string{"-y", "-i", videoPath, "-i", thumbPath,
		"-map", "0", "-map", "1",
		"-c", "copy",
		"-disposition:v:1", "attached_pic",
		tmpPath,
	}

	cmd := exec.CommandContext(e.ctx, e.ffmpeg, args...)
	if env := ffmpegEnv(); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := runFFmpeg(cmd); err != nil {
		return videoPath, err
	}
	if err := os.Rename(tmpPath, videoPath); err != nil {
		return videoPath, err
	}
	return videoPath, nil
}

// embedMetadata writes title/artist/date metadata into the container.
func (e *Engine) embedMetadata(videoPath string, info *extractor.MediaInfo) (string, error) {
	tmpPath := videoPath + ".tmp"
	defer os.Remove(tmpPath)

	args := []string{"-y", "-i", videoPath}
	if info.Title != "" {
		args = append(args, "-metadata", fmt.Sprintf("title=%s", info.Title))
	}
	if info.Artist != "" {
		args = append(args, "-metadata", fmt.Sprintf("artist=%s", info.Artist))
	}
	if date := metadataDate(info); date != "" {
		args = append(args, "-metadata", fmt.Sprintf("date=%s", date))
	}
	args = append(args, "-c", "copy", tmpPath)

	cmd := exec.CommandContext(e.ctx, e.ffmpeg, args...)
	if env := ffmpegEnv(); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := runFFmpeg(cmd); err != nil {
		return videoPath, err
	}
	if err := os.Rename(tmpPath, videoPath); err != nil {
		return videoPath, err
	}
	return videoPath, nil
}

// recodeVideo re-encodes the video to a different container/codec.
func (e *Engine) recodeVideo(videoPath string, targetFormat string) (string, error) {
	targetFormat = strings.ToLower(strings.TrimSpace(targetFormat))
	if targetFormat == "" {
		return videoPath, nil
	}

	currentExt := strings.TrimPrefix(filepath.Ext(videoPath), ".")
	if strings.EqualFold(currentExt, targetFormat) {
		return videoPath, nil
	}

	outPath := strings.TrimSuffix(videoPath, filepath.Ext(videoPath)) + "." + targetFormat
	tmpPath := outPath + ".tmp"
	defer os.Remove(tmpPath)

	args := []string{"-y", "-i", videoPath, "-c:v", "libx264", "-c:a", "aac", tmpPath}

	cmd := exec.CommandContext(e.ctx, e.ffmpeg, args...)
	if env := ffmpegEnv(); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := runFFmpeg(cmd); err != nil {
		return videoPath, err
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return videoPath, err
	}
	// Remove original file since format changed
	if outPath != videoPath {
		os.Remove(videoPath)
	}
	return outPath, nil
}

// thumbnailURL extracts thumbnail URL from MediaInfo.Extra.
func thumbnailURL(info *extractor.MediaInfo) string {
	if info == nil || info.Extra == nil {
		return ""
	}
	for _, key := range []string{"thumbnail", "thumbnail_url", "cover", "cover_url"} {
		if v, ok := info.Extra[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// metadataDate extracts a date string from MediaInfo.Extra.
func metadataDate(info *extractor.MediaInfo) string {
	if info == nil || info.Extra == nil {
		return ""
	}
	for _, key := range []string{"date", "upload_date", "publish_date"} {
		if v, ok := info.Extra[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// langFromSubtitlePath extracts language code from subtitle filename.
// Expected format: video.zho.srt -> "zho"
func langFromSubtitlePath(path string) string {
	base := filepath.Base(path)
	// Remove extension (e.g. ".srt")
	base = strings.TrimSuffix(base, filepath.Ext(base))
	// Get last dot-separated part as language
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		lang := base[idx+1:]
		if lang != "" {
			return lang
		}
	}
	return "und"
}
