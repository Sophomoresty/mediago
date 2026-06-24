// Package caixuetang helper functions for source-aligned JSON tree parsing.
package caixuetang

import (
	"encoding/json"
	"fmt"
	"strings"
)

func findChapterRoots(p map[string]any) []map[string]any {
	var out []map[string]any
	for _, k := range []string{"chapter_content", "coursechapteralive", "coursechapterbaijiayun", "chapter_list", "chapterList", "chapters", "catalog", "catalogs", "course_chapter", "courseChapter"} {
		out = append(out, extractItems(p[k])...)
	}
	return out
}
func iterChildren(n map[string]any) []map[string]any {
	var out []map[string]any
	for _, k := range []string{"children", "child", "chapter", "chapters", "chapter_list", "chapterList", "catalog", "catalogs", "catalog_list", "section", "sections", "lesson", "lessons", "list", "chapter_content"} {
		out = append(out, extractItems(n[k])...)
	}
	return out
}
func looksVideoNode(n map[string]any) bool {
	typ := strings.ToLower(firstString(n, "suffix", "resource_suffix", "resourceSuffix", "resource_type", "resourceType", "material_type", "materialType", "type", "ctype"))
	return containsAny(typ, "视频", "video", "vod", "live", "回放", "直播") || hasAny(n, "play_url", "playUrl", "video_url", "videoUrl", "item_id", "itemId", "video_id", "videoId", "videoid", "vod_id", "vodId", "vid") && !containsAny(typ, "file", "doc", "pdf", "ppt", "material", "datum")
}
func looksFileNode(n map[string]any) bool {
	if extractFileURL(n) != "" {
		return true
	}
	typ := strings.ToLower(firstString(n, "suffix", "resource_suffix", "resourceSuffix", "resource_type", "resourceType", "material_type", "materialType", "type", "ctype"))
	return containsAny(typ, "file", "doc", "pdf", "ppt", "material", "datum") && !containsAny(typ, "视频", "video", "vod", "live", "回放", "直播")
}
func parseVideoInfo(n map[string]any, idx []int, cid, ctype string) map[string]any {
	title := nodeTitle(n, fmt.Sprintf("%v", idx))
	return map[string]any{"type": "video", "video_id": nodeVideoID(n), "direct_url": firstString(n, "play_url", "playUrl", "video_url", "videoUrl", "url"), "video_name": fmt.Sprintf("[%s]--%s", joinIdx(idx), cleanTitle(title)), "course_id": firstNonEmpty(firstString(n, "course_id", "courseId", "courseid", "relation_course_id", "cid"), cid), "course_type": firstNonEmpty(firstString(n, "course_type", "courseType", "relation_course_type"), ctype), "node": n}
}
func parseFileInfo(n map[string]any, idx []int) map[string]any {
	u := extractFileURL(n)
	name := cleanTitle(firstNonEmpty(firstString(n, "file_name", "fileName", "name", "title"), nodeTitle(n, "file")))
	return map[string]any{"type": "file", "item_id": nodeVideoID(n), "file_url": u, "file_name": fmt.Sprintf("(%s)--%s", joinIdx(idx), name), "file_fmt": firstNonEmpty(firstString(n, "file_ext", "fileExt", "ext", "suffix", "file_type", "fileType", "format"), fileExt(u), "bin"), "node": n}
}
func nodeTitle(n map[string]any, def string) string {
	return firstNonEmpty(firstString(n, "title", "name", "item_name", "itemName", "chapter_name", "chapterName", "course_name", "courseName", "video_name", "videoName", "material_name", "file_name", "fileName", "live_name", "dir_name"), def)
}
func nodeVideoID(n map[string]any) string {
	return firstString(n, "item_id", "itemId", "relation_video_id", "relationVideoId", "video_id", "videoId", "videoid", "material_id", "materialId", "baijiayun_id", "bjy_video_id", "vod_id", "vodId", "vid", "id")
}
func extractFileURL(n map[string]any) string {
	return firstString(n, "file_url", "fileUrl", "download_url", "downloadUrl", "material_url", "materialUrl", "courseware_url", "coursewareUrl", "url", "path")
}

func extractPlayURL(v any) string {
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		if strings.HasPrefix(s, "http") {
			return s
		}
		if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			var y any
			if json.Unmarshal([]byte(s), &y) == nil {
				return extractPlayURL(y)
			}
		}
	case []any:
		best := pickByDefinition(x)
		if best != "" {
			return best
		}
		for _, it := range x {
			if u := extractPlayURL(it); u != "" {
				return u
			}
		}
	case map[string]any:
		for _, k := range []string{"url", "Url", "URL", "play_url", "playUrl", "PlayURL", "videoUrl", "source", "src", "path", "m3u8"} {
			if u := extractPlayURL(x[k]); u != "" {
				return u
			}
		}
		for _, k := range []string{"list", "playList", "sources", "source", "data"} {
			if u := extractPlayURL(x[k]); u != "" {
				return u
			}
		}
	}
	return ""
}
func pickByDefinition(items []any) string {
	order := map[string]int{"HD": 6, "FHD": 5, "UD": 4, "SD": 3, "LD": 2, "FD": 1}
	bestU := ""
	best := 0
	for _, it := range items {
		m := asMap(it)
		u := candidateURL(m)
		if u == "" {
			continue
		}
		d := strings.ToUpper(firstString(m, "definition", "Definition", "quality", "qualityCode", "defaultDefinition", "label", "text", "name"))
		if strings.Contains(d, "高清") {
			d = "HD"
		} else if strings.Contains(d, "超清") {
			d = "FHD"
		} else if strings.Contains(d, "标清") {
			d = "SD"
		} else if strings.Contains(d, "流畅") {
			d = "FD"
		}
		if score := order[d]; score >= best {
			best, bestU = score, u
		}
	}
	return bestU
}
func candidateURL(m map[string]any) string {
	for _, k := range []string{"url", "Url", "URL", "play_url", "playUrl", "PlayURL", "videoUrl", "source", "src", "path", "m3u8"} {
		if u := firstString(m, k); strings.HasPrefix(u, "http") {
			return u
		}
	}
	return ""
}
func extractItems(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m := asMap(it); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	}
	m := asMap(v)
	for _, k := range []string{"list", "course_list", "courseList", "dataList", "rows", "items", "records", "mycourse", "myCourse"} {
		if out := extractItems(m[k]); len(out) > 0 {
			return out
		}
	}
	return nil
}

func isSuccess(m map[string]any) bool {
	for _, k := range []string{"code", "errcode", "status"} {
		s := firstString(m, k)
		if s == "1" || s == "0" || s == "true" {
			return true
		}
	}
	return len(asMap(m["data"])) > 0
}
func hasAny(m map[string]any, keys ...string) bool { return firstString(m, keys...) != "" }
func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(fmt.Sprint(m[k])); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func containsAny(s string, vals ...string) bool {
	for _, v := range vals {
		if strings.Contains(s, strings.ToLower(v)) || strings.Contains(s, v) {
			return true
		}
	}
	return false
}
func joinIdx(idx []int) string {
	parts := make([]string, len(idx))
	for i, v := range idx {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ".")
}
func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }
func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
func fileExt(u string) string {
	path := strings.SplitN(u, "?", 2)[0]
	parts := strings.Split(path, ".")
	if len(parts) > 1 {
		return strings.ToLower(parts[len(parts)-1])
	}
	return ""
}
