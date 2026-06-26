package chaoxing

import (
	"fmt"
	htmlpkg "html"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type chaoxingChapter struct {
	ID    string
	Title string
	Index int
}

func (x *chaoxingContext) resolveCourse(rawURL string) (*extractor.MediaInfo, string, error) {
	page := ""
	pageObjectID := ""
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		if body, err := x.getString(rawURL); err == nil {
			page = body
			x.extractAccessFromText(body)
			pageObjectID = extractObjectIDFromPage(body)
			if x.title == "" {
				x.title = parseChaoxingTitle(body)
			}
		}
	}

	coursePage := page
	if u := x.buildCoursePageURL(); u != "" {
		if body, err := x.getString(u); err == nil && strings.TrimSpace(body) != "" {
			coursePage = body
			x.extractAccessFromText(body)
			if x.title == "" {
				x.title = parseChaoxingTitle(body)
			}
		}
	}
	if strings.TrimSpace(coursePage) == "" {
		return nil, pageObjectID, fmt.Errorf("chaoxing: cannot fetch course page")
	}

	chapters := collectChaoxingChapters(coursePage)
	if pid := firstNonEmpty(queryValue(rawURL, "chapterId"), queryValue(rawURL, "chapterid")); pid != "" && !chapterSeen(chapters, pid) {
		chapters = append(chapters, chaoxingChapter{ID: pid, Title: "chapter_" + pid, Index: len(chapters) + 1})
	}
	if len(chapters) == 0 {
		return nil, pageObjectID, fmt.Errorf("chaoxing: no chapter/knowledge ids found")
	}

	seen := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(chapters))
	for _, ch := range chapters {
		for _, entry := range x.resolveChapter(ch) {
			if entry == nil || len(entry.Streams) == 0 {
				continue
			}
			key := entry.Title
			for _, st := range entry.Streams {
				if len(st.URLs) > 0 {
					key += "|" + st.URLs[0]
					break
				}
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, entry)
		}
	}
	// Course-data attachment/material branch (Chaoxing_Course._get_file_list).
	// Only runs when an openc is available; the source probes for it but the
	// probe relies on transfer-redirect parsing, so when openc is absent we
	// fail closed for this branch rather than guess a value.
	if x.openc != "" {
		for _, entry := range x.resolveFileEntries(x.openc) {
			if entry == nil || len(entry.Streams) == 0 {
				continue
			}
			key := entry.Title
			for _, st := range entry.Streams {
				if len(st.URLs) > 0 {
					key += "|" + st.URLs[0]
					break
				}
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, entry)
		}
	}

	if len(entries) == 0 {
		return nil, pageObjectID, fmt.Errorf("chaoxing: no resources resolved from course cards")
	}
	return &extractor.MediaInfo{
		Site:    "chaoxing",
		Title:   util.SanitizeFilename(firstNonEmpty(x.title, "chaoxing_"+firstNonEmpty(x.courseID, x.clazzID, "course"))),
		Entries: entries,
		Extra: compactExtra(map[string]any{
			"course_id": x.courseID,
			"clazz_id":  x.clazzID,
			"enc":       x.enc,
			"cpi":       x.cpi,
		}),
	}, pageObjectID, nil
}

func (x *chaoxingContext) resolveChapter(ch chaoxingChapter) []*extractor.MediaInfo {
	cpi := x.ensureCPI(ch.ID)
	ajax, err := x.c.PostForm(x.abs("/mycourse/studentstudyAjax"), map[string]string{
		"verificationcode": "",
		"cpi":              cpi,
		"chapterId":        ch.ID,
		"clazzid":          x.clazzID,
		"courseId":         x.courseID,
	}, x.headers)
	if err != nil {
		return nil
	}
	nums, kid := parseCardCountAndKnowledgeID(ajax, ch.ID)
	cardTexts := []string{ajax}
	if nums > 0 && kid != "" {
		cardTexts = cardTexts[:0]
		if nums > 100 {
			nums = 100
		}
		for i := 0; i < nums; i++ {
			values := url.Values{}
			values.Set("clazzid", x.clazzID)
			values.Set("courseid", x.courseID)
			values.Set("knowledgeid", kid)
			values.Set("num", fmt.Sprint(i))
			values.Set("cpi", cpi)
			body, err := x.getString(x.abs("/knowledge/cards") + "?" + values.Encode())
			if err == nil {
				cardTexts = append(cardTexts, body)
			}
		}
	}

	resources := collectChaoxingResources(cardTexts, ch.Title)
	entries := make([]*extractor.MediaInfo, 0, len(resources))
	for _, res := range resources {
		if res.Title == "" {
			res.Title = ch.Title
		}
		if entry := x.resolveResource(res); entry != nil {
			entry.Title = util.SanitizeFilename(prefixChapterTitle(ch, entry.Title))
			entries = append(entries, entry)
		}
	}
	return entries
}

func (x *chaoxingContext) ensureCPI(pid string) string {
	if x.cpi != "" || pid == "" || x.courseID == "" || x.clazzID == "" {
		return x.cpi
	}
	values := url.Values{}
	values.Set("chapterId", pid)
	values.Set("courseId", x.courseID)
	values.Set("clazzid", x.clazzID)
	if x.enc != "" {
		values.Set("enc", x.enc)
	}
	body, err := x.getString(x.abs("/mycourse/studentstudy") + "?" + values.Encode())
	if err != nil {
		return x.cpi
	}
	x.extractAccessFromText(body)
	if x.cpi == "" {
		if m := regexp.MustCompile(`getTeacherAjax\('\d+','\d+','\d+','(\d+)'.*?\);`).FindStringSubmatch(body); len(m) > 1 {
			x.cpi = m[1]
		}
	}
	return x.cpi
}

func (x *chaoxingContext) buildCoursePageURL() string {
	if x.courseID == "" || x.clazzID == "" || x.enc == "" {
		return ""
	}
	values := url.Values{}
	values.Set("courseId", x.courseID)
	values.Set("clazzid", x.clazzID)
	if x.cpi != "" {
		values.Set("cpi", x.cpi)
	}
	values.Set("enc", x.enc)
	return x.abs("/mycourse/studentcourse") + "?" + values.Encode()
}

func (x *chaoxingContext) extractAccessFromURL(raw string) {
	if raw == "" {
		return
	}
	raw = htmlpkg.UnescapeString(raw)
	if u, err := url.Parse(raw); err == nil {
		for key, vals := range u.Query() {
			if len(vals) == 0 {
				continue
			}
			switch strings.ToLower(key) {
			case "courseid", "course_id", "moocid":
				x.courseID = firstNonEmpty(x.courseID, vals[0])
			case "clazzid", "classid":
				x.clazzID = firstNonEmpty(x.clazzID, vals[0])
			case "enc":
				x.enc = firstNonEmpty(x.enc, vals[0])
			case "cpi":
				x.cpi = firstNonEmpty(x.cpi, vals[0])
			case "openc":
				x.openc = firstNonEmpty(x.openc, vals[0])
			}
		}
	}
	x.courseID = firstNonEmpty(x.courseID, regexpFirst(raw, `(?i)(?:courseId|courseid|course_id|moocId)=([0-9]+)`), regexpFirst(raw, `(?i)["']courseId["']\s*[:=]\s*["']?([0-9]+)`))
	x.clazzID = firstNonEmpty(x.clazzID, regexpFirst(raw, `(?i)(?:clazzId|clazzid|classId)=([0-9]+)`), regexpFirst(raw, `(?i)["']clazzId["']\s*[:=]\s*["']?([0-9]+)`))
	x.enc = firstNonEmpty(x.enc, regexpFirst(raw, `(?i)(?:\?|&|&amp;)enc=([a-z0-9]+)`), regexpFirst(raw, `(?i)["']enc["']\s*[:=]\s*["']([a-z0-9]+)`))
	x.cpi = firstNonEmpty(x.cpi, regexpFirst(raw, `(?i)(?:\?|&|&amp;)cpi=([0-9]+)`), regexpFirst(raw, `(?i)["']cpi["']\s*[:=]\s*["']?([0-9]+)`))
	x.openc = firstNonEmpty(x.openc, regexpFirst(raw, `(?i)(?:\?|&|&amp;)openc=([\w\d]+)`), regexpFirst(raw, `(?i)["']openc["']\s*[:=]\s*["']([\w\d]+)`))
}

func (x *chaoxingContext) extractAccessFromText(text string) {
	if text == "" {
		return
	}
	x.extractAccessFromURL(text)
	if val := hiddenValue(text, "cpi"); val != "" {
		x.cpi = val
	}
	if val := hiddenValue(text, "openc"); val != "" {
		x.openc = val
	}
	if val := hiddenValue(text, "oldenc"); val != "" {
		x.oldEnc = val
	}
	if val := hiddenValue(text, "enc"); val != "" {
		x.enc = firstNonEmpty(x.enc, val)
	}
	if val := hiddenValue(text, "downpath"); val != "" {
		x.downpath = strings.TrimRight(val, "/")
	}
	if val := hiddenValue(text, "courseId"); val != "" {
		x.courseID = firstNonEmpty(x.courseID, val)
	}
	if val := hiddenValue(text, "clazzid"); val != "" {
		x.clazzID = firstNonEmpty(x.clazzID, val)
	}
	if val := hiddenValue(text, "clazzId"); val != "" {
		x.clazzID = firstNonEmpty(x.clazzID, val)
	}
	if m := regexp.MustCompile(`var\s+enc\s*=\s*"([a-z0-9]+)";\s*var\s+cpi\s*=\s*(\d+)`).FindStringSubmatch(text); len(m) > 2 {
		x.enc = firstNonEmpty(x.enc, m[1])
		x.cpi = firstNonEmpty(x.cpi, m[2])
	}
}

func collectChaoxingChapters(text string) []chaoxingChapter {
	seen := map[string]bool{}
	var out []chaoxingChapter
	add := func(id, title string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, chaoxingChapter{ID: id, Title: firstNonEmpty(cleanText(title), "chapter_"+id), Index: len(out) + 1})
	}
	for _, m := range regexp.MustCompile(`(?is)toOld\('\d+',\s*'(\d+)',\s*'\d+'`).FindAllStringSubmatchIndex(text, -1) {
		id := text[m[2]:m[3]]
		start := lastIndexFloor(text, "<li", m[0], 900)
		end := nextIndexCeil(text, "</li>", m[1], 2000)
		chunk := text[start:end]
		add(id, titleFromChunk(chunk))
	}
	for _, m := range regexp.MustCompile(`(?is)<a[^>]+href=["'][^"']*chapterId=(\d+)[^"']*["'][^>]*>([\s\S]*?)</a>`).FindAllStringSubmatch(text, -1) {
		add(m[1], stripTags(m[2]))
	}
	return out
}

func parseCardCountAndKnowledgeID(text, fallbackKid string) (int, string) {
	if m := regexp.MustCompile(`\('(\d+)','(\d+)','\d+','\d+','\d*'\)`).FindStringSubmatch(text); len(m) > 2 {
		return atoi(m[1]), m[2]
	}
	count := 0
	kid := ""
	if m := regexp.MustCompile(`id=["']cardcount["']\s+type=["']hidden["']\s+value=["'](\d+)["']`).FindStringSubmatch(text); len(m) > 1 {
		count = atoi(m[1])
	}
	if m := regexp.MustCompile(`knowledge/cards\?[^"']*knowledgeid=(\d+)[^"']*num=(\d+)`).FindStringSubmatch(text); len(m) > 1 {
		kid = m[1]
	}
	if count > 0 && kid == "" {
		kid = fallbackKid
	}
	return count, kid
}

func parseChaoxingTitle(text string) string {
	if t := cleanText(regexpFirst(text, `(?is)<title>([\s\S]*?)</title>`)); t != "" {
		for _, bad := range []string{"学习进度页面", "温馨提示", "用户登录", "403", "404"} {
			if strings.Contains(t, bad) {
				t = ""
				break
			}
		}
		if t != "" {
			return t
		}
	}
	return cleanText(firstNonEmpty(regexpFirst(text, `(?is)<h2[^>]*class=["'][^"']*xs_head_name[^"']*["'][^>]*>([\s\S]*?)</h2>`), regexpFirst(text, `(?is)<span\s+title=["'](.*?)["']\s*>`)))
}

func prefixChapterTitle(ch chaoxingChapter, title string) string {
	base := cleanText(title)
	if strings.HasPrefix(base, "[") || strings.HasPrefix(base, "(") || strings.HasPrefix(base, "#") {
		return base
	}
	if ch.Index > 0 {
		return fmt.Sprintf("[%d]--%s", ch.Index, base)
	}
	return base
}

func chapterSeen(chapters []chaoxingChapter, id string) bool {
	for _, ch := range chapters {
		if ch.ID == id {
			return true
		}
	}
	return false
}
