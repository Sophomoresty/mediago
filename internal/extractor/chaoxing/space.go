package chaoxing

import (
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type chaoxingCourseLink struct {
	URL   string
	Title string
}

func isChaoxingSpaceIndexURL(rawURL string) bool {
	return strings.Contains(strings.ToLower(rawURL), "i.mooc.chaoxing.com/space/index")
}

func (x *chaoxingContext) resolveSpaceIndex(rawURL string) (*extractor.MediaInfo, error) {
	body, err := x.getString(rawURL)
	if err != nil {
		return nil, fmt.Errorf("chaoxing space index: %w", err)
	}
	if !hasChaoxingPersonalName(body) {
		return nil, fmt.Errorf("chaoxing space index: login marker personalName not found")
	}
	x.extractAccessFromText(body)
	links := collectChaoxingSpaceCourseLinks(body, rawURL)
	if len(links) == 0 {
		return nil, fmt.Errorf("chaoxing space index: no course links found")
	}

	seen := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(links))
	for i, link := range links {
		child := x.courseChildContext(link.URL)
		course, _, err := child.resolveCourse(link.URL)
		if err != nil || course == nil {
			continue
		}
		courseTitle := firstNonEmpty(course.Title, link.Title, child.title, fmt.Sprintf("course_%d", i+1))
		for _, entry := range course.Entries {
			if entry == nil {
				continue
			}
			entry.Title = util.SanitizeFilename(fmt.Sprintf("[%d]--%s/%s", i+1, courseTitle, firstNonEmpty(entry.Title, "item")))
			if entry.Extra == nil {
				entry.Extra = map[string]any{}
			}
			entry.Extra["source"] = "i.mooc.space"
			entry.Extra["course_url"] = link.URL
			entries = appendUniqueEntry(entries, entry, seen)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("chaoxing space index: no downloadable course resources found")
	}
	return &extractor.MediaInfo{
		Site:    "chaoxing",
		Title:   util.SanitizeFilename(firstNonEmpty(x.title, "chaoxing_space_courses")),
		Entries: entries,
		Extra: compactExtra(map[string]any{
			"source":       "i.mooc.space",
			"course_count": len(links),
		}),
	}, nil
}

func (x *chaoxingContext) courseChildContext(rawURL string) *chaoxingContext {
	child := *x
	child.pathPrefix = ""
	child.newCourse = false
	child.courseID = ""
	child.clazzID = ""
	child.enc = ""
	child.oldEnc = ""
	child.cpi = ""
	child.openc = ""
	child.portalEnc = ""
	child.portalCourseEnc = ""
	child.portalT = ""
	child.title = ""
	child.headers = map[string]string{}
	for k, v := range x.headers {
		child.headers[k] = v
	}
	child.applyURLContext(rawURL)
	child.extractAccessFromURL(rawURL)
	child.extractPortalParams(rawURL)
	return &child
}

func (x *chaoxingContext) applyURLContext(rawURL string) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return
	}
	host := strings.ToLower(u.Host)
	x.sourceHost = host
	customHost := u.Scheme + "://" + u.Host
	path := strings.ToLower(u.Path)
	for _, marker := range []string{"/mycourse/stu", "/mycourse/studentcourse"} {
		if idx := strings.Index(u.Path, marker); idx >= 0 {
			x.pathPrefix = u.Path[:idx]
			break
		}
	}
	if x.pathPrefix != "" && strings.Contains(u.Path, "/course/") {
		x.pathPrefix = u.Path[:strings.Index(u.Path, "/course/")]
	}
	if strings.Contains(path, "/mooc2-ans/") || strings.Contains(path, "/mooc-ans/mycourse/") || queryValue(rawURL, "mooc2") == "1" || queryValue(rawURL, "ismooc2") == "1" {
		x.newCourse = true
	}
	if strings.HasPrefix(host, "mooc") {
		x.courseURL = customHost
		x.headers["Referer"] = x.courseURL + "/"
		x.headers["Origin"] = x.courseURL
		if isChaoxingSchoolHost(host) || strings.Contains(host, "mooc2-ans.") {
			x.newCourseURL = customHost
		}
	}
}

func collectChaoxingSpaceCourseLinks(text, baseURL string) []chaoxingCourseLink {
	seen := map[string]bool{}
	var out []chaoxingCourseLink
	add := func(raw, title string) {
		u := normalizeSpaceURL(raw, baseURL)
		if u == "" || seen[u] || !isChaoxingCourseCandidateURL(u) {
			return
		}
		seen[u] = true
		out = append(out, chaoxingCourseLink{URL: u, Title: cleanText(title)})
	}
	for _, m := range regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>([\s\S]*?)</a>`).FindAllStringSubmatch(text, -1) {
		add(m[1], firstNonEmpty(titleFromChunk(m[0]), stripTags(m[2])))
	}
	for _, m := range regexp.MustCompile(`(?is)(?:href|data-url|data-href|url|linkUrl)\s*=\s*["']([^"']+)["']`).FindAllStringSubmatch(text, -1) {
		add(m[1], "")
	}
	for _, m := range regexp.MustCompile(`(?is)["']((?:https?:\\?/\\?/|/)(?:\\.|[^"'])+?)["']`).FindAllStringSubmatch(text, -1) {
		add(m[1], "")
	}
	for _, link := range collectChaoxingSpaceCourseObjectLinks(text, baseURL) {
		add(link.URL, link.Title)
	}
	return out
}

func collectChaoxingSpaceCourseObjectLinks(text, baseURL string) []chaoxingCourseLink {
	seen := map[string]bool{}
	var out []chaoxingCourseLink
	add := func(link chaoxingCourseLink) {
		link.URL = normalizeSpaceURL(link.URL, baseURL)
		if link.URL == "" || seen[link.URL] || !isChaoxingCourseCandidateURL(link.URL) {
			return
		}
		seen[link.URL] = true
		link.Title = cleanText(link.Title)
		out = append(out, link)
	}

	for _, tag := range regexp.MustCompile(`(?is)<[^>]*(?:courseid|courseId|clazzid|clazzId|classId)[^>]*>`).FindAllString(text, -1) {
		attrs := htmlAttrMap(tag)
		if link, ok := spaceCourseLinkFromMap(attrs, baseURL); ok {
			add(link)
		}
		for _, raw := range attrs {
			for _, link := range spaceCourseLinksFromObjectText(toString(raw), baseURL) {
				add(link)
			}
		}
	}

	for _, link := range spaceCourseLinksFromObjectText(text, baseURL) {
		add(link)
	}
	return out
}

func spaceCourseLinksFromObjectText(text, baseURL string) []chaoxingCourseLink {
	var out []chaoxingCourseLink
	for _, obj := range jsonObjectsInText(htmlpkg.UnescapeString(text)) {
		var payload any
		if json.Unmarshal([]byte(obj), &payload) == nil {
			out = append(out, spaceCourseLinksFromPayload(payload, baseURL)...)
			continue
		}
		if m := looseCourseObjectMap(obj); len(m) > 0 {
			if link, ok := spaceCourseLinkFromMap(m, baseURL); ok {
				out = append(out, link)
			}
		}
	}
	return out
}

func jsonObjectsInText(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		obj, end := balancedJSONObject(text, i)
		if obj == "" {
			continue
		}
		if strings.Contains(strings.ToLower(obj), "courseid") ||
			strings.Contains(strings.ToLower(obj), "clazzid") ||
			strings.Contains(strings.ToLower(obj), "classid") ||
			strings.Contains(strings.ToLower(obj), "courseurl") ||
			strings.Contains(strings.ToLower(obj), "linkurl") {
			out = append(out, strings.ReplaceAll(obj, `\/`, `/`))
		}
		if end > i {
			i = end - 1
		}
	}
	return out
}

func spaceCourseLinksFromPayload(payload any, baseURL string) []chaoxingCourseLink {
	var out []chaoxingCourseLink
	var walk func(any)
	walk = func(v any) {
		switch vv := v.(type) {
		case map[string]any:
			if link, ok := spaceCourseLinkFromMap(vv, baseURL); ok {
				out = append(out, link)
				return
			}
			for _, child := range vv {
				walk(child)
			}
		case []any:
			for _, child := range vv {
				walk(child)
			}
		}
	}
	walk(payload)
	return out
}

func looseCourseObjectMap(obj string) map[string]any {
	keyVariants := []string{
		"courseId", "courseid", "course_id", "moocId", "moocid",
		"clazzId", "clazzid", "classId", "classid", "clazz_id",
		"enc", "encsn", "encSn", "courseEnc", "courseenc",
		"cpi", "courseName", "coursename", "courseTitle", "coursetitle",
		"name", "title", "clazzName", "clazzname", "className", "classname",
		"courseUrl", "courseURL", "courseurl", "studyUrl", "studyURL", "studyurl",
		"jumpUrl", "jumpURL", "jumpurl", "linkUrl", "linkURL", "linkurl",
		"href", "url", "mooc2", "isMooc2", "ismooc2", "isNewCourse", "newCourse",
	}
	out := map[string]any{}
	for _, key := range keyVariants {
		pattern := `(?is)(?:["']?` + regexp.QuoteMeta(key) + `["']?)\s*:\s*(?:"([^"]*)"|'([^']*)'|([a-zA-Z0-9_./:?=&%+\-]+))`
		if m := regexp.MustCompile(pattern).FindStringSubmatch(obj); len(m) > 0 {
			out[key] = htmlpkg.UnescapeString(firstNonEmpty(m[1], m[2], m[3]))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func spaceCourseLinkFromMap(m map[string]any, baseURL string) (chaoxingCourseLink, bool) {
	if len(m) == 0 {
		return chaoxingCourseLink{}, false
	}
	title := directMapString(m, "courseName", "coursename", "courseTitle", "coursetitle", "name", "title", "clazzName", "clazzname", "className", "classname")
	for _, raw := range []string{
		directMapString(m, "courseUrl", "courseURL", "courseurl", "studyUrl", "studyURL", "studyurl", "jumpUrl", "jumpURL", "jumpurl", "linkUrl", "linkURL", "linkurl", "href", "url"),
	} {
		u := normalizeSpaceURL(raw, baseURL)
		if isChaoxingCourseCandidateURL(u) {
			return chaoxingCourseLink{URL: u, Title: title}, true
		}
	}
	courseID := directMapString(m, "courseId", "courseid", "course_id", "moocId", "moocid")
	clazzID := directMapString(m, "clazzId", "clazzid", "classId", "classid", "clazz_id")
	enc := directMapString(m, "enc", "encsn", "encSn", "courseEnc", "courseenc")
	if courseID == "" || clazzID == "" || enc == "" {
		return chaoxingCourseLink{}, false
	}
	cpi := directMapString(m, "cpi")
	newCourse := mapBoolish(m, "mooc2", "isMooc2", "ismooc2", "isNewCourse", "newCourse")
	if raw := directMapString(m, "courseUrl", "courseurl", "studyUrl", "studyurl", "jumpUrl", "jumpurl", "linkUrl", "linkurl", "href", "url"); strings.Contains(strings.ToLower(raw), "mooc2-ans") || queryValue(raw, "mooc2") == "1" {
		newCourse = true
	}
	return chaoxingCourseLink{URL: synthesizeSpaceCourseURL(courseID, clazzID, enc, cpi, newCourse, baseURL), Title: title}, true
}

func synthesizeSpaceCourseURL(courseID, clazzID, enc, cpi string, newCourse bool, baseURL string) string {
	values := url.Values{}
	if newCourse {
		values.Set("courseid", courseID)
	} else {
		values.Set("courseId", courseID)
	}
	values.Set("clazzid", clazzID)
	if cpi != "" {
		values.Set("cpi", cpi)
	}
	values.Set("enc", enc)
	if newCourse {
		return spaceCourseOrigin(baseURL, defaultNewHost) + "/mooc2-ans/mycourse/studentcourse?" + values.Encode()
	}
	return spaceCourseOrigin(baseURL, defaultCourseHost) + "/mycourse/studentcourse?" + values.Encode()
}

func spaceCourseOrigin(baseURL, fallback string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fallback
	}
	host := strings.ToLower(u.Host)
	if !strings.Contains(host, "chaoxing.com") && !strings.Contains(host, "xueyinonline.com") {
		return u.Scheme + "://" + u.Host
	}
	if strings.HasPrefix(host, "mooc") {
		return u.Scheme + "://" + u.Host
	}
	return fallback
}

func htmlAttrMap(tag string) map[string]any {
	out := map[string]any{}
	for _, m := range regexp.MustCompile(`(?is)([a-zA-Z_:][-\w:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`).FindAllStringSubmatch(tag, -1) {
		key := normalizeAttrCourseKey(m[1])
		out[key] = htmlpkg.UnescapeString(firstNonEmpty(m[2], m[3]))
	}
	return out
}

func normalizeAttrCourseKey(key string) string {
	key = strings.TrimSpace(key)
	lower := strings.ToLower(key)
	lower = strings.TrimPrefix(lower, "data-")
	lower = strings.ReplaceAll(lower, "-", "")
	switch lower {
	case "courseid":
		return "courseId"
	case "clazzid":
		return "clazzId"
	case "classid":
		return "classId"
	case "courseurl":
		return "courseUrl"
	case "studyurl":
		return "studyUrl"
	case "jumpurl":
		return "jumpUrl"
	case "linkurl":
		return "linkUrl"
	case "coursename":
		return "courseName"
	case "coursetitle":
		return "courseTitle"
	case "clazzname":
		return "clazzName"
	case "classname":
		return "className"
	case "ismooc2":
		return "isMooc2"
	case "isnewcourse":
		return "isNewCourse"
	case "enc", "cpi", "href", "url", "name", "title", "mooc2", "newcourse":
		return lower
	default:
		return key
	}
}

func directMapString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		for mk, mv := range m {
			if strings.EqualFold(mk, key) {
				if s := strings.TrimSpace(toString(mv)); s != "" {
					return htmlpkg.UnescapeString(s)
				}
			}
		}
	}
	return ""
}

func mapBoolish(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		for mk, mv := range m {
			if !strings.EqualFold(mk, key) {
				continue
			}
			switch v := mv.(type) {
			case bool:
				return v
			case string:
				s := strings.TrimSpace(strings.ToLower(v))
				return s == "1" || s == "true" || s == "yes" || s == "y"
			default:
				return strings.TrimSpace(toString(v)) == "1"
			}
		}
	}
	return false
}

func normalizeSpaceURL(raw, baseURL string) string {
	raw = strings.TrimSpace(htmlpkg.UnescapeString(raw))
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
		return ""
	}
	raw = strings.ReplaceAll(raw, `\/`, `/`)
	raw = strings.ReplaceAll(raw, `\u0026`, "&")
	raw = strings.ReplaceAll(raw, `\\u0026`, "&")
	raw = strings.Trim(raw, `"'`)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if !isHTTPURL(raw) && baseURL != "" {
		raw = resolveRelativeURL(baseURL, raw)
	}
	return raw
}

func isChaoxingCourseCandidateURL(rawURL string) bool {
	low := strings.ToLower(rawURL)
	if strings.Contains(low, "i.mooc.chaoxing.com/space/index") {
		return false
	}
	if strings.Contains(low, "/mycourse/stu") ||
		strings.Contains(low, "/mycourse/studentcourse") ||
		strings.Contains(low, "/visit/stucoursemiddle") ||
		strings.Contains(low, "/courseportal/portal/") ||
		strings.Contains(low, "/course-ans/courseportal/") ||
		strings.Contains(low, "/ps/") ||
		strings.Contains(low, "xueyinonline.com/detail/") {
		return true
	}
	if regexp.MustCompile(`(?i)/(?:mooc-ans/)?course/\d+\.html`).FindString(rawURL) != "" {
		return true
	}
	return strings.Contains(low, "courseid=") && (strings.Contains(low, "clazzid=") || strings.Contains(low, "enc=") || strings.Contains(low, "chapterid="))
}

func hasChaoxingPersonalName(text string) bool {
	return regexp.MustCompile(`(?is)<p\s+[^>]*class=["'][^"']*\bpersonalName\b[^"']*["'][\s\S]*?>`).MatchString(text)
}
