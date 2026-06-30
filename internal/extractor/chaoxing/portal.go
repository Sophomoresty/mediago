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

const (
	urlNewIndex        = "https://www.xueyinonline.com/detail/%s"
	urlCourse          = "https://mooc1.chaoxing.com/mycourse/studentcourse?courseId=%s&clazzid=%s&enc=%s"
	urlNewCourse       = "https://mooc2-ans.chaoxing.com/mooc2-ans/mycourse/studentcourse?courseid=%s&clazzid=%s&enc=%s"
	urlJoined          = "https://www.xueyinonline.com/detail/clazz-tag?courseId=%s&enc=%s"
	urlOldJoined       = "https://mooc1.chaoxing.com/course/clazzTag?courseId=%s"
	urlMap             = "https://www.xueyinonline.com/detail/get-map-url?courseId=%s&enc=%s"
	urlApply           = "https://www.xueyinonline.com/detail/apply-course?courseId=%s&clazzId=%s&enc=%s"
	urlPortalLookIndex = "https://k.chaoxing.com/res/look/index.html"

	portalBasicInfoPath   = "/course-ans/courseportal/portal-basic-info?showCustomArticle=true&cssType=1"
	portalNodeListPath    = "/course-ans/moocstatistics/portal-node-list"
	portalNodeResourceURL = "/course-ans/courseportal/portal-node-resource"
	portalBtnStatePath    = "/course-ans/courseportal/portalbtnstate"
)

func isPortalLikeURL(rawURL, page string) bool {
	low := strings.ToLower(rawURL)
	if strings.Contains(low, "/courseportal/portal/") || strings.Contains(low, "/course-ans/courseportal/") || strings.Contains(low, "xueyinonline.com/detail/") || strings.Contains(low, "xueyinonline.com/portal/new-header") || strings.Contains(low, "i.mooc.chaoxing.com/space/index") || strings.Contains(low, "k.chaoxing.com/res/look/index.html") || strings.Contains(low, "/ps/") {
		return true
	}
	if regexp.MustCompile(`(?i)/(?:mooc-ans/)?course/\d+\.html`).FindString(rawURL) != "" {
		return true
	}
	page = strings.ToLower(page)
	return strings.Contains(page, "portalenc") || strings.Contains(page, "portal-node-resource") || strings.Contains(page, "portal-node-list")
}

func (x *chaoxingContext) extractPortalParams(text string) {
	text = htmlpkg.UnescapeString(text)
	if v := hiddenValue(text, "courseId"); v != "" {
		x.courseID = firstNonEmpty(x.courseID, v)
	}
	if x.courseID == "" {
		x.courseID = firstNonEmpty(x.courseID, regexpFirst(text, `(?i)(?:\?|&|&amp;)courseid=(\d+)`), regexpFirst(text, `(?i)/(?:detail|course|ps)/(\d+)(?:\.html)?`), regexpFirst(text, `(?i)/mooc-ans/course/(\d+)\.html`))
	}
	if v := hiddenValue(text, "courseEnc"); v != "" {
		x.portalCourseEnc = firstNonEmpty(x.portalCourseEnc, v)
	}
	x.portalCourseEnc = firstNonEmpty(x.portalCourseEnc, regexpFirst(text, `(?i)(?:\?|&|&amp;)courseEnc=([a-z0-9]+)`), regexpFirst(text, `(?i)courseEnc["']?\s*[:=]\s*["']([a-z0-9]+)`))
	if v := hiddenValue(text, "portalEnc"); v != "" {
		x.portalEnc = firstNonEmpty(x.portalEnc, v)
	}
	x.portalEnc = firstNonEmpty(x.portalEnc, regexpFirst(text, `(?i)(?:\?|&|&amp;)portalEnc=([a-z0-9]+)`), regexpFirst(text, `(?i)portalEnc["']?\s*[:=]\s*["']([a-z0-9]+)`))
	x.portalT = firstNonEmpty(x.portalT, regexpFirst(text, `(?i)(?:\?|&|&amp;)t=(\d+)`), hiddenValue(text, "t"))
}

func (x *chaoxingContext) resolvePortalAccess(rawURL, page string) {
	x.extractPortalParams(rawURL)
	x.extractPortalParams(page)
	if x.title == "" || x.title == "课程门户首页" {
		x.title = x.portalTitle(page)
	}
	if x.clazzID != "" && x.enc != "" {
		return
	}
	for _, probe := range x.portalProbeURLs(rawURL) {
		body, err := x.getString(probe)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}
		x.extractAccessFromText(body)
		x.extractPortalParams(body)
		if x.title == "" {
			x.title = parseChaoxingTitle(body)
		}
		if x.clazzID != "" && x.enc != "" {
			return
		}
	}
	if body := x.portalJoinText(); body != "" {
		x.extractAccessFromText(body)
		x.extractPortalParams(body)
	}
}

func (x *chaoxingContext) portalProbeURLs(rawURL string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	if isHTTPURL(rawURL) {
		add(rawURL)
	}
	if x.courseID != "" {
		add(fmt.Sprintf(urlNewIndex, url.PathEscape(x.courseID)))
		add(fmt.Sprintf("https://mooc1.chaoxing.com/course/%s.html", url.PathEscape(x.courseID)))
		add(fmt.Sprintf("https://mooc1.chaoxing.com/mooc-ans/course/%s.html", url.PathEscape(x.courseID)))
		add(fmt.Sprintf(urlOldJoined, url.QueryEscape(x.courseID)))
		enc := firstNonEmpty(x.enc, x.portalCourseEnc, x.portalEnc)
		if enc != "" {
			add(fmt.Sprintf(urlJoined, url.QueryEscape(x.courseID), url.QueryEscape(enc)))
			add(fmt.Sprintf(urlMap, url.QueryEscape(x.courseID), url.QueryEscape(enc)))
		}
	}
	return out
}

func (x *chaoxingContext) portalJoinText() string {
	if x.courseID == "" || x.portalEnc == "" {
		return ""
	}
	values := url.Values{}
	values.Set("courseId", x.courseID)
	values.Set("clazzId", "0")
	values.Set("portalEnc", x.portalEnc)
	body, err := x.getString(x.abs(portalBtnStatePath) + "?" + values.Encode())
	if err != nil || strings.TrimSpace(body) == "" {
		return ""
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) == nil {
		jump := firstFieldString(payload, "jumpUrl", "url", "linkUrl")
		if jump != "" {
			if fetched, ferr := x.getString(resolveRelativeURL(x.courseURL+"/", jump)); ferr == nil && strings.TrimSpace(fetched) != "" {
				return fetched
			}
		}
	}
	return body
}

func (x *chaoxingContext) portalAPIURL(path string, includeCourse bool, extra url.Values) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	base := path
	if !isHTTPURL(base) {
		base = x.abs(path)
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	q := parsed.Query()
	if x.portalCourseEnc != "" {
		q.Set("courseEnc", x.portalCourseEnc)
	}
	if x.portalT != "" {
		q.Set("t", x.portalT)
	}
	if includeCourse && x.courseID != "" {
		q.Set("courseId", x.courseID)
	}
	for k, vals := range extra {
		for _, v := range vals {
			q.Add(k, v)
		}
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

func (x *chaoxingContext) requestPortalJSON(path string, includeCourse bool, extra url.Values) any {
	api := x.portalAPIURL(path, includeCourse, extra)
	if api == "" {
		return nil
	}
	body, err := x.getString(api)
	if err != nil || strings.TrimSpace(body) == "" {
		return nil
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) != nil {
		return nil
	}
	return payload
}

func (x *chaoxingContext) portalTitle(page string) string {
	payload := x.requestPortalJSON(portalBasicInfoPath, true, nil)
	if t := firstFieldString(payload, "courseName", "courseNameUS", "name", "title"); t != "" {
		return cleanText(t)
	}
	if t := parseChaoxingTitle(page); t != "" && t != "课程门户首页" {
		return t
	}
	return ""
}

func (x *chaoxingContext) resolvePortalResourceEntries() []*extractor.MediaInfo {
	extra := url.Values{}
	extra.Set("showResourceCount", "true")
	payload := x.requestPortalJSON(portalNodeResourceURL, true, extra)
	maps := portalResourceMaps(payload)
	out := make([]*extractor.MediaInfo, 0, len(maps))
	seen := map[string]bool{}
	for i, item := range maps {
		entry := x.resolvePortalResourceMap(item, i+1)
		out = appendUniqueEntry(out, entry, seen)
	}
	return out
}

func portalResourceMaps(payload any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(v any) {
		switch vv := v.(type) {
		case map[string]any:
			if _, ok := resourceFromMap(vv); ok {
				out = append(out, vv)
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

func (x *chaoxingContext) resolvePortalResourceMap(item map[string]any, index int) *extractor.MediaInfo {
	res, ok := resourceFromMap(item)
	if !ok {
		return nil
	}
	if status := normalizeURL(firstFieldString(item, "statusUrl")); status != "" {
		if strings.HasPrefix(status, "/") {
			status = resolveRelativeURL(x.courseURL+"/", status)
		}
		if body, err := x.getString(status); err == nil {
			var payload any
			if json.Unmarshal([]byte(body), &payload) == nil {
				if u := firstURLMatching(payload, isHTTPURL); u != "" {
					res.RawURL = u
				}
				res.ObjectID = firstNonEmpty(res.ObjectID, firstFieldString(payload, "objectId", "objectid"))
			}
		}
	}
	entry := x.resolveResource(res)
	if entry == nil {
		return nil
	}
	entry.Title = util.SanitizeFilename(fmt.Sprintf("[%d]--%s", index, entry.Title))
	if entry.Extra == nil {
		entry.Extra = map[string]any{}
	}
	entry.Extra["source"] = "portal-node-resource"
	if nodeID := firstFieldString(item, "nodeId", "nodeid"); nodeID != "" {
		entry.Extra["node_id"] = nodeID
	}
	return entry
}

func resolveRelativeURL(base, ref string) string {
	if isHTTPURL(ref) {
		return ref
	}
	u, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return u.ResolveReference(r).String()
}
