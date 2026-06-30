package chaoxing

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	publicChapterListPath = "/course-ans/moocstatistics/chapterlist"
	publicKnowledgePath   = "/nodedetailcontroller/visitnodedetail"
)

// Python Chaoxing_Course accepts public-course input examples such as
// https://mooc1.chaoxing.com/course/222529124.html and
// https://mooc1.chaoxing.com/mooc-ans/course/207139102.html. Both forms are
// normalized here to the same xueyinonline public chapter/knowledge APIs.
func (x *chaoxingContext) shouldTryPublicCourseFallback(rawURL, page string, chapters []chaoxingChapter, entries []*extractor.MediaInfo) bool {
	if x.courseID == "" || x.newCourse {
		return false
	}
	if len(entries) == 0 {
		return true
	}
	return isPublicCourseLikeURL(rawURL, page) && len(chapters) == 0
}

func isPublicCourseLikeURL(rawURL, page string) bool {
	low := strings.ToLower(rawURL)
	if strings.Contains(low, "xueyinonline.com/detail/") ||
		strings.Contains(low, "mooc1.xueyinonline.com/course/") ||
		regexp.MustCompile(`(?i)/(?:mooc-ans/)?course/\d+\.html`).FindString(rawURL) != "" {
		return true
	}
	page = strings.ToLower(page)
	return strings.Contains(page, "chapter-list") || strings.Contains(page, "jumpknowledge(") || strings.Contains(page, "visitnodedetail")
}

func (x *chaoxingContext) resolvePublicCourseEntries() []*extractor.MediaInfo {
	return x.resolveXueyinMoocCourse()
}

func (x *chaoxingContext) resolveXueyinMoocCourse() []*extractor.MediaInfo {
	if x.courseID == "" {
		return nil
	}
	body, err := x.getString(x.publicChapterListURL())
	if err != nil || strings.TrimSpace(body) == "" {
		return nil
	}
	if x.title == "" {
		x.title = parseChaoxingTitle(body)
	}
	queue := collectChaoxingPublicChapters(body)
	if len(queue) == 0 {
		return nil
	}

	seenKnowledge := map[string]bool{}
	seenEntries := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(queue))
	for len(queue) > 0 && len(seenKnowledge) < 200 {
		ch := queue[0]
		queue = queue[1:]
		if ch.ID == "" || seenKnowledge[ch.ID] {
			continue
		}
		seenKnowledge[ch.ID] = true
		text, err := x.getString(x.publicKnowledgeURL(ch.ID))
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		resources := collectChaoxingResources([]string{text}, ch.Title)
		for _, res := range resources {
			if res.Title == "" {
				res.Title = ch.Title
			}
			entry := x.resolveResource(res)
			if entry == nil {
				continue
			}
			entry.Title = util.SanitizeFilename(prefixChapterTitle(ch, entry.Title))
			if entry.Extra == nil {
				entry.Extra = map[string]any{}
			}
			entry.Extra["source"] = "public-course"
			entry.Extra["knowledge_id"] = ch.ID
			entries = appendUniqueEntry(entries, entry, seenEntries)
		}
		for _, inner := range collectChaoxingPublicChapters(text) {
			if inner.ID == "" || seenKnowledge[inner.ID] || inner.ID == ch.ID {
				continue
			}
			if inner.Title == "" || strings.HasPrefix(inner.Title, "chapter_") {
				inner.Title = ch.Title
			}
			if inner.Index == 0 {
				inner.Index = len(seenKnowledge) + len(queue) + 1
			}
			queue = append(queue, inner)
		}
	}
	return entries
}

func (x *chaoxingContext) publicChapterListURL() string {
	values := url.Values{}
	values.Set("courseId", x.courseID)
	return strings.TrimRight(x.publicCourseURL, "/") + publicChapterListPath + "?" + values.Encode()
}

func (x *chaoxingContext) publicKnowledgeURL(knowledgeID string) string {
	values := url.Values{}
	values.Set("courseId", x.courseID)
	values.Set("knowledgeId", knowledgeID)
	return strings.TrimRight(x.publicCourseURL, "/") + publicKnowledgePath + "?" + values.Encode()
}

func collectChaoxingPublicChapters(text string) []chaoxingChapter {
	seen := map[string]bool{}
	var out []chaoxingChapter
	add := func(id, title string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, chaoxingChapter{ID: id, Title: firstNonEmpty(publicKnowledgeTitle(title), "chapter_"+id), Index: len(out) + 1})
	}

	for _, loc := range regexp.MustCompile(`(?is)<li\b[\s\S]*?</li>`).FindAllStringIndex(text, -1) {
		chunk := text[loc[0]:loc[1]]
		if id := firstNonEmpty(regexpFirst(chunk, `(?is)jumpKnowledge\((\d+)\)`), regexpFirst(chunk, `(?i)(?:\?|&|&amp;)knowledgeId=(\d+)`)); id != "" {
			add(id, chunk)
		}
	}

	for _, m := range regexp.MustCompile(`(?is)(?:jumpKnowledge\((\d+)\)|(?:\?|&|&amp;)knowledgeId=(\d+))`).FindAllStringSubmatchIndex(text, -1) {
		id := ""
		if m[2] >= 0 {
			id = text[m[2]:m[3]]
		} else if m[4] >= 0 {
			id = text[m[4]:m[5]]
		}
		start := lastIndexFloor(text, "<", m[0], 900)
		end := nextIndexCeil(text, ">", m[1], 1600)
		add(id, text[start:end])
	}
	return out
}

func publicKnowledgeTitle(chunk string) string {
	title := firstNonEmpty(
		stripTags(regexpFirst(chunk, `(?is)<p[^>]*>\s*<a[^>]*>([\s\S]*?)</a>`)),
		stripTags(regexpFirst(chunk, `(?is)<a[^>]*>([\s\S]*?)</a>`)),
		titleFromChunk(chunk),
	)
	title = regexp.MustCompile(`^\s*\d+(?:\.\d+)*\s*`).ReplaceAllString(title, "")
	title = strings.Trim(title, " \t\r\n-—_、.")
	return cleanText(title)
}
