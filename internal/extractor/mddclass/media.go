package mddclass

import (
	"encoding/json"
	"fmt"
	"net/url"
	stdpath "path"
	"strconv"
	"strings"
)

func mddclassPayloadSuccess(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	status := payload["status"]
	code := payload["code"]
	statusOK := status == nil || mddclassFirstText(status) == "0"
	codeText := mddclassFirstText(code)
	return statusOK && codeText != "-1"
}

func mddclassPayloadData(payload map[string]any) any {
	if payload == nil {
		return map[string]any{}
	}
	data, ok := payload["data"]
	if !ok || data == nil {
		if alt, exists := payload["data:"]; exists {
			data = alt
		}
	}
	switch data.(type) {
	case map[string]any, []any:
		return data
	default:
		return map[string]any{}
	}
}

func mddclassExtractList(data any) any {
	if data == nil {
		return []any{}
	}
	if list, ok := data.([]any); ok {
		return list
	}
	m := mddclassMap(data)
	if len(m) == 0 {
		return []any{}
	}
	for _, key := range []string{"items", "list", "records", "rows", "data", "result"} {
		value, exists := m[key]
		if !exists {
			continue
		}
		if _, ok := value.([]any); ok {
			return value
		}
		if nested := mddclassExtractList(value); len(mddclassRecords(nested)) > 0 {
			return nested
		}
	}
	return []any{}
}

func mddclassHasNextPage(data map[string]any, itemCount, start, limit int) bool {
	if itemCount == 0 {
		return false
	}
	if next, ok := data["nextPage"]; ok {
		if b, ok := next.(bool); ok {
			return b
		}
		return !mddclassStringIn(mddclassFirstText(next), "", "0", "-1", "false", "False")
	}
	if total, ok := mddclassInt(data["total"]); ok {
		return start+itemCount < total
	}
	if total, ok := mddclassInt(data["totalCount"]); ok {
		return start+itemCount < total
	}
	if total, ok := mddclassInt(data["count"]); ok {
		return start+itemCount < total
	}
	return itemCount >= limit
}

func mddclassExtractCoursewareInfo(detail any) map[string]any {
	out := map[string]any{}
	mddclassCollectCoursewareInfo(detail, out, 0)
	return out
}

func mddclassCollectCoursewareInfo(value any, out map[string]any, depth int) {
	if depth > 6 || value == nil {
		return
	}
	m, ok := value.(map[string]any)
	if ok {
		for _, key := range []string{"coursewareInfo", "courseWareInfo", "courseware_info", "ocsInfo", "videoInfo", "mediaInfo", "contentInfo", "resourceInfo", "playInfo", "activityInfo", "lessonInfo", "collectInfo", "detail", "raw"} {
			if nested, exists := m[key]; exists {
				mddclassCollectCoursewareInfo(nested, out, depth+1)
			}
		}
		for _, pair := range [][2]string{{"tenantId", "tenantId"}, {"tenant_id", "tenantId"}, {"userSign", "userSign"}, {"user_sign", "userSign"}, {"ocsId", "coursewareId"}, {"coursewareId", "coursewareId"}, {"ocs_id", "coursewareId"}, {"courseware_id", "coursewareId"}, {"courseWareId", "coursewareId"}, {"courseId", "courseId"}, {"course_id", "courseId"}, {"videoId", "videoId"}, {"video_id", "videoId"}, {"contentId", "videoId"}, {"content_id", "videoId"}, {"sourceType", "sourceType"}, {"source_type", "sourceType"}, {"contentType", "contentType"}, {"content_type", "contentType"}, {"companyId", "companyId"}, {"company_id", "companyId"}, {"sellerId", "companyId"}, {"seller_id", "companyId"}, {"userSignKey", "userSignKey"}, {"user_sign_key", "userSignKey"}, {"xUserSign", "userSign"}, {"signature", "userSign"}, {"sign", "userSign"}, {"videoUrl", "videoUrl"}, {"playUrl", "videoUrl"}, {"m3u8Url", "videoUrl"}, {"hlsUrl", "videoUrl"}, {"mediaUrl", "videoUrl"}, {"mediaURL", "videoUrl"}, {"mp4URL", "videoUrl"}, {"downloadUrl", "videoUrl"}, {"url", "videoUrl"}} {
			if current := mddclassFirstText(out[pair[1]]); current == "" {
				if value := mddclassFirstText(m[pair[0]]); value != "" {
					out[pair[1]] = value
				}
			}
		}
		for _, nested := range m {
			if _, ok := nested.(map[string]any); ok {
				mddclassCollectCoursewareInfo(nested, out, depth+1)
			}
			if _, ok := nested.([]any); ok {
				mddclassCollectCoursewareInfo(nested, out, depth+1)
			}
		}
		return
	}
	if list, ok := value.([]any); ok {
		for _, item := range list {
			mddclassCollectCoursewareInfo(item, out, depth+1)
		}
	}
}

func mddclassFindMediaURL(value any) string {
	return mddclassFindMediaURLDepth(value, 0)
}

func mddclassFindMediaURLDepth(value any, depth int) string {
	if depth > 8 || value == nil {
		return ""
	}
	if m, ok := value.(map[string]any); ok {
		for _, key := range []string{"videoUrl", "playUrl", "m3u8Url", "hlsUrl", "mediaUrl", "mediaURL", "mp4URL", "downloadUrl"} {
			if u := mddclassNormalizeMediaURL(mddclassFirstText(m[key])); u != "" && !mddclassIsPlaceholderURL(u) {
				return u
			}
		}
		if u := mddclassNormalizeMediaURL(mddclassFirstText(m["url"])); u != "" && !mddclassIsPlaceholderURL(u) && mddclassLooksLikeMediaURL(u) {
			return u
		}
		for _, key := range []string{"coursewareInfo", "courseWareInfo", "courseware_info", "ocsInfo", "videoInfo", "mediaInfo", "contentInfo", "resourceInfo", "playInfo", "detail", "raw"} {
			if u := mddclassFindMediaURLDepth(m[key], depth+1); u != "" {
				return u
			}
		}
		for _, nested := range m {
			if u := mddclassFindMediaURLDepth(nested, depth+1); u != "" {
				return u
			}
		}
	}
	if list, ok := value.([]any); ok {
		for _, item := range list {
			if u := mddclassFindMediaURLDepth(item, depth+1); u != "" {
				return u
			}
		}
	}
	return ""
}

func mddclassURLWithParams(raw string, params map[string]string) string {
	if len(params) == 0 {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func mddclassFormatVideoTitle(index int, title string) string {
	if title == "" {
		title = "未命名课时"
	}
	return mddclassCleanTitle(fmt.Sprintf("[%d]--%s", index, title))
}

func mddclassCleanTitle(title string) string {
	return strings.TrimSpace(mddclassWindowsBadTitleChar.ReplaceAllString(title, "_"))
}

func mddclassNormalizeMediaURL(mediaURL string) string {
	mediaURL = strings.TrimSpace(strings.ReplaceAll(mediaURL, `\u0026`, "&"))
	if strings.HasPrefix(mediaURL, "//") {
		mediaURL = "https:" + mediaURL
	}
	return mediaURL
}

func mddclassIsPlaceholderURL(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	if u == "" || u == "http://" || u == "https://" || u == "null" || u == "none" || u == "undefined" || strings.HasSuffix(u, "/404") || strings.Contains(u, "blank") {
		return true
	}
	return strings.Contains(u, mddclassPlaceholderMP4)
}

func mddclassLooksLikeMediaURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") || strings.Contains(lower, ".flv") || strings.Contains(lower, ".ts") || strings.Contains(lower, "/hls/") || strings.Contains(lower, "video")
}

func mddclassStreamFormat(raw string) string {
	lower := strings.ToLower(raw)
	if strings.Contains(lower, ".m3u8") || strings.Contains(lower, "m3u8") {
		return "m3u8"
	}
	u, err := url.Parse(raw)
	if err == nil {
		ext := strings.TrimPrefix(strings.ToLower(stdpath.Ext(u.Path)), ".")
		if ext != "" {
			return ext
		}
	}
	return "mp4"
}

func mddclassMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func mddclassRecords(value any) []map[string]any {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func mddclassMergeMaps(first, second map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range first {
		out[k] = v
	}
	for k, v := range second {
		if _, exists := out[k]; !exists || mddclassFirstText(out[k]) == "" {
			out[k] = v
		}
	}
	return out
}

func mddclassFirstText(values ...any) string {
	for _, value := range values {
		switch v := value.(type) {
		case nil:
			continue
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		case fmt.Stringer:
			if s := strings.TrimSpace(v.String()); s != "" {
				return s
			}
		case json.Number:
			if s := strings.TrimSpace(v.String()); s != "" {
				return s
			}
		case float64:
			if v != 0 {
				if v == float64(int64(v)) {
					return strconv.FormatInt(int64(v), 10)
				}
				return strconv.FormatFloat(v, 'f', -1, 64)
			}
		case float32:
			if v != 0 {
				return strconv.FormatFloat(float64(v), 'f', -1, 32)
			}
		case int, int8, int16, int32, int64:
			if s := fmt.Sprint(v); s != "0" {
				return s
			}
		case uint, uint8, uint16, uint32, uint64:
			if s := fmt.Sprint(v); s != "0" {
				return s
			}
		case bool:
			if v {
				return "true"
			}
		default:
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func mddclassInt(value any) (int, bool) {
	s := mddclassFirstText(value)
	if s == "" {
		return 0, false
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i, true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(f), true
	}
	return 0, false
}

func mddclassInt64(values ...any) int64 {
	for _, value := range values {
		s := mddclassFirstText(value)
		if s == "" {
			continue
		}
		if i, err := strconv.ParseInt(s, 10, 64); err == nil && i > 0 {
			return i
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
			return int64(f)
		}
	}
	return 0
}

func mddclassUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func mddclassStringIn(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
