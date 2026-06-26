// Package duanshu implements the Duanshu h5/fairy content extractor.
package duanshu

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	referer               = "https://h5.duanshu.com"
	user_detail_url       = "https://api.duanshu.com/h5/user/detail"
	shop_identifier_url   = "https://api.duanshu.com/fairy/api/v1/shop/identifier/"
	content_detail_url    = "https://api.duanshu.com/h5/content/detail/%s"
	column_detail_url     = "https://api.duanshu.com/h5/content/column/detail/%s"
	column_contents_url   = "https://api.duanshu.com/h5/content/column/contents"
	course_detail_url     = "https://api.duanshu.com/h5/content/course/detail/%s"
	course_chapters_url   = "https://api.duanshu.com/fairy/api/v1/courses/%s/chapters/"
	class_detail_url      = "https://api.duanshu.com/h5/content/course/class/detail"
	video_play_info_url   = "https://api.duanshu.com/fairy/api/v1/videos/play_info/"
	user_column_url       = "https://api.duanshu.com/h5/content/user/column"
	urlShopPattern        = "https://%s.duanshu.com"
	urlCupsjBriefTemplate = "https://cupsj.duanshu.com/#/brief/%s/%s"
)

var patterns = []string{`(?:[\w-]+\.)?duanshu\.com/`}

func init() {
	extractor.Register(&Duanshu{}, extractor.SiteInfo{Name: "Duanshu", URL: "duanshu.com", NeedAuth: true})
}

type Duanshu struct{}

func (d *Duanshu) Patterns() []string { return patterns }

var (
	briefRe  = regexp.MustCompile(`(?i)#/(?:brief/)?(video|audio|article|column|course|single)/(\w[\w-]*)`)
	singleRe = regexp.MustCompile(`(?i)#/single/(\w[\w-]*)`)
	classRe  = regexp.MustCompile(`(?i)#/(?:course|form/cou?rse)/class/(\w[\w-]*)/(\w[\w-]*)`)
	courseRe = regexp.MustCompile(`(?i)#/(?:course|form/cou?rse)/(\w[\w-]*)|/course/detail/(\w[\w-]*)`)
)

type duanshuURL struct {
	Shop      string
	Kind      string
	ContentID string
	ClassID   string
}

type contentItem struct {
	ID    string
	Title string
	Kind  string
	Class string
	Index int
	Test  bool
}

func (d *Duanshu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("duanshu requires login cookies")
	}
	info := parseDuanshuURL(rawURL)
	if info.ContentID == "" {
		return nil, fmt.Errorf("cannot parse duanshu content id from URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := duanshuHeaders(rawURL, opts.Cookies, info.Shop)
	_, _ = requestJSON(c, user_detail_url, headers)

	if info.ClassID != "" {
		entry, err := resolveClass(c, headers, info.ContentID, info.ClassID, "")
		if err != nil {
			return nil, err
		}
		return entry, nil
	}

	switch normalizeType(info.Kind) {
	case "column":
		return resolveColumn(c, headers, info.ContentID)
	case "course":
		return resolveCourse(c, headers, info.ContentID)
	default:
		return resolveSingle(c, headers, info.ContentID, info.Kind, "")
	}
}

func resolveSingle(c *util.Client, headers map[string]string, contentID, kind, fallback string) (*extractor.MediaInfo, error) {
	payload, err := requestJSON(c, fmt.Sprintf(content_detail_url, url.PathEscape(contentID)), headers)
	if err != nil {
		return nil, fmt.Errorf("fetch duanshu content detail: %w", err)
	}
	media := findMediaURL(payload)
	if media == "" {
		media = resolveVideoPlayInfo(c, headers, payload)
	}
	if media == "" {
		return nil, fmt.Errorf("duanshu: no media URL in %s %s", firstNonEmpty(kind, "single"), contentID)
	}
	title := util.SanitizeFilename(firstNonEmpty(pickTitle(payload), fallback, contentID))
	return mediaInfo(title, media, headers), nil
}

func resolveColumn(c *util.Client, headers map[string]string, contentID string) (*extractor.MediaInfo, error) {
	detail, err := requestJSON(c, fmt.Sprintf(column_detail_url, url.PathEscape(contentID)), headers)
	if err != nil {
		return nil, fmt.Errorf("fetch duanshu column detail: %w", err)
	}
	title := firstNonEmpty(pickTitle(detail), contentID)
	items, err := fetchColumnItems(c, headers, contentID)
	if err != nil {
		return nil, err
	}
	entries := make([]*extractor.MediaInfo, 0, len(items))
	for _, item := range items {
		entry, err := resolveSingle(c, headers, item.ID, item.Kind, item.Title)
		if err == nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		if media := findMediaURL(detail); media != "" {
			entries = append(entries, mediaInfo(title, media, headers))
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("duanshu: no playable entries in column %s", contentID)
	}
	return &extractor.MediaInfo{Site: "duanshu", Title: util.SanitizeFilename(title), Entries: entries}, nil
}

func resolveCourse(c *util.Client, headers map[string]string, contentID string) (*extractor.MediaInfo, error) {
	detail, err := requestJSON(c, fmt.Sprintf(course_detail_url, url.PathEscape(contentID)), headers)
	if err != nil {
		return nil, fmt.Errorf("fetch duanshu course detail: %w", err)
	}
	title := firstNonEmpty(pickTitle(detail), contentID)
	chapters, err := requestJSON(c, fmt.Sprintf(course_chapters_url, url.PathEscape(contentID)), headers)
	if err != nil {
		return nil, fmt.Errorf("fetch duanshu course chapters: %w", err)
	}
	classes := collectClassItems(chapters)
	entries := make([]*extractor.MediaInfo, 0, len(classes))
	for _, item := range classes {
		entry, err := resolveClass(c, headers, contentID, item.Class, item.Title)
		if err == nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		if media := findMediaURL(detail); media != "" {
			entries = append(entries, mediaInfo(title, media, headers))
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("duanshu: no playable classes in course %s", contentID)
	}
	return &extractor.MediaInfo{Site: "duanshu", Title: util.SanitizeFilename(title), Entries: entries}, nil
}

func resolveClass(c *util.Client, headers map[string]string, courseID, classID, fallback string) (*extractor.MediaInfo, error) {
	api := class_detail_url + "?course_id=" + url.QueryEscape(courseID) + "&class_id=" + url.QueryEscape(classID)
	payload, err := requestJSON(c, api, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch duanshu class detail: %w", err)
	}
	media := findMediaURL(payload)
	if media == "" {
		media = resolveVideoPlayInfo(c, headers, payload)
	}
	if media == "" {
		return nil, fmt.Errorf("duanshu: no media URL in class %s", classID)
	}
	title := util.SanitizeFilename(firstNonEmpty(pickTitle(payload), fallback, classID))
	return mediaInfo(title, media, headers), nil
}

func requestJSON(c *util.Client, api string, headers map[string]string) (any, error) {
	if headers["x-shop"] != "" && !strings.Contains(api, "shop_id=") {
		sep := "?"
		if strings.Contains(api, "?") {
			sep = "&"
		}
		api += sep + "shop_id=" + url.QueryEscape(headers["x-shop"])
	}
	body, err := c.GetString(api, headers)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func fetchColumnItems(c *util.Client, headers map[string]string, contentID string) ([]contentItem, error) {
	var out []contentItem
	for page := 1; page <= 50; page++ {
		api := column_contents_url + "?column_id=" + url.QueryEscape(contentID) + "&page=" + fmt.Sprint(page)
		payload, err := requestJSON(c, api, headers)
		if err != nil {
			return out, fmt.Errorf("fetch duanshu column contents: %w", err)
		}
		items := collectContentItems(payload)
		if len(items) == 0 {
			break
		}
		out = append(out, items...)
		if !hasNextPage(payload, page) {
			break
		}
	}
	return out, nil
}

func resolveVideoPlayInfo(c *util.Client, headers map[string]string, payload any) string {
	ids := collectStringsByKeys(payload, "video_id", "videoId", "id")
	for _, id := range ids {
		api := video_play_info_url + "?video_id=" + url.QueryEscape(id)
		if p, err := requestJSON(c, api, headers); err == nil {
			if media := findMediaURL(p); media != "" {
				return media
			}
		}
	}
	return ""
}

func parseDuanshuURL(raw string) duanshuURL {
	out := duanshuURL{Kind: "single"}
	if u, err := url.Parse(raw); err == nil && strings.Contains(u.Host, ".duanshu.com") {
		out.Shop = strings.TrimSuffix(u.Host, ".duanshu.com")
	}
	if m := classRe.FindStringSubmatch(raw); m != nil {
		out.Kind, out.ContentID, out.ClassID = "course", m[1], m[2]
		return out
	}
	if m := briefRe.FindStringSubmatch(raw); m != nil {
		out.Kind, out.ContentID = m[1], m[2]
		return out
	}
	if m := singleRe.FindStringSubmatch(raw); m != nil {
		out.Kind, out.ContentID = "single", m[1]
		return out
	}
	if m := courseRe.FindStringSubmatch(raw); m != nil {
		out.Kind, out.ContentID = "course", firstNonEmpty(m[1], m[2])
		return out
	}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		out.Kind = firstNonEmpty(q.Get("type"), q.Get("content_type"), out.Kind)
		out.ContentID = firstNonEmpty(q.Get("content_id"), q.Get("contentId"), q.Get("id"))
		out.ClassID = firstNonEmpty(q.Get("class_id"), q.Get("classId"))
	}
	return out
}

func duanshuHeaders(raw string, jar http.CookieJar, shop string) map[string]string {
	shopDomain := firstNonEmpty(shop, "h5") + ".duanshu.com"
	headers := map[string]string{
		"Accept":  "application/json, text/plain, */*",
		"referer": "https://" + shopDomain,
		"Referer": "https://" + shopDomain,
	}
	for _, host := range []string{shopDomain, "h5.duanshu.com", "api.duanshu.com"} {
		u := &url.URL{Scheme: "https", Host: host, Path: "/"}
		for _, cookie := range jar.Cookies(u) {
			switch strings.ToLower(cookie.Name) {
			case "x-member":
				headers["x-member"] = cookie.Value
			case "x-shop":
				headers["x-shop"] = cookie.Value
			}
		}
	}
	return headers
}
