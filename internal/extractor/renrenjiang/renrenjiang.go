// Package renrenjiang implements an extractor for renrenjiang.cn (人人讲) courses.
package renrenjiang

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	API_HOST                  = "https://api.renrenjiang.cn"
	REFERER                   = "https://ke.renrenjiang.cn/"
	ORIGIN                    = "https://ke.renrenjiang.cn"
	QCLOUD_APP_ID             = "1255652068"
	QCLOUD_LICENSE_URL        = "1400817455"
	QCLOUD_PLAY_API           = "https://playvideo.qcloud.com/getplayinfo/v4/%s/%s"
	columns_subscribed_api    = "/api/v2/columns/subscribed"
	activities_subscribed_api = "/api/v3/activities/subscribed"
	column_detail_api         = "/api/v2/columns/%s"
	column_subscribed_api     = "/api/v2/columns/%s/is_subscribed"
	column_activities_api     = "/api/v2/columns/%s/activities3"
	activity_detail_api       = "/api/v2/activities/%s"
	activity_pay_api          = "/api/v3/activities/%s/is_pay"
	activity_stream_api       = "/api/v3/activities/%s/stream/info"
	activity_stream_url_api   = "/api/v3/activities/%s/stream_url"
	activity_reservation_api  = "/api/v3/activities/%s/reservation"
	activity_docs_api         = "/api/v3/document/activity/list"
	column_docs_api           = "/api/v3/document/column/list"
)

var patterns = []string{`(?:[\w-]+\.)?renrenjiang\.cn/`}

func init() {
	extractor.Register(&Renrenjiang{}, extractor.SiteInfo{Name: "Renrenjiang", URL: "renrenjiang.cn", NeedAuth: true})
}

type Renrenjiang struct{}

func (r *Renrenjiang) Patterns() []string { return patterns }

func (r *Renrenjiang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("renrenjiang requires login cookies")
	}
	cid, courseType := parseCourseID(rawURL)
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	auth := authFromJar(opts.Cookies)
	h := headers(auth.Token)
	if cid == "" {
		courses, _ := getCourseList(c, h, auth.UserID)
		if len(courses) > 0 {
			cid, courseType = courses[0].ID, courses[0].Type
		}
	}
	if cid == "" || courseType == "" {
		return nil, fmt.Errorf("cannot parse renrenjiang id/type from URL")
	}
	if courseType == "activity" {
		detail, _ := requestJSON(c, "GET", fmt.Sprintf(activity_detail_api, cid), map[string]string{"u": auth.UserID}, nil, h)
		entry, err := resolveActivity(c, h, auth.UserID, cid, first(textAt(unwrapMap(detail), "title", "name"), "renrenjiang_"+cid))
		if err != nil {
			return nil, err
		}
		entry.Extra = mergeExtra(entry.Extra, map[string]any{"activity_detail": detail, "documents": getDocuments(c, h, auth.UserID, cid, false)})
		return entry, nil
	}
	detail, _ := requestJSON(c, "GET", fmt.Sprintf(column_detail_api, cid), map[string]string{"u": auth.UserID}, nil, h)
	title := first(textAt(unwrapMap(detail), "title", "name"), "renrenjiang_"+cid)
	lessons, err := getColumnActivities(c, h, auth.UserID, cid)
	if err != nil {
		return nil, fmt.Errorf("renrenjiang column activities: %w", err)
	}
	var entries []*extractor.MediaInfo
	for i, lesson := range lessons {
		id := textAt(lesson, "id", "activity_id")
		if id == "" {
			continue
		}
		name := sanitize(fmt.Sprintf("[%d]--%s", i+1, first(textAt(lesson, "title", "name"), id)))
		entry, err := resolveActivity(c, h, auth.UserID, id, name)
		if err == nil && entry != nil {
			entry.Extra = mergeExtra(entry.Extra, map[string]any{"activity": lesson, "documents": getDocuments(c, h, auth.UserID, id, false)})
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("renrenjiang: no playable activity streams found")
	}
	return &extractor.MediaInfo{Site: "renrenjiang", Title: sanitize(title), Entries: entries, Extra: map[string]any{"course_id": cid, "course_type": courseType, "documents": getDocuments(c, h, auth.UserID, cid, true)}}, nil
}

type authInfo struct{ Token, UserID string }
type courseRef struct{ ID, Type, Title string }
type playInfo struct {
	URL  string
	Size int64
}

func getCourseList(c *util.Client, h map[string]string, userID string) ([]courseRef, error) {
	var out []courseRef
	cols, _ := getPagedItems(c, h, columns_subscribed_api, "columns", userID, 100, nil)
	for _, it := range cols {
		if id := textAt(it, "id", "column_id"); id != "" {
			out = append(out, courseRef{ID: id, Type: "column", Title: textAt(it, "title", "name")})
		}
	}
	acts, _ := getPagedItems(c, h, activities_subscribed_api, "activities", userID, 100, nil)
	for _, it := range acts {
		if id := textAt(it, "id", "activity_id"); id != "" {
			out = append(out, courseRef{ID: id, Type: "activity", Title: textAt(it, "title", "name")})
		}
	}
	return out, nil
}
func getPagedItems(c *util.Client, h map[string]string, api, itemKey, userID string, pageSize int, extra map[string]string) ([]map[string]any, error) {
	var out []map[string]any
	for page := 1; page <= 50; page++ {
		params := map[string]string{"page": fmt.Sprint(page), "pageSize": fmt.Sprint(pageSize), "total": "yes", "u": userID}
		for k, v := range extra {
			params[k] = v
		}
		resp, err := requestJSON(c, "GET", api, params, nil, h)
		if err != nil {
			return out, err
		}
		items := extractItems(resp, itemKey, "list")
		if len(items) == 0 {
			break
		}
		out = append(out, items...)
		if len(items) < pageSize {
			break
		}
	}
	return out, nil
}
func getColumnActivities(c *util.Client, h map[string]string, userID, cid string) ([]map[string]any, error) {
	var out []map[string]any
	for page := 1; page <= 50; page++ {
		resp, err := requestJSON(c, "GET", fmt.Sprintf(column_activities_api, cid), map[string]string{"u": userID, "page": fmt.Sprint(page), "pageSize": "100"}, nil, h)
		if err != nil {
			return out, err
		}
		items := extractItems(resp, "list", "activities")
		if len(items) == 0 {
			break
		}
		out = append(out, items...)
		if len(items) < 100 {
			break
		}
	}
	return out, nil
}
func resolveActivity(c *util.Client, h map[string]string, userID, activityID, title string) (*extractor.MediaInfo, error) {
	play := resolveActivityStream(c, h, userID, activityID)
	if play.URL == "" {
		_, _ = requestJSON(c, "GET", fmt.Sprintf(activity_pay_api, activityID), map[string]string{"u": userID}, nil, h)
		_, _ = requestJSON(c, "POST", fmt.Sprintf(activity_reservation_api, activityID), map[string]string{"u": userID}, map[string]string{}, h)
		play = resolveActivityStream(c, h, userID, activityID)
	}
	if play.URL == "" {
		return nil, fmt.Errorf("renrenjiang: empty stream for activity %s", activityID)
	}
	return &extractor.MediaInfo{Site: "renrenjiang", Title: sanitize(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{play.URL}, Format: pickFormat(play.URL), Size: play.Size, Headers: map[string]string{"Referer": REFERER, "Origin": ORIGIN}}}, Extra: map[string]any{"activity_id": activityID}}, nil
}
func resolveActivityStream(c *util.Client, h map[string]string, userID, id string) playInfo {
	for _, api := range []string{fmt.Sprintf(activity_stream_api, id), fmt.Sprintf(activity_stream_url_api, id)} {
		resp, err := requestJSON(c, "GET", api, map[string]string{"u": userID, "leak": "0"}, nil, h)
		if err != nil {
			continue
		}
		if p := extractStream(resp, c, h); p.URL != "" {
			return p
		}
	}
	return playInfo{}
}
func extractStream(v any, c *util.Client, h map[string]string) playInfo {
	m := unwrapMap(v)
	for _, k := range []string{"hls_url", "stream_url", "rtmp_url", "play_url", "playUrl", "url"} {
		if u := textAt(m, k); strings.HasPrefix(u, "http") {
			return playInfo{URL: u}
		}
	}
	if p := findURLInAny(m); p != "" {
		return playInfo{URL: p}
	}
	fileID := first(textAt(m, "file_id", "fileId"), findTextInAny(m, "file_id"), findTextInAny(m, "fileId"))
	psign := first(textAt(m, "psign", "pSign"), findTextInAny(m, "psign"), findTextInAny(m, "pSign"))
	if fileID != "" && psign != "" {
		return getQCloudPlayURL(c, h, fileID, psign)
	}
	return playInfo{}
}
func getQCloudPlayURL(c *util.Client, h map[string]string, fileID, psign string) playInfo {
	resp, err := requestJSON(c, "GET", fmt.Sprintf(QCLOUD_PLAY_API, QCLOUD_APP_ID, fileID), map[string]string{"psign": psign}, nil, h)
	if err != nil {
		return playInfo{}
	}
	return pickQCloudURL(resp)
}
func pickQCloudURL(v any) playInfo {
	var urls []playInfo
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			if u := textAt(t, "url", "playUrl", "hls_url", "stream_url"); strings.HasPrefix(u, "http") {
				urls = append(urls, playInfo{URL: u, Size: int64(numAt(t, "size"))})
			}
			for _, v := range t {
				walk(v)
			}
		case []any:
			for _, v := range t {
				walk(v)
			}
		}
	}
	walk(v)
	if len(urls) == 0 {
		return playInfo{}
	}
	sort.SliceStable(urls, func(i, j int) bool { return urls[i].Size > urls[j].Size })
	return urls[0]
}
func getDocuments(c *util.Client, h map[string]string, userID, id string, column bool) []map[string]any {
	api, params := activity_docs_api, map[string]string{"product_id": id, "u": userID}
	if column {
		api, params = column_docs_api, map[string]string{"product_id": id, "u": userID, "page": "1", "pageSize": "100"}
	}
	resp, err := requestJSON(c, "GET", api, params, nil, h)
	if err != nil {
		return nil
	}
	return extractItems(resp, "list", "documents", "data")
}
func requestJSON(c *util.Client, method, path string, params, data, h map[string]string) (any, error) {
	u := path
	if !strings.HasPrefix(u, "http") {
		u = strings.TrimRight(API_HOST, "/") + "/" + strings.TrimLeft(path, "/")
	}
	uu, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	q := uu.Query()
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	uu.RawQuery = q.Encode()
	var body string
	if strings.EqualFold(method, "POST") {
		body, err = c.PostForm(uu.String(), data, h)
	} else {
		body, err = c.GetString(uu.String(), h)
	}
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}
