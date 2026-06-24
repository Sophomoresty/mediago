package cctalk

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
)

func extractCoursewareInfo(item any) map[string]any {
	out := map[string]any{}
	collectCoursewareInfo(item, out, 0)
	if firstNonEmpty(textValue(out, "tenantId"), "") == "" {
		out["tenantId"] = CCTALK_TENANT_ID
	}
	return out
}

func collectCoursewareInfo(value any, out map[string]any, depth int) {
	if value == nil || depth > 7 {
		return
	}
	switch x := value.(type) {
	case map[string]any:
		for _, key := range []string{"coursewareInfo", "courseWareInfo", "courseware_info", "ocsInfo", "videoInfo", "mediaInfo", "contentInfo", "resourceInfo", "playInfo", "activityInfo", "lessonInfo", "detail", "raw"} {
			if nested, ok := x[key]; ok {
				collectCoursewareInfo(nested, out, depth+1)
			}
		}
		for _, pair := range [][2]string{
			{"coursewareId", "coursewareId"}, {"courseWareId", "coursewareId"}, {"courseware_id", "coursewareId"}, {"coursewareID", "coursewareId"}, {"courseId", "coursewareId"}, {"course_id", "coursewareId"}, {"ocsId", "coursewareId"}, {"ocs_id", "coursewareId"},
			{"videoId", "videoId"}, {"video_id", "videoId"}, {"contentId", "videoId"}, {"content_id", "videoId"},
			{"tenantId", "tenantId"}, {"tenantID", "tenantId"}, {"tenant_id", "tenantId"},
			{"sourceType", "sourceType"}, {"source_type", "sourceType"}, {"contentType", "contentType"}, {"content_type", "contentType"},
			{"userSign", "userSign"}, {"user_sign", "userSign"}, {"userSignKey", "userSign"}, {"user_sign_key", "userSign"}, {"xUserSign", "userSign"}, {"signature", "userSign"}, {"sign", "userSign"},
			{"videoUrl", "videoUrl"}, {"playUrl", "videoUrl"}, {"m3u8Url", "videoUrl"}, {"hlsUrl", "videoUrl"}, {"mediaUrl", "videoUrl"}, {"mediaURL", "videoUrl"}, {"mp4URL", "videoUrl"}, {"downloadUrl", "videoUrl"}, {"url", "videoUrl"},
			{"fileUrl", "fileUrl"}, {"fileURL", "fileUrl"}, {"resourceUrl", "fileUrl"}, {"resourceURL", "fileUrl"}, {"materialUrl", "fileUrl"}, {"attachUrl", "fileUrl"},
		} {
			if textValue(out, pair[1]) == "" {
				if value := textValue(x, pair[0]); value != "" {
					out[pair[1]] = value
				}
			}
		}
		for _, nested := range x {
			switch nested.(type) {
			case map[string]any, []any:
				collectCoursewareInfo(nested, out, depth+1)
			}
		}
	case []any:
		for _, item := range x {
			collectCoursewareInfo(item, out, depth+1)
		}
	}
}

func ocsHeadersFor(coursewareInfo map[string]any) map[string]string {
	headers := map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Referer":         CCTALK_BASE_URL + "/",
		"Origin":          CCTALK_BASE_URL,
		"User-Agent":      CCTALK_OCS_USER_AGENT,
		"X-Tenant-Id":     firstNonEmpty(textValue(coursewareInfo, "tenantId"), CCTALK_TENANT_ID),
		"X-Tenant-ID":     firstNonEmpty(textValue(coursewareInfo, "tenantId"), CCTALK_TENANT_ID),
		"X-User-Sign":     textValue(coursewareInfo, "userSign"),
		"Hujiang-App-Key": CCTALK_PCWEB_KEY,
	}
	if headers["X-User-Sign"] == "" {
		delete(headers, "X-User-Sign")
	}
	return headers
}

func (a *apiClient) resolveOCSStream(coursewareInfo map[string]any) (extractor.Stream, map[string]any, bool) {
	coursewareID := textValue(coursewareInfo, "coursewareId")
	if coursewareID == "" || a == nil || a.c == nil {
		return extractor.Stream{}, nil, false
	}
	headers := ocsHeadersFor(coursewareInfo)
	for _, base := range cctalkOCSCurrentBases {
		endpoint := strings.TrimRight(base, "/") + "/courseware_contents/" + url.PathEscape(coursewareID)
		if q := ocsQuery(coursewareInfo); q != "" {
			endpoint += "?" + q
		}
		body, err := a.c.GetString(endpoint, headers)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			continue
		}
		if stream, extra, ok := buildOCSStreamFromPayload(payload, coursewareInfo, headers); ok {
			extra["ocs_url"] = endpoint
			return stream, extra, true
		}
	}
	return extractor.Stream{}, nil, false
}

func ocsQuery(coursewareInfo map[string]any) string {
	q := url.Values{}
	for _, key := range []string{"tenantId", "sourceType", "contentType"} {
		if value := textValue(coursewareInfo, key); value != "" {
			q.Set(key, value)
		}
	}
	return q.Encode()
}

func buildEmbeddedOCSStream(item map[string]any, coursewareInfo map[string]any) (extractor.Stream, map[string]any, bool) {
	return buildOCSStreamFromPayload(item, coursewareInfo, ocsHeadersFor(coursewareInfo))
}

func buildOCSStreamFromPayload(payload any, coursewareInfo map[string]any, headers map[string]string) (extractor.Stream, map[string]any, bool) {
	payload = normalizeOCSPayload(payload)
	if mediaURL := normalizeMediaURL(findMediaURL(payload)); mediaURL != "" {
		format := pickFormat(mediaURL)
		return extractor.Stream{Quality: "best", URLs: []string{mediaURL}, Format: format, Headers: headers, NeedMerge: format == "m3u8"}, map[string]any{"mode": "direct_ocs", "payload": payload}, true
	}
	if item, root, ok := findV55M3U8Item(payload); ok {
		content := textValue(item, "content", "m3u8", "text")
		if decoded := maybeDecodeText(content); decoded != "" {
			content = decoded
		}
		if !strings.HasPrefix(strings.TrimSpace(content), "#EXTM3U") {
			return extractor.Stream{}, nil, false
		}
		host := firstNonEmpty(candidateHosts(root)...)
		if host == "" {
			host = CCTALK_OCS_MATERIAL_HOST
		}
		playlist := rewriteV55M3U8Text(content, host, item)
		resourceID := firstNonEmpty(textValue(item, "resourceId"), textValue(item, "resourceID"), textValue(root, "resourceId"), textValue(root, "resourceID"))
		extra := map[string]any{
			"mode":              "v55",
			"m3u8_resource_id":  resourceID,
			"m3u8_text":         playlist,
			"payload":           payload,
			"decrypted_payload": root,
			"courseware_id":     firstNonEmpty(textValue(coursewareInfo, "coursewareId"), textValue(root, "coursewareId")),
		}
		stream := extractor.Stream{
			Quality:   firstNonEmpty(textValue(item, "quality"), textValue(item, "name"), textValue(item, "label"), "v55"),
			URLs:      []string{dataURL("application/vnd.apple.mpegurl", playlist)},
			Format:    "m3u8",
			Headers:   headers,
			NeedMerge: true,
		}
		return stream, extra, true
	}
	return extractor.Stream{}, nil, false
}

func normalizeOCSPayload(payload any) any {
	switch x := payload.(type) {
	case map[string]any:
		for _, key := range []string{"data", "Data", "result", "Result", "payload"} {
			if nested, ok := x[key]; ok && nested != nil {
				if normalized := normalizeOCSPayload(nested); normalized != nil {
					return normalized
				}
			}
		}
		return x
	case string:
		text := strings.TrimSpace(x)
		if decoded := maybeDecodeText(text); decoded != "" {
			text = decoded
		}
		if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
			var out any
			if json.Unmarshal([]byte(text), &out) == nil {
				return normalizeOCSPayload(out)
			}
		}
		return x
	default:
		return payload
	}
}

func findV55M3U8Item(payload any) (map[string]any, map[string]any, bool) {
	var foundItem map[string]any
	var foundRoot map[string]any
	var walk func(any, map[string]any)
	walk = func(value any, root map[string]any) {
		if foundItem != nil {
			return
		}
		switch x := value.(type) {
		case map[string]any:
			if root == nil {
				root = x
			}
			if content := textValue(x, "content", "m3u8", "text"); strings.HasPrefix(strings.TrimSpace(maybeDecodeFallback(content)), "#EXTM3U") {
				foundItem, foundRoot = x, root
				return
			}
			if list, ok := x["m3u8s"].([]any); ok {
				for _, item := range list {
					if m, ok := item.(map[string]any); ok {
						if content := textValue(m, "content", "m3u8", "text"); strings.HasPrefix(strings.TrimSpace(maybeDecodeFallback(content)), "#EXTM3U") {
							foundItem, foundRoot = m, x
							return
						}
					}
				}
			}
			for _, nested := range x {
				walk(nested, root)
			}
		case []any:
			for _, item := range x {
				walk(item, root)
			}
		}
	}
	walk(payload, nil)
	return foundItem, foundRoot, foundItem != nil
}

func maybeDecodeFallback(text string) string {
	if decoded := maybeDecodeText(text); decoded != "" {
		return decoded
	}
	return text
}

func maybeDecodeText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "#EXTM3U") || strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		return text
	}
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err == nil {
		plain := strings.TrimSpace(string(decoded))
		if strings.HasPrefix(plain, "#EXTM3U") || strings.HasPrefix(plain, "{") || strings.HasPrefix(plain, "[") {
			return plain
		}
	}
	return ""
}

func candidateHosts(payload map[string]any) []string {
	var out []string
	for _, key := range []string{"cdnHosts", "cdn_hosts", "hosts", "host", "cdnHost", "baseUrl", "baseURL", "materialHost"} {
		switch value := payload[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				out = append(out, strings.TrimRight(strings.TrimSpace(value), "/"))
			}
		case []any:
			for _, item := range value {
				if s := strings.TrimSpace(textAny(item)); s != "" {
					out = append(out, strings.TrimRight(s, "/"))
				}
			}
		}
	}
	return out
}

func rewriteV55M3U8Text(content, host string, item map[string]any) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var out []string
	insertedKey := false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if i == 0 && line != "#EXTM3U" {
			out = append(out, "#EXTM3U")
		}
		if line == "" {
			out = append(out, raw)
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-KEY") {
			insertedKey = true
			out = append(out, raw)
			continue
		}
		out = append(out, rewriteM3U8Line(raw, host))
		if i == 0 && !insertedKey {
			if keyLine := v55KeyLine(item); keyLine != "" {
				out = append(out, keyLine)
				insertedKey = true
			}
		}
	}
	return strings.Join(out, "\n")
}

func rewriteM3U8Line(raw, host string) string {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "data:") {
		return raw
	}
	if strings.HasPrefix(line, "//") {
		return "https:" + line
	}
	if host == "" {
		return raw
	}
	if strings.HasPrefix(line, "/") {
		return strings.TrimRight(host, "/") + line
	}
	return strings.TrimRight(host, "/") + "/" + strings.TrimLeft(path.Clean(line), "/")
}

func v55KeyLine(item map[string]any) string {
	keyText := firstNonEmpty(textValue(item, "key"), textValue(item, "cryptor"), textValue(item, "hlsKey"))
	keyBytes := decodeKeyBytes(keyText)
	if len(keyBytes) == 0 {
		return ""
	}
	iv := ivHex(firstNonEmpty(textValue(item, "iv"), textValue(item, "IV")))
	return `#EXT-X-KEY:METHOD=AES-128,URI="data:application/octet-stream;base64,` + base64.StdEncoding.EncodeToString(keyBytes) + `",IV=` + iv
}

func decodeKeyBytes(text string) []byte {
	text = strings.TrimSpace(strings.TrimPrefix(text, "0x"))
	if text == "" {
		return nil
	}
	if decoded, err := hex.DecodeString(text); err == nil && len(decoded) > 0 {
		return decoded
	}
	if decoded, err := base64.StdEncoding.DecodeString(text); err == nil && len(decoded) > 0 {
		return decoded
	}
	return []byte(text)
}

func ivHex(text string) string {
	text = strings.TrimSpace(strings.TrimPrefix(text, "0x"))
	if text == "" {
		return "0x00000000000000000000000000000000"
	}
	if _, err := hex.DecodeString(text); err == nil {
		return "0x" + strings.ToLower(text)
	}
	return "0x" + hex.EncodeToString([]byte(text))
}

func dataURL(mime, content string) string {
	return "data:" + mime + ";charset=utf-8," + url.PathEscape(content)
}

func playbackType(item map[string]any, extra map[string]any) string {
	for _, value := range []string{textValue(item, "playback_type"), textValue(item, "sourceType"), textValue(item, "contentType"), textAny(extra["mode"])} {
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "board" || lower == "whiteboard" || strings.Contains(lower, "board") {
			return "board"
		}
	}
	if isBoardPayload(item) || textAny(extra["m3u8_resource_id"]) != "" && strings.Contains(strings.ToLower(textAny(extra["m3u8_resource_id"])), "board") {
		return "board"
	}
	return "video"
}

func isBoardPayload(value any) bool {
	switch x := value.(type) {
	case map[string]any:
		for _, key := range []string{"board", "whiteboard", "boards", "boardInfo", "boardResources"} {
			if x[key] != nil {
				return true
			}
		}
		for _, key := range []string{"sourceType", "contentType", "type", "playback_type"} {
			lower := strings.ToLower(textValue(x, key))
			if lower == "board" || lower == "whiteboard" || strings.Contains(lower, "board") {
				return true
			}
		}
		for _, nested := range x {
			if isBoardPayload(nested) {
				return true
			}
		}
	case []any:
		for _, item := range x {
			if isBoardPayload(item) {
				return true
			}
		}
	}
	return false
}

func textAny(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "<nil>" {
			return ""
		}
		return s
	}
}
