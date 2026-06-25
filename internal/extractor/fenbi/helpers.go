package fenbi

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

func collectEpisodes(v any) []episodeNode {
	seen := map[string]bool{}
	var out []episodeNode
	var walk func(any, string)
	walk = func(x any, title string) {
		x = unwrapData(x)
		switch vv := x.(type) {
		case map[string]any:
			nextTitle := firstNonEmpty(valueString(vv, "title", "name", "episodeTitle", "lessonTitle", "episodeName", "videoName", "video_name", "coursewareName"), title)
			id := valueString(vv, "id", "episodeId", "episode_id", "episode_id_str", "videoId", "video_id", "contentId")
			if id != "" && !seen[id] && (hasAny(vv, "episodeId", "episode_id", "episode_id_str", "videoId", "video_id") || hasAny(vv, "mediafile", "mediaFile", "duration", "mediaDuration", "bizType", "biz_type")) {
				seen[id] = true
				out = append(out, episodeNode{ID: id, Title: nextTitle, Raw: vv})
			}
			for _, k := range []string{"episodes", "episodeList", "episodeNodes", "nodes", "lessons", "lessonList", "tasks", "taskList", "items", "list", "children", "syllabus", "contents", "chapters", "chapterList", "units", "unitList", "data"} {
				if child, ok := vv[k]; ok {
					walk(child, nextTitle)
				}
			}
		case []any:
			for _, child := range vv {
				walk(child, title)
			}
		}
	}
	walk(v, "")
	return out
}

func findMediaURL(v any) string {
	v = unwrapData(v)
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"url", "mediaUrl", "media_url", "path", "downloadUrl", "download_url", "fileUrl", "file_url", "playUrl", "m3u8"} {
			if s := normalizeURL(valueString(x, k)); isMediaURL(s) {
				return s
			}
		}
		for _, k := range []string{"mediaFiles", "qualities", "mediaList", "mediaSizes", "streamList", "videoList", "definitions", "urls", "files", "list", "streams", "data"} {
			if child, ok := x[k]; ok {
				if s := findMediaURL(child); s != "" {
					return s
				}
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

func pickTitle(v any) string {
	v = unwrapData(v)
	switch x := v.(type) {
	case map[string]any:
		if s := valueString(x, "courseTitle", "lectureTitle", "lectureSetTitle", "title", "name", "episodeTitle", "episodeName", "videoName"); s != "" {
			return s
		}
		for _, child := range x {
			if s := pickTitle(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := pickTitle(child); s != "" {
				return s
			}
		}
	}
	return ""
}

func mediaInfo(title, mediaURL string, headers map[string]string) *extractor.MediaInfo {
	format := "mp4"
	if strings.Contains(strings.ToLower(mediaURL), ".m3u8") {
		format = "m3u8"
	}
	return &extractor.MediaInfo{Site: "fenbi", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{mediaURL}, Format: format, Headers: headers}}}
}

func valueString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := anyString(v)
			if s != "" && s != "0" {
				return s
			}
		}
	}
	return ""
}

func hasAny(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func normalizeURL(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	s = strings.ReplaceAll(s, `\/`, `/`)
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	return s
}

func unwrapData(v any) any {
	for {
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		code := anyString(m["code"])
		if code != "" && code != "0" && code != "1" && !strings.EqualFold(code, "true") {
			return map[string]any{}
		}
		child, ok := m["data"]
		if !ok {
			return v
		}
		switch child.(type) {
		case map[string]any, []any, string:
			v = child
		default:
			return v
		}
	}
}

func listMaps(v any, keys ...string) []map[string]any {
	v = unwrapData(v)
	switch x := v.(type) {
	case []any:
		return mapsFromList(x)
	case map[string]any:
		for _, key := range keys {
			if rows, ok := x[key]; ok {
				if out := listMaps(rows); len(out) > 0 {
					return out
				}
			}
		}
		for _, key := range []string{"list", "items", "data", "datas", "lectures", "lectureList", "materials", "materialList"} {
			if rows, ok := x[key]; ok {
				if out := listMaps(rows); len(out) > 0 {
					return out
				}
			}
		}
	}
	return nil
}

func mapsFromList(rows []any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if m, ok := unwrapData(row).(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func firstAny(v any, keys ...string) any {
	if m, ok := unwrapData(v).(map[string]any); ok {
		for _, key := range keys {
			if value, ok := m[key]; ok && anyString(value) != "" {
				return value
			}
		}
	}
	return nil
}

func toInt(v any, fallback int) int {
	switch x := v.(type) {
	case nil:
		return fallback
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case jsonNumber:
		i, err := strconv.Atoi(x.String())
		if err == nil {
			return i
		}
	}
	s := anyString(v)
	if s == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fallback
	}
	return int(f)
}

type jsonNumber interface{ String() string }

func mergeVideoInfo(dst map[string]any, src any) {
	switch x := unwrapData(src).(type) {
	case map[string]any:
		for _, key := range []string{"prefix", "episode_id", "episodeId", "lecture_id", "lectureId", "content_id", "contentId", "biz_type", "bizType", "biz_id", "bizId", "material_id", "materialId", "note_material_id", "noteMaterialId"} {
			if _, exists := dst[key]; exists {
				continue
			}
			if value, ok := x[key]; ok && anyString(value) != "" {
				dst[key] = value
			}
		}
		for _, key := range []string{"episode", "episodeInfo", "mediafile", "mediaFile", "video", "data", "detail"} {
			if child, ok := x[key]; ok {
				mergeVideoInfo(dst, child)
			}
		}
	case []any:
		for _, child := range x {
			mergeVideoInfo(dst, child)
		}
	}
}

func infoString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if s := anyString(value); s != "" {
				return s
			}
		}
	}
	return ""
}

func collectMaterialCandidates(values ...any) []map[string]any {
	var out []map[string]any
	seen := map[string]bool{}
	var walk func(any, bool)
	walk = func(v any, inMaterialList bool) {
		v = unwrapData(v)
		switch x := v.(type) {
		case map[string]any:
			if isMaterialMap(x, inMaterialList) {
				addMaterialCandidate(&out, seen, x)
			}
			for _, key := range []string{"materials", "materialList", "material_list", "courseMaterials", "courseMaterialList", "handouts", "handoutList", "attachments", "attachmentList", "coursewareList", "datas", "data", "list", "items"} {
				if child, ok := x[key]; ok {
					lowKey := strings.ToLower(key)
					walk(child, strings.Contains(lowKey, "material") || strings.Contains(lowKey, "handout") || strings.Contains(lowKey, "attachment") || strings.Contains(lowKey, "courseware"))
				}
			}
			for _, key := range []string{"material_id", "materialId", "note_material_id", "noteMaterialId"} {
				if valueString(x, key) != "" {
					addMaterialCandidate(&out, seen, map[string]any{key: valueString(x, key), "name": materialDefaultName(key)})
				}
			}
		case []any:
			for _, child := range x {
				walk(child, inMaterialList)
			}
		}
	}
	for _, value := range values {
		walk(value, false)
	}
	return out
}

func isMaterialMap(m map[string]any, inMaterialList bool) bool {
	if hasAny(m, "materialId", "material_id", "noteMaterialId", "note_material_id", "fileId", "file_id") {
		return true
	}
	if pickURLFromResponse(m) != "" && (inMaterialList || hasAny(m, "fileName", "filename", "materialName", "coursewareName", "typeName", "fileType", "ext")) {
		return true
	}
	return false
}

func addMaterialCandidate(out *[]map[string]any, seen map[string]bool, m map[string]any) {
	key := firstNonEmpty(valueString(m, "materialId", "id", "material_id", "fileId", "file_id", "noteMaterialId", "note_material_id"), pickURLFromResponse(m))
	if key == "" {
		key = fmt.Sprintf("%p", m)
	}
	if seen[key] {
		return
	}
	seen[key] = true
	copyMap := map[string]any{}
	for k, v := range m {
		copyMap[k] = v
	}
	*out = append(*out, copyMap)
}

func materialDefaultName(key string) string {
	if strings.Contains(strings.ToLower(key), "note") {
		return "笔记解析"
	}
	return "讲义"
}

func pickURLFromResponse(v any) string {
	v = unwrapData(v)
	switch x := v.(type) {
	case string:
		return normalizeURL(x)
	case []any:
		candidates := make([]string, 0, len(x))
		for _, child := range x {
			if u := pickURLFromResponse(child); u != "" {
				candidates = append(candidates, u)
			}
		}
		return bestMaterialURL(candidates)
	case map[string]any:
		var candidates []string
		for _, key := range []string{"url", "path", "downloadUrl", "download_url", "fileUrl", "file_url", "sourceUrl", "source_url"} {
			if s := normalizeURL(anyString(x[key])); s != "" {
				candidates = append(candidates, s)
			}
		}
		for _, key := range []string{"urls", "urlList", "paths", "files", "list", "data"} {
			if child, ok := x[key]; ok {
				if u := pickURLFromResponse(child); u != "" {
					candidates = append(candidates, u)
				}
			}
		}
		return bestMaterialURL(candidates)
	default:
		return ""
	}
}

func bestMaterialURL(candidates []string) string {
	var first, firstNonImage string
	for _, candidate := range candidates {
		candidate = normalizeURL(candidate)
		if candidate == "" {
			continue
		}
		if first == "" {
			first = candidate
		}
		lower := strings.ToLower(candidate)
		if strings.Contains(lower, ".pdf") {
			return candidate
		}
		if firstNonImage == "" && !isImageExt(fileExt(candidate)) {
			firstNonImage = candidate
		}
	}
	return firstNonEmpty(firstNonImage, first)
}

func materialName(m map[string]any) string {
	return firstNonEmpty(valueString(m, "name", "title", "materialName", "material_name", "fileName", "file_name", "coursewareName", "typeName"), "课件")
}

func fileExt(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "bin"
	}
	if strings.Contains(raw, "/") || strings.Contains(raw, ".") {
		if u, err := url.Parse(raw); err == nil {
			if ext := strings.TrimPrefix(strings.ToLower(path.Ext(u.Path)), "."); ext != "" {
				return ext
			}
		}
		if ext := strings.TrimPrefix(strings.ToLower(path.Ext(raw)), "."); ext != "" {
			return ext
		}
	}
	raw = strings.TrimPrefix(strings.ToLower(raw), ".")
	if raw == "" {
		return "bin"
	}
	return raw
}

func isImageExt(ext string) bool {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "ico", "heic", "heif":
		return true
	default:
		return false
	}
}

func isMediaURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "http") && (strings.Contains(low, ".m3u8") || strings.Contains(low, ".mp4") || strings.Contains(low, ".flv") || strings.Contains(low, ".mov") || strings.Contains(low, ".m4v") || strings.Contains(low, ".mp3") || strings.Contains(low, ".m4a") || strings.Contains(low, ".aac") || strings.Contains(low, ".wav"))
}

func anyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		s := strings.TrimSpace(fmt.Sprint(x))
		if s == "<nil>" {
			return ""
		}
		return s
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// collectEpisodeSetIDs walks a payload tree and collects all episode set IDs
// into the seen map, so we can avoid re-fetching sets we already have.
func collectEpisodeSetIDs(v any, seen map[string]bool) {
	v = unwrapData(v)
	switch x := v.(type) {
	case map[string]any:
		if id := valueString(x, "episodeSetId", "episode_set_id"); id != "" {
			seen[id] = true
		}
		for _, child := range x {
			collectEpisodeSetIDs(child, seen)
		}
	case []any:
		for _, child := range x {
			collectEpisodeSetIDs(child, seen)
		}
	}
}

// extractSummaryEpisodeSetIDs extracts root-level episode set IDs from
// the lecture summary's "episodeSets" array. Only returns sets that are
// root sets (not child sets). Mirrors source _summary_episode_set_entries
// (Fenbi_Course line 1135).
func extractSummaryEpisodeSetIDs(v any) []string {
	v = unwrapData(v)
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	// Look in the summary payload for episodeSets.
	setsRaw, ok := m["episodeSets"]
	if !ok {
		return nil
	}
	setsList, ok := setsRaw.([]any)
	if !ok {
		return nil
	}
	var ids []string
	for _, item := range setsList {
		setMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		// Skip non-root sets: source checks for "root" key or parentEpisodeSetId.
		if rootVal, hasRoot := setMap["root"]; hasRoot {
			if rootVal == false || anyString(rootVal) == "false" || anyString(rootVal) == "0" {
				continue
			}
		}
		parentID := firstNonEmpty(anyString(setMap["parentEpisodeSetId"]), anyString(setMap["parent_episode_set_id"]))
		if parentID != "" && parentID != "0" {
			continue
		}
		setID := firstNonEmpty(
			valueString(setMap, "episodeSetId", "episode_set_id", "setId", "set_id", "id"),
		)
		if setID != "" {
			ids = append(ids, setID)
		}
	}
	return ids
}
