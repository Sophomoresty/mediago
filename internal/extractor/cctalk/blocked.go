package cctalk

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

// blockedError marks a lesson that cannot be produced via any REST API. It
// carries a human-readable reason and the classified playback type so callers
// can surface a clear explanation instead of a generic "missing URL" failure.
type blockedError struct {
	reason       string
	playbackType string
}

func (e *blockedError) Error() string { return e.reason }

func asBlocked(err error) (*blockedError, bool) {
	var b *blockedError
	if errors.As(err, &b) {
		return b, true
	}
	return nil, false
}

// boardLocalRenderReason explains why a whiteboard ("board") lesson without a
// streamable media URL cannot be downloaded. Such lessons ship only the
// whiteboard XML (normalPage/whiteboard elements + per-stroke draw events) plus
// separate audio segments; the source player reconstructs the video frame by
// frame with OpenCV polyline drawing and Windows GDI text, then muxes with
// ffmpeg. There is no server-side rendered mp4/m3u8 for this case.
//
// Source: Cctalk_Local.pyc.1shot.cdc.py
//   - _download_cctalk_board_playback:3737 (entered only when no streamable
//     fallback exists; builds resource model from whiteboard XML pages)
//   - _render_cctalk_board_video:2118 returns False at :2122 when cv2/numpy are
//     unavailable (`if cv2 is None or np is None`)
//   - _draw_cctalk_board_polyline:1485 (cv2.polylines :1514)
//   - _cctalk_measure_text_gdi:1583 (ctypes.windll.user32/gdi32 :1594-1595)
//   - mux via Mooc_Render.mux_render_mp4 (ffmpeg) :3920
// The m3u8-backed board case is NOT blocked: when the courseware payload
// carries `m3u8s`, _prefer_v55_board_ocs_info:3223 returns the v55 ocs_info and
// playback resolves to a normal m3u8 stream (Cctalk_Course :3240, :3214).
const boardLocalRenderReason = "cctalk 板书课时需要本地 OpenCV 渲染白板笔迹合成视频, 无服务端预渲染视频可下载 (源码 Cctalk_Local.pyc.1shot.cdc.py:2122 cv2 缺失即返回 False, :1514 cv2.polylines 逐笔绘制, :3920 ffmpeg 合成)"

// liveReplayUnavailableReason explains why a live lesson has no downloadable
// media yet. CCtalk live playback reuses the exact same /video/play + OCS
// resolution path as VOD; there is no WebSocket video capture and no separate
// recording API. Before a live session's replay is published it exposes no
// videoUrl / userSign / OCS media / duration, only forecast/scheduling fields.
//
// Source: Cctalk_Course.pyc.1shot.cdc.py
//   - _is_unavailable_replay:3067 (no media url :3076, no userSign/OCS :3078,
//     no duration :3080, gated by forecastStartDate + reviewStatus 0/'' :3089)
//   - download dispatch reuses _get_video_play_info + OCS for live and VOD
//     alike (:3621, :3653, :3662); only :3636 skips an unavailable replay.
// The IM/login WebSocket (_message_gateway_socket, Cctalk_Base:79) is unrelated
// to video; no ws://wss:// video stream exists in the source.
const liveReplayUnavailableReason = "cctalk 直播课时回放尚未生成, 暂无可下载媒体 (源码 Cctalk_Course.pyc.1shot.cdc.py:3067 _is_unavailable_replay: 无 videoUrl/userSign/OCS/时长, forecastStartDate 存在且 reviewStatus 为 0/空)"

// blockedEntry builds a stream-less MediaInfo that records why a lesson cannot
// be downloaded, so list/playlist output surfaces the reason instead of
// silently dropping the item.
func blockedEntry(title string, b *blockedError) *extractor.MediaInfo {
	return &extractor.MediaInfo{
		Site:  "cctalk",
		Title: util.SanitizeFilename(title),
		Extra: map[string]any{
			"blocked":       true,
			"block_reason":  b.reason,
			"playback_type": b.playbackType,
		},
	}
}

// classifyBlocked decides why an item that yielded no playable URL and no
// resolvable OCS stream is unavailable: a local-render-only whiteboard lesson,
// an unavailable live replay, or a generic missing-media failure.
func classifyBlocked(item map[string]any) error {
	if isBoardItem(item) {
		return &blockedError{reason: boardLocalRenderReason, playbackType: "board"}
	}
	if isUnavailableReplay(item) {
		return &blockedError{reason: liveReplayUnavailableReason, playbackType: "live"}
	}
	return fmt.Errorf("cctalk media URL missing")
}

// isBoardItem reports whether the lesson is a whiteboard ("board") playback.
// Mirrors the source classification which keys off contentType/sourceType and
// the presence of board/whiteboard markers in the payload
// (Cctalk_Local _classify_cctalk_ocs_media_type:420, _is_cctalk_board_payload:296).
func isBoardItem(item map[string]any) bool {
	for _, key := range []string{"playback_type", "sourceType", "source_type", "contentType", "content_type", "type"} {
		lower := strings.ToLower(strings.TrimSpace(textValue(item, key)))
		if lower == "board" || lower == "whiteboard" || strings.Contains(lower, "board") {
			return true
		}
	}
	return isBoardPayload(item)
}

// isUnavailableReplay reports whether a live lesson's replay has not been
// published yet. Mirrors _is_unavailable_replay (Cctalk_Course:3067): the item
// has no playable media URL, no userSign, and exposes a forecast window with a
// reviewStatus of 0 or empty.
func isUnavailableReplay(item map[string]any) bool {
	if normalizeMediaURL(findMediaURL(item)) != "" {
		return false
	}
	if textValue(extractCoursewareInfo(item), "userSign") != "" {
		return false
	}
	hasForecast := firstNonEmpty(
		textValue(item, "forecastStartDate", "forecast_start_date"),
		textValue(item, "forecastEndDate", "forecast_end_date"),
	) != ""
	if !hasForecast {
		return false
	}
	review := strings.TrimSpace(firstNonEmpty(textValue(item, "reviewStatus", "review_status")))
	return review == "0" || review == ""
}
