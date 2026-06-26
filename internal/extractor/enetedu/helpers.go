package enetedu

import (
	"fmt"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

func apiURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return api_base + path
}

func dataOf(payload any) map[string]any {
	return dataOfAny(payload)
}

func dataOfAny(payload any) map[string]any {
	if m, ok := payload.(map[string]any); ok {
		if d, ok := m["data"].(map[string]any); ok {
			return d
		}
		return m
	}
	return map[string]any{}
}

func walkLivePayload(v any, out *[]videoInfo) {
	switch x := v.(type) {
	case map[string]any:
		title := valueString(x, "name", "title", "realId", "id")
		nodeID := valueString(x, "realId", "id", "node_id")
		media := firstNonEmpty(valueString(x, "playbackUrl", "sourceAddress", "url"), findMediaURL(x))
		if nodeID != "" || isMediaURL(media) {
			*out = append(*out, videoInfo{Title: title, NodeID: nodeID, URL: normalizeURL(media), Raw: x})
		}
		for _, k := range []string{"children", "childList", "list", "data", "records"} {
			if child, ok := x[k]; ok {
				walkLivePayload(child, out)
			}
		}
	case []any:
		for _, child := range x {
			walkLivePayload(child, out)
		}
	}
}

func walkLearningPayload(v any, out *[]videoInfo) {
	switch x := v.(type) {
	case map[string]any:
		title := valueString(x, "fileName", "mediaName", "chapterName", "courseName", "name")
		videoID := valueString(x, "videoId", "mediaId")
		media := firstNonEmpty(valueString(x, "filePath", "playUrl", "url"), findMediaURL(x))
		if videoID != "" || isMediaURL(media) {
			*out = append(*out, videoInfo{Title: title, VideoID: videoID, URL: normalizeURL(media), FileName: valueString(x, "fileName"), ChapterID: valueString(x, "chapterId", "id"), Raw: x})
		}
		for _, k := range []string{"children", "childList", "list", "data", "records"} {
			if child, ok := x[k]; ok {
				walkLearningPayload(child, out)
			}
		}
	case []any:
		for _, child := range x {
			walkLearningPayload(child, out)
		}
	}
}

func findMediaURL(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"playUrl", "url", "filePath", "sourceAddress", "playbackUrl", "downloadUrl", "resourceUrl", "fileUrl"} {
			if s := normalizeURL(valueString(x, k)); isMediaURL(s) {
				return s
			}
		}
		for _, child := range x {
			if s := findMediaURL(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := findMediaURL(child); s != "" {
				return s
			}
		}
	case string:
		if s := normalizeURL(x); isMediaURL(s) {
			return s
		}
	}
	return ""
}

func mediaInfo(title, mediaURL string, headers map[string]string) *extractor.MediaInfo {
	format := "mp4"
	low := strings.ToLower(mediaURL)
	if strings.Contains(low, ".m3u8") {
		format = "m3u8"
	} else if strings.Contains(low, ".mp3") || strings.Contains(low, ".m4a") || strings.Contains(low, ".aac") || strings.Contains(low, ".wav") {
		format = "audio"
	}
	return &extractor.MediaInfo{Site: "enetedu", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{mediaURL}, Format: format, Headers: headers}}}
}

func dedupe(in []*extractor.MediaInfo) []*extractor.MediaInfo {
	seen := map[string]bool{}
	out := make([]*extractor.MediaInfo, 0, len(in))
	for _, item := range in {
		if item == nil || len(item.Streams) == 0 {
			continue
		}
		key := item.Title
		for _, st := range item.Streams {
			if len(st.URLs) > 0 {
				key += "|" + st.URLs[0]
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func valueString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func normalizeURL(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	s = strings.ReplaceAll(s, `\/`, `/`)
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	if strings.HasPrefix(s, "/") {
		return origin + s
	}
	return s
}

func isMediaURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "http") && (strings.Contains(low, ".m3u8") || strings.Contains(low, ".mp4") || strings.Contains(low, ".flv") || strings.Contains(low, ".mov") || strings.Contains(low, ".m4v") || strings.Contains(low, ".mp3") || strings.Contains(low, ".m4a") || strings.Contains(low, ".aac") || strings.Contains(low, ".wav"))
}

func cloneHeaders(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
