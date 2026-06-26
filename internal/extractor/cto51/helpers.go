package cto51

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

func parseRoute(raw string) route {
	var r route
	r.CID = extractFirst(courseRe, raw)
	r.LID = extractFirst(lessonRe, raw)
	r.TrainID = extractFirst(trainRe, raw)
	r.TrainCourseID = extractFirst(trainCourseRe, raw)
	return r
}

type lessonRef struct{ ID, Title string }

func parseLessonLinks(body string) []lessonRef {
	seen := map[string]bool{}
	var out []lessonRef
	for _, m := range lessonLinkRe.FindAllStringSubmatch(body, -1) {
		if m[2] == "" || seen[m[2]] {
			continue
		}
		seen[m[2]] = true
		out = append(out, lessonRef{ID: m[2], Title: cleanText(m[3])})
	}
	return out
}

func headers(raw string) map[string]string {
	return map[string]string{"Accept": "application/json, text/plain, */*", "Origin": "https://edu.51cto.com", "Referer": firstNonEmpty(raw, "https://edu.51cto.com/")}
}
func addQuery(base string, params map[string]string) string {
	if len(params) == 0 {
		return base
	}
	q := url.Values{}
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	if strings.Contains(base, "?") {
		return base + "&" + q.Encode()
	}
	return base + "?" + q.Encode()
}
func mediaInfo(title, u, format string, h map[string]string) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "cto51", Title: util.SanitizeFilename(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{u}, Format: format, Headers: h}}}
}
func firstMedia(list []media) media {
	if len(list) > 0 {
		return list[0]
	}
	return media{}
}
func normalizeText(s string) string {
	s = html.UnescapeString(strings.TrimSpace(strings.Trim(s, `"'`)))
	s = strings.ReplaceAll(s, `\/`, "/")
	return strings.TrimRight(s, `"' )],;`)
}
func extractTitle(body string) string {
	m := titleRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return firstNonEmpty(cleanText(m[1]), cleanText(m[2]))
}
func cleanText(s string) string {
	return strings.Join(strings.Fields(regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(html.UnescapeString(s), " ")), " ")
}
func textValue(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func extractFirst(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}
