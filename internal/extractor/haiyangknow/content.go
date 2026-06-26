package haiyangknow

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func (x *hyCtx) loadSources() ([]hySource, error) {
	groups := x.requestGroupList()
	var lessons []hyLesson
	if len(groups) == 0 {
		lessons = append(lessons, x.buildLessonList(x.requestChapterList(""), 1)...)
	} else {
		for gi, group := range groups {
			gid := firstString(group, "id", "groupId", "group_id")
			lessons = append(lessons, x.buildLessonList(x.requestChapterList(gid), gi+1)...)
		}
	}
	if len(lessons) == 0 {
		return nil, fmt.Errorf("haiyangknow: no lessons found")
	}
	var out []hySource
	for _, lesson := range lessons {
		play := x.requestLessonPlayInfo(lesson.ID)
		out = append(out, x.sourcesFromLesson(lesson, play)...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("haiyangknow: no playable lesson sources")
	}
	return out, nil
}

func (x *hyCtx) requestGroupList() []map[string]any {
	for _, id := range x.courseContentIDs() {
		for _, params := range x.courseParamCandidates(id, "") {
			data := x.requestAPIData(x.platformAPIPrefix()+"/findCourseGroupList", params, []any{})
			if rows := extractRecords(data); len(rows) > 0 {
				x.apiCourseID = id
				return rows
			}
		}
	}
	return nil
}

func (x *hyCtx) requestChapterList(groupID string) []map[string]any {
	for _, id := range x.courseContentIDs() {
		for _, params := range x.courseParamCandidates(id, groupID) {
			data := x.requestAPIData(x.platformAPIPrefix()+"/findCourseChapterList", params, []any{})
			if rows := extractRecords(data); len(rows) > 0 {
				x.apiCourseID = id
				return rows
			}
		}
	}
	return nil
}

func (x *hyCtx) courseContentIDs() []string {
	c := x.selected
	vals := []string{x.cid, x.apiCourseID, c.ID, c.DraftID}
	for _, key := range []string{"courseId", "id", "draftId", "draft_id", "curriculumId"} {
		vals = append(vals, firstString(c.Course, key))
	}
	return uniqueNonEmpty(vals...)
}

func (x *hyCtx) isWXPlatform() bool {
	return x.platformType == "3" || strings.EqualFold(x.selected.RawPlatformType, "3")
}

func (x *hyCtx) platformAPIPrefix() string {
	s := strings.ToLower(strings.TrimSpace(firstNonEmpty(x.platformType, x.selected.PlatformType, x.selected.RawPlatformType)))
	if s == "2" || s == "ks" || s == "kuaishou" {
		return "/curriculum/ksCourse/mini/applet/anon"
	}
	if s == "3" || s == "wx" || s == "wechat" {
		return "/curriculum/wxCourse/mini/applet/anon"
	}
	return "/curriculum/dyCourse/mini/applet/anon"
}

func (x *hyCtx) courseParamCandidates(id, groupID string) []map[string]string {
	first, second := "id", "courseId"
	if x.isWXPlatform() {
		first, second = "courseId", "id"
	}
	var out []map[string]string
	for _, key := range []string{first, second} {
		p := map[string]string{key: id}
		if groupID != "" {
			p["groupId"] = groupID
		}
		out = append(out, p)
	}
	return out
}

func (x *hyCtx) buildLessonList(rows []map[string]any, chapterIndex int) []hyLesson {
	var out []hyLesson
	for i, row := range rows {
		if lesson := x.buildLessonInfo(row, chapterIndex, i+1); lesson.ID != "" {
			out = append(out, lesson)
		}
	}
	return out
}

func (x *hyCtx) buildLessonInfo(row map[string]any, chapterIndex, lessonIndex int) hyLesson {
	id := firstString(row, "id", "chapterId", "chapter_id", "lesson_id")
	if id == "" {
		return hyLesson{}
	}
	x.lessonSerial++
	serial := x.lessonSerial
	title := firstNonEmpty(firstString(row, "title", "name", "chapterName"), id)
	stem := lessonStem(title, serial, "[", "]")
	mat := lessonStem(title, serial, "(", ")")
	return hyLesson{ID: id, Title: title, VideoName: stem, MaterialName: mat, MaterialPrefix: fmt.Sprintf("(%d)--", serial), DocumentURL: firstString(row, "documentUrl", "document_url", "fileUrl"), Content: firstString(row, "contents", "content", "htmlContent"), Raw: row, Duration: row["duration"], MediaType: row["mediaType"]}
}

func (x *hyCtx) requestLessonPlayInfo(lessonID string) map[string]any {
	lessonID = strings.TrimSpace(lessonID)
	if lessonID == "" {
		return map[string]any{}
	}
	if cached, ok := x.playCache[lessonID]; ok {
		return cached
	}
	data := x.requestAPIData("/curriculum/course/mini/applet/findChapterVideoId", map[string]string{"platform": x.platformType, "id": lessonID}, map[string]any{})
	m := asMap(data)
	x.playCache[lessonID] = m
	return m
}

func (x *hyCtx) sourcesFromLesson(lesson hyLesson, play map[string]any) []hySource {
	var out []hySource
	if src, ok := x.aliyunSource(play); ok {
		src.Name = lesson.VideoName
		src.Kind = "video"
		out = append(out, src)
	} else {
		for _, raw := range collectMediaCandidates(play) {
			fmtv := extFormat(raw)
			kind := "video"
			if isAudioExt(fmtv) {
				kind = "audio"
			}
			out = append(out, hySource{Name: lesson.VideoName, URL: raw, Format: firstNonEmpty(fmtv, "mp4"), Kind: kind, NeedMerge: fmtv == "m3u8"})
		}
	}
	out = append(out, materialSources(lesson, play)...)
	return out
}

func materialSources(lesson hyLesson, play map[string]any) []hySource {
	var out []hySource
	for _, item := range iterMaterialItems(lesson, play) {
		if item.URL == "" {
			continue
		}
		item.Name = firstNonEmpty(item.Name, lesson.MaterialName)
		item.Kind = "material"
		item.Format = firstNonEmpty(item.Format, extFormat(item.URL), "pdf")
		out = append(out, item)
	}
	return out
}

func collectMediaCandidates(value any) []string {
	seen := map[string]bool{}
	var out []string
	appendOne := func(s string) {
		u := normalizeMediaURL(s)
		if u == "" || seen[strings.ToLower(u)] {
			return
		}
		seen[strings.ToLower(u)] = true
		out = append(out, u)
	}
	if m, ok := value.(map[string]any); ok {
		for _, k := range []string{"playUrl", "playURL", "videoUrl", "videoURL", "url", "mediaUrl", "mediaURL", "mp4Url", "m3u8Url"} {
			appendOne(str(m[k]))
		}
	}
	for _, s := range walkStrings(value) {
		appendOne(s)
	}
	return out
}

func iterMaterialItems(lesson hyLesson, play map[string]any) []hySource {
	queue := []any{lesson.DocumentURL, lesson.Content, lesson.Raw["documentUrl"], lesson.Raw["contents"]}
	for _, k := range []string{"documentUrl", "document_url", "fileUrl", "contents", "content"} {
		queue = append(queue, play[k])
	}
	seen := map[string]bool{}
	var out []hySource
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		switch t := v.(type) {
		case string:
			s := strings.TrimSpace(t)
			if strings.HasPrefix(s, "//") {
				s = "https:" + s
			}
			if strings.HasPrefix(s, "http") && !seen[strings.ToLower(s)] {
				seen[strings.ToLower(s)] = true
				out = append(out, hySource{Name: lesson.MaterialName, URL: s, Format: extFormat(s), Kind: "material"})
			}
		case map[string]any:
			for _, k := range []string{"url", "file_url", "download_url", "material_url", "documentUrl", "oss_url"} {
				if str(t[k]) != "" {
					queue = append(queue, t[k])
				}
			}
			for _, vv := range t {
				if _, ok := vv.(map[string]any); ok {
					queue = append(queue, vv)
				}
				if _, ok := vv.([]any); ok {
					queue = append(queue, vv)
				}
			}
		case []any:
			queue = append(queue, t...)
		}
	}
	return out
}

func (x *hyCtx) mediaFromSources(sources []hySource) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, src := range sources {
		if src.URL == "" {
			continue
		}
		fmtv := firstNonEmpty(src.Format, extFormat(src.URL), "mp4")
		mi := &extractor.MediaInfo{Site: "haiyangknow", Title: firstNonEmpty(src.Name, path.Base(parsedPath(src.URL))), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{src.URL}, Format: fmtv, Size: src.Size, NeedMerge: src.NeedMerge, Headers: map[string]string{"Referer": referer, "Cookie": x.cookie}}}, Extra: map[string]any{"kind": src.Kind}}
		for k, v := range src.Extra {
			mi.Extra[k] = v
		}
		entries = append(entries, mi)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("haiyangknow: empty sources")
	}
	if len(entries) == 1 {
		entries[0].Extra["course_title"] = x.title
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "haiyangknow", Title: firstNonEmpty(x.title, x.cid, "haiyangknow"), Entries: entries, Extra: map[string]any{"course_id": x.cid, "platform": x.platformType}}, nil
}

func lessonStem(title string, serial int, left, right string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = fmt.Sprintf("lesson_%d", serial)
	}
	for _, ext := range []string{".mp4", ".m3u8", ".mp3", ".m4a", ".aac", ".wav", ".pdf"} {
		if strings.HasSuffix(strings.ToLower(title), ext) {
			title = strings.TrimSuffix(title, title[len(title)-len(ext):])
		}
	}
	return fmt.Sprintf("%s%d%s--%s", left, serial, right, title)
}

func normalizeMediaURL(s string) string {
	s = strings.Trim(strings.TrimSpace(s), "'\"")
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	}
	if !strings.HasPrefix(s, "http") {
		return ""
	}
	low := strings.ToLower(s)
	for _, ext := range []string{".m3u8", ".mp4", ".m4v", ".mov", ".flv", ".mp3", ".m4a", ".aac", ".wav"} {
		if strings.Contains(low, ext) {
			return s
		}
	}
	return ""
}

func extFormat(raw string) string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(parsedPath(raw))), ".")
}
func parsedPath(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Path != "" {
		return u.Path
	}
	return raw
}
func isAudioExt(ext string) bool { return ext == "mp3" || ext == "m4a" || ext == "aac" || ext == "wav" }
