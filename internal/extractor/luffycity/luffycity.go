// Package luffycity implements an extractor for luffycity.com (路飞学城) courses.
package luffycity

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlReferer = "https://www.luffycity.com/"
	urlOrigin  = "https://www.luffycity.com"
	urlAPIBase = "https://api.luffycity.com/api/v1"
	urlCDN     = "https://hcdn2.luffycity.com"
	luffyUA    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var patterns = []string{`(?:[\w-]+\.)?luffycity\.com/`, `路飞学城`, `luffycity`}

func init() {
	extractor.Register(&Luffycity{}, extractor.SiteInfo{Name: "Luffycity", URL: "luffycity.com", NeedAuth: true})
}

type Luffycity struct{}

func (l *Luffycity) Patterns() []string { return patterns }

type luffySession struct {
	Cookie, Token string
	Headers       map[string]string
	Logined       bool
}

type luffyTarget struct {
	CID, CourseType, SectionID, Title string
	PlayMode, StudyModule, Purchased  bool
}

type luffyItem struct {
	Kind, Title, SectionID, DirectURL, FileURL, FileFmt string
	CanPlay                                             bool
}

var (
	playRe   = regexp.MustCompile(`(?i)/play/([0-9]+)`)
	studyRe  = regexp.MustCompile(`(?i)/study/chapter/([0-9]+)`)
	actualRe = regexp.MustCompile(`(?i)/actual-course/([0-9]+)`)
	degreeRe = regexp.MustCompile(`(?i)/employment-course/([0-9]+)`)
	freeRe   = regexp.MustCompile(`(?i)/free-course/([0-9]+)`)
	courseRe = regexp.MustCompile(`(?i)/course/(?:vip|detail|free|actual|degree)/([0-9]+)`)
)

func (l *Luffycity) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("luffycity requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	sess, err := luffyBuildSession(c, opts.Cookies)
	if err != nil {
		return nil, err
	}
	target, err := luffyResolveTarget(c, sess, rawURL)
	if err != nil {
		return nil, err
	}
	if target.Title == "" {
		target.Title = luffyFetchTitle(c, sess, &target)
	}
	payload := luffyFetchSections(c, sess, target)
	items := luffyCollectItems(payload, nil, sess.Logined || target.Purchased)
	entries := make([]*extractor.MediaInfo, 0, len(items))
	for _, item := range items {
		entry, err := luffyBuildEntry(c, sess, item)
		if err == nil && entry != nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("luffycity: no playable entries for course=%s section=%s", target.CID, target.SectionID)
	}
	if target.Title == "" {
		target.Title = firstText("路飞学城课程" + firstText(target.CID, target.SectionID))
	}
	return &extractor.MediaInfo{Site: "luffycity", Title: target.Title, Entries: entries, Extra: map[string]any{"course_id": target.CID, "course_type": target.CourseType, "section_id": target.SectionID, "play_mode": target.PlayMode}}, nil
}

func luffyBuildSession(c *util.Client, jar http.CookieJar) (*luffySession, error) {
	cookie := luffyCookieString(jar)
	token := firstText(cookieValue(cookie, "luffy-client-key"), cookieValue(cookie, "token"), cookieValue(cookie, "key"))
	if token == "" {
		return nil, fmt.Errorf("luffycity requires luffy-client-key/token cookie")
	}
	headers := map[string]string{"cookie": cookie, "Origin": urlOrigin, "Referer": urlReferer, "Accept": "application/json, text/plain, */*", "User-Agent": luffyUA, "Authorization": "Token " + token}
	sess := &luffySession{Cookie: cookie, Token: token, Headers: headers}
	for _, path := range []string{"/auth/token/", "/study/courses/"} {
		resp, err := luffyAPIGet(c, path, nil, headers)
		if err == nil && firstText(resp["code"]) != "401" {
			sess.Logined = true
			return sess, nil
		}
	}
	return nil, fmt.Errorf("luffycity requires valid login token")
}

func luffyResolveTarget(c *util.Client, sess *luffySession, rawURL string) (luffyTarget, error) {
	var t luffyTarget
	switch {
	case playRe.MatchString(rawURL):
		t.SectionID = playRe.FindStringSubmatch(rawURL)[1]
		t.PlayMode = true
	case studyRe.MatchString(rawURL):
		t.CID, t.CourseType, t.StudyModule = studyRe.FindStringSubmatch(rawURL)[1], "degree", true
	case actualRe.MatchString(rawURL):
		t.CID, t.CourseType = actualRe.FindStringSubmatch(rawURL)[1], "actual"
	case degreeRe.MatchString(rawURL):
		t.CID, t.CourseType = degreeRe.FindStringSubmatch(rawURL)[1], "degree"
	case freeRe.MatchString(rawURL):
		t.CID, t.CourseType = freeRe.FindStringSubmatch(rawURL)[1], "free"
	case courseRe.MatchString(rawURL):
		t.CID, t.CourseType = courseRe.FindStringSubmatch(rawURL)[1], "actual"
	}
	if u, err := url.Parse(rawURL); err == nil {
		q := u.Query()
		if t.CID == "" {
			t.CID = firstText(q.Get("course_id"), q.Get("courseId"), q.Get("cid"), q.Get("id"))
		}
		if t.SectionID == "" {
			t.SectionID = firstText(q.Get("section_id"), q.Get("sectionId"))
		}
	}
	courses := luffyFetchCourseList(c, sess)
	if picked := luffyPickCourse(courses, t.CID, t.CourseType); picked.CID != "" {
		t.CID, t.CourseType, t.Title, t.Purchased = picked.CID, picked.CourseType, picked.Title, picked.Purchased
	} else if t.CID == "" && !t.PlayMode && len(courses) > 0 {
		t = courses[0]
	}
	if t.PlayMode && t.SectionID != "" {
		return t, nil
	}
	if t.CID == "" {
		return t, fmt.Errorf("cannot parse luffycity course or play id from URL: %s", rawURL)
	}
	if t.CourseType == "" {
		t.CourseType = "actual"
	}
	return t, nil
}

func luffyFetchCourseList(c *util.Client, sess *luffySession) []luffyTarget {
	var out []luffyTarget
	seen := map[string]bool{}
	if sess.Logined {
		for _, path := range []string{"/study/courses/", "/study/category-courses/"} {
			if resp, err := luffyAPIGet(c, path, nil, sess.Headers); err == nil {
				luffyAppendCourses(&out, luffyAPIData(resp), "", seen)
			}
		}
	}
	for _, typ := range []string{"free", "actual", "degree"} {
		for offset := 0; offset < 1000; offset += 100 {
			resp, err := luffyAPIGet(c, "/course/"+typ+"/", map[string]string{"limit": "100", "offset": strconv.Itoa(offset)}, sess.Headers)
			if err != nil {
				break
			}
			data := luffyAPIData(resp)
			before := len(out)
			luffyAppendCourses(&out, data, typ, seen)
			if mapAny(data)["next"] == nil || len(out) == before {
				break
			}
		}
	}
	if sess.Logined {
		luffyAppendVIPCardCourses(c, sess, &out, seen)
	}
	return out
}

// luffyAppendVIPCardCourses fetches VIP card subscriptions and appends their
// courses to the list.  Mirrors Luffycity_Course._append_vip_card_courses.
//
// Source: GET /study/vip-card/  -> list of cards
//
//	GET /study/vip-card/{number}/ -> card detail with category_courses
func luffyAppendVIPCardCourses(c *util.Client, sess *luffySession, out *[]luffyTarget, seen map[string]bool) {
	cardsRaw := luffyGetData(c, "/study/vip-card/", nil, sess.Headers)
	cards, ok := cardsRaw.([]any)
	if !ok || len(cards) == 0 {
		return
	}
	// Build lookup of existing courses by (course_id, title) so we can mark
	// them purchased instead of duplicating.
	type cidTitle struct{ cid, title string }
	existing := make(map[cidTitle]int, len(*out))
	for i, t := range *out {
		existing[cidTitle{t.CID, t.Title}] = i
	}
	for _, rawCard := range cards {
		card, ok := rawCard.(map[string]any)
		if !ok {
			continue
		}
		if v, exists := card["is_valid"]; exists && v == false {
			continue
		}
		number := firstText(card["number"])
		if number == "" {
			continue
		}
		detailRaw := luffyGetData(c, fmt.Sprintf("/study/vip-card/%s/", url.PathEscape(number)), nil, sess.Headers)
		detail, ok := detailRaw.(map[string]any)
		if !ok {
			continue
		}
		catCoursesRaw := detail["category_courses"]
		if catCoursesRaw == nil {
			continue
		}
		catCourses, ok := catCoursesRaw.([]any)
		if !ok {
			continue
		}
		for _, rawCat := range catCourses {
			cat, ok := rawCat.(map[string]any)
			if !ok {
				continue
			}
			coursesRaw := cat["courses"]
			if coursesRaw == nil {
				continue
			}
			courses, ok := coursesRaw.([]any)
			if !ok {
				continue
			}
			for _, rawCourse := range courses {
				cm, ok := rawCourse.(map[string]any)
				if !ok {
					continue
				}
				norm := luffyNormalizeCourse(cm, "actual")
				if norm.CID == "" {
					continue
				}
				norm.CourseType = "actual"
				norm.Purchased = true
				key := cidTitle{norm.CID, norm.Title}
				if idx, dup := existing[key]; dup {
					(*out)[idx].Purchased = true
					continue
				}
				seenKey := norm.CourseType + ":" + norm.CID
				if seen[seenKey] {
					continue
				}
				seen[seenKey] = true
				existing[key] = len(*out)
				*out = append(*out, norm)
			}
		}
	}
}

func luffyAppendCourses(out *[]luffyTarget, v any, defaultType string, seen map[string]bool) {
	switch x := v.(type) {
	case []any:
		for _, it := range x {
			luffyAppendCourses(out, it, defaultType, seen)
		}
	case map[string]any:
		if c := luffyNormalizeCourse(x, defaultType); c.CID != "" {
			key := c.CourseType + ":" + c.CID
			if !seen[key] {
				seen[key] = true
				*out = append(*out, c)
			}
		}
		for _, k := range []string{"result", "list", "items", "records", "courses", "enrolled_courses", "children", "data"} {
			luffyAppendCourses(out, x[k], defaultType, seen)
		}
	}
}

func luffyNormalizeCourse(m map[string]any, defaultType string) luffyTarget {
	cid := firstText(m["course_id"], m["courseId"], m["id"], m["cid"], m["degree_course_id"], m["actual_course_id"])
	title := firstText(m["course_name"], m["courseName"], m["name"], m["title"])
	if cid == "" || title == "" {
		for _, k := range []string{"course", "course_info", "courseInfo", "degree_course", "actual_course", "free_course"} {
			if c := luffyNormalizeCourse(mapAny(m[k]), defaultType); c.CID != "" {
				return c
			}
		}
		return luffyTarget{}
	}
	typ := luffyTypeName(firstText(m["course_type"], m["courseType"], m["type"], defaultType))
	return luffyTarget{CID: cid, CourseType: typ, Title: title, Purchased: boolOf(m["is_valid"]) || boolOf(m["is_buy"]) || boolOf(m["isBuy"]) || boolOf(m["purchased"]) || boolOf(m["has_buy"])}
}

func luffyPickCourse(courses []luffyTarget, cid, typ string) luffyTarget {
	for _, c := range courses {
		if cid != "" && c.CID != cid {
			continue
		}
		if typ != "" && c.CourseType != typ {
			continue
		}
		return c
	}
	return luffyTarget{}
}

func luffyFetchTitle(c *util.Client, sess *luffySession, t *luffyTarget) string {
	var data any
	if t.PlayMode && t.SectionID != "" {
		data = luffyGetData(c, fmt.Sprintf("/play/%s/", t.SectionID), nil, sess.Headers)
		return firstText(nested(data, "course_name"), nested(data, "courseName"), nested(data, "name"), nested(data, "title"), "路飞学城课程"+t.SectionID)
	}
	if t.StudyModule {
		data = luffyGetData(c, fmt.Sprintf("/study/module/degree/%s/", t.CID), nil, sess.Headers)
	} else {
		data = luffyGetData(c, fmt.Sprintf("/course/%s/%s/", firstText(t.CourseType, "actual"), t.CID), nil, sess.Headers)
	}
	return firstText(nested(data, "name"), nested(data, "title"), nested(data, "course_name"), "路飞学城课程"+t.CID)
}

func luffyFetchSections(c *util.Client, sess *luffySession, t luffyTarget) any {
	if t.PlayMode && t.SectionID != "" {
		return luffyGetData(c, "/play/sections/", map[string]string{"section_id": t.SectionID}, sess.Headers)
	}
	if t.StudyModule {
		return luffyGetData(c, fmt.Sprintf("/study/module/degree/%s/", t.CID), nil, sess.Headers)
	}
	return luffyGetData(c, fmt.Sprintf("/course/%s/%s/sections/", firstText(t.CourseType, "actual"), t.CID), nil, sess.Headers)
}

func luffyCollectItems(v any, prefix []int, canPlay bool) []luffyItem {
	var out []luffyItem
	m := mapAny(v)
	if len(m) > 0 {
		if luffyIsVideo(m) {
			out = append(out, luffyMakeVideoItem(m, prefix, canPlay))
		}
		if att := firstText(m["attachment_path"]); att != "" {
			out = append(out, luffyItem{Kind: "file", Title: indexedTitle(prefix, firstText(m["name"], m["title"], "资料")), FileURL: luffyNormalizeURL(att, true), FileFmt: mediaExt(att)})
		}
	}
	children := childMaps(v)
	for i, child := range children {
		out = append(out, luffyCollectItems(child, append(append([]int{}, prefix...), i+1), canPlay)...)
	}
	return out
}

func luffyBuildEntry(c *util.Client, sess *luffySession, item luffyItem) (*extractor.MediaInfo, error) {
	if item.Kind == "file" {
		return &extractor.MediaInfo{Site: "luffycity", Title: item.Title, Streams: map[string]extractor.Stream{"default": {Quality: "default", URLs: []string{item.FileURL}, Format: item.FileFmt, Headers: sess.Headers}}}, nil
	}
	source, err := luffyResolvePlaySource(c, sess, item)
	if err != nil || source.URL == "" {
		return nil, fmt.Errorf("luffycity: empty source for section=%s", item.SectionID)
	}
	extra := map[string]any{"section_id": item.SectionID, "source_type": source.Type}
	for k, v := range source.Extra {
		extra[k] = v
	}
	headers := sess.Headers
	if source.Headers != nil {
		headers = source.Headers
	}
	format := mediaExt(source.URL)
	if source.Type == "m3u8_text" || source.Type == "polyv_pdx" {
		format = "m3u8"
	}
	streamExtra := luffyStreamExtra(source)
	return &extractor.MediaInfo{Site: "luffycity", Title: item.Title, Streams: map[string]extractor.Stream{"default": {Quality: "default", URLs: []string{source.URL}, Format: format, Size: source.Size, NeedMerge: format == "m3u8" || format == "pdx", Headers: headers, Extra: streamExtra}}, Extra: extra}, nil
}

func luffyStreamExtra(source luffySource) map[string]any {
	extra := map[string]any{}
	if meta, ok := source.Extra["m3u8_meta"]; ok && meta != nil {
		extra["m3u8_meta"] = meta
	}
	if cryptor, ok := source.Extra["cryptor"]; ok && cryptor != nil {
		extra["cryptor"] = cryptor
	} else if source.Type == "polyv_pdx" {
		extra["cryptor"] = "polyv_pdx"
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func luffyResolvePlaySource(c *util.Client, sess *luffySession, item luffyItem) (luffySource, error) {
	if u := luffyNormalizeMediaURL(item.DirectURL); u != "" {
		return luffySource{URL: u, Type: mediaExt(u)}, nil
	}
	if item.SectionID == "" {
		return luffySource{}, fmt.Errorf("missing section id")
	}
	play := luffyGetData(c, fmt.Sprintf("/play/%s/", item.SectionID), nil, sess.Headers)
	player := strings.ToUpper(firstText(nested(play, "player")))
	auth := mapAny(nested(play, "auth_info"))
	if player == "POLYV" || firstText(auth["vid"], auth["video_id"], auth["videoId"]) != "" {
		vid := firstText(auth["vid"], auth["video_id"], auth["videoId"])
		polyvHeaders := luffyPolyvHeaders(sess)
		sec, err := shared.PolyvResolveSecure(c, vid, polyvHeaders)
		if err == nil {
			if u, err := shared.PolyvPickBestManifest(sec); err == nil {
				u = shared.PolyvNormalizeManifestURL(u)
				token := firstText(auth["token"], auth["play_safe_token"], auth["playSafeToken"], auth["playsafe"], sec.PlayToken())
				if strings.Contains(strings.ToLower(u), ".pdx") {
					if pdx, err := shared.PolyvResolvePDX(c, shared.PolyvPDXOptions{VideoID: vid, PDXURL: u, PlaySafeToken: token, Headers: polyvHeaders, Secure: sec}); err == nil {
						extra := map[string]any{"polyv_vid": vid, "source_type": "polyv_pdx", "m3u8_meta": pdx.M3U8Meta()}
						for k, v := range pdx.ExtraMap() {
							extra[k] = v
						}
						return luffySource{URL: luffyM3U8DataURL(pdx.M3U8Text), Type: "polyv_pdx", Headers: polyvHeaders, Extra: extra}, nil
					}
				}
				src := luffySource{URL: u, Type: "m3u8_url", Headers: polyvHeaders, Extra: map[string]any{"polyv_vid": vid}}
				if text, err := c.GetString(u, polyvHeaders); err == nil {
					if rewritten, err := shared.PolyvRewriteM3U8KeysWithOptions(c, text, shared.PolyvRewriteOptions{Token: token, Referer: urlReferer, ManifestURL: u, SeedConst: sec.SeedConst()}); err == nil {
						src.URL = luffyM3U8DataURL(rewritten)
						src.Extra["m3u8_text"] = rewritten
						src.Type = "m3u8_text"
					}
				}
				return src, nil
			}
		}
	}
	if player == "ALI" || player == "ALIYUN" || firstText(auth["play_auth"], auth["playAuth"], auth["playauth"]) != "" {
		if src := luffyResolveAliyun(c, sess, auth); src.URL != "" {
			return src, nil
		}
	}
	for _, u := range luffyCollectMedia(play) {
		return luffySource{URL: u, Type: mediaExt(u)}, nil
	}
	return luffySource{}, fmt.Errorf("unsupported luffycity playback source")
}

func luffyPolyvHeaders(sess *luffySession) map[string]string {
	return map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"Origin":     urlOrigin,
		"Referer":    urlReferer,
		"User-Agent": sess.Headers["User-Agent"],
	}
}

func luffyAPIGet(c *util.Client, path string, params map[string]string, headers map[string]string) (map[string]any, error) {
	body, err := c.GetString(luffyAPIURL(path, params), headers)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func luffyGetData(c *util.Client, path string, params map[string]string, headers map[string]string) any {
	resp, err := luffyAPIGet(c, path, params, headers)
	if err != nil {
		return map[string]any{}
	}
	return luffyAPIData(resp)
}
