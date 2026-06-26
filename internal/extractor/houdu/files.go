package houdu

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func (x *hdCtx) extractSourceInfo(detail map[string]any) []hdSource {
	seen := map[string]bool{}
	var out []hdSource
	var walk func(any, []int)
	walk = func(v any, prefix []int) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		for _, key := range FILE_LIST_KEYS {
			rows := listMaps(m[key])
			for i, row := range rows {
				if src := makeFileSource(row, append(prefix, i+1)); src.URL != "" && !seen[strings.ToLower(src.URL)] {
					seen[strings.ToLower(src.URL)] = true
					out = append(out, src)
				}
			}
		}
		for _, key := range CHILD_LIST_KEYS {
			for i, child := range listMaps(m[key]) {
				walk(child, append(prefix, i+1))
			}
		}
	}
	walk(detail, nil)
	return out
}

func makeFileSource(info map[string]any, prefix []int) hdSource {
	rawURL := firstString(info, "url", "download_url", "file_url", "path")
	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	}
	if !strings.HasPrefix(rawURL, "http") {
		return hdSource{}
	}
	name := cleanName(firstNonEmpty(firstString(info, "file_name", "filename", "name", "title"), rawURL[strings.LastIndex(rawURL, "/")+1:]))
	fmtv := firstNonEmpty(strings.TrimPrefix(strings.ToLower(firstString(info, "suffix", "ext", "extension", "format", "file_format")), "."), extFormat(rawURL))
	if len(prefix) > 0 {
		name = fmt.Sprintf("(%s)--%s", joinInts(prefix, "."), trimFileSuffix(name, fmtv))
	}
	return hdSource{Name: name, URL: rawURL, Kind: "file", Format: fmtv, NeedMerge: fmtv == "m3u8"}
}

func (x *hdCtx) mediaFromSources(sources []hdSource) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, src := range sources {
		if src.URL == "" {
			continue
		}
		fmtv := firstNonEmpty(src.Format, extFormat(src.URL))
		extra := ensureExtra(src.Extra)
		extra["kind"] = firstNonEmpty(src.Kind, "video")
		entries = append(entries, &extractor.MediaInfo{Site: "houdu", Title: cleanName(firstNonEmpty(src.Name, src.URL)), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{src.URL}, Format: fmtv, NeedMerge: src.NeedMerge || fmtv == "m3u8", Headers: map[string]string{"Referer": referer, "Cookie": x.cookie, "Authorization": x.token, "User-Agent": USER_AGENT}}}, Extra: extra})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("houdu: empty media entries")
	}
	return &extractor.MediaInfo{Site: "houdu", Title: firstNonEmpty(x.title, x.cid, "houdu"), Entries: entries, Extra: map[string]any{"course_id": x.cid, "course_type": x.courseType, "price": x.price}}, nil
}

func ensureExtra(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func isFlatLessonList(items []map[string]any) bool {
	for _, item := range items {
		if firstString(item, "lesson_id", "id") != "" && (firstString(item, "lesson_name", "title", "name") != "" || item["lesson_type"] != nil) {
			return true
		}
	}
	return false
}

func sortLessonList(rows []map[string]any) []map[string]any {
	out := append([]map[string]any{}, rows...)
	sort.SliceStable(out, func(i, j int) bool { return lessonSortValue(out[i], i) < lessonSortValue(out[j], j) })
	return out
}

func sortGroupList(rows []map[string]any) []map[string]any {
	out := append([]map[string]any{}, rows...)
	sort.SliceStable(out, func(i, j int) bool { return groupSortValue(out[i], i) < groupSortValue(out[j], j) })
	return out
}

func lessonSortValue(m map[string]any, fallback int) int {
	for _, key := range []string{"sort", "order", "lesson_index", "index", "seq", "sequence", "id", "lesson_id"} {
		if n := intVal(m[key]); n > 0 {
			return n
		}
	}
	return fallback + 1
}

func groupSortValue(m map[string]any, fallback int) int {
	for _, key := range []string{"sort", "order", "group_index", "index", "seq", "sequence", "id"} {
		if n := intVal(m[key]); n > 0 {
			return n
		}
	}
	return fallback + 1
}

func joinInts(vals []int, sep string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, sep)
}

func trimFileSuffix(name, fmtv string) string {
	fmtv = strings.Trim(strings.ToLower(fmtv), ".")
	if fmtv == "" {
		return name
	}
	suffix := "." + fmtv
	if strings.HasSuffix(strings.ToLower(name), suffix) {
		return name[:len(name)-len(suffix)]
	}
	return name
}
