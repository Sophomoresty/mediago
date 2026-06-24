// Package qihang implements an extractor for iqihang.com courses.
package qihang

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	referer        = "https://www.iqihang.com"
	course_url     = "https://www.iqihang.com/api/ark/web/v1/user/course/course-list?isMarketingCourse=&status=&type=1"
	info_url       = "https://www.iqihang.com/api/ark/web/v1/course/catalog/%s"
	video_play_url = "https://p.bokecc.com/servlet/getvideofile?vid=%s&siteid=A183AC83A2983CCC"
	live_url       = "https://www.iqihang.com/api/ark/web/v1/user/course/live/replay?liveId=%s"
	live_login_url = "https://view.csslcloud.net/api/room/replay/login?roomid=%s&userid=%s&recordid=%s&viewertoken=%s%%3A%s"
	live_play_url  = "https://view.csslcloud.net/api/record/vod?accountId=%s&recordId=%s&terminal=3&token=%s"
	source_url     = "https://www.iqihang.com/api/ark/web/v1/lecture/curriculum/node?curriculumId=%s"
	price_url      = "https://iqihang.com/api/ark/web/v1/product/%s"
	user_info_url  = "https://www.iqihang.com/api/ark/web/v1/user/info"
	bokeccSiteID   = "A183AC83A2983CCC"
)

var patterns = []string{`(?:[\w-]+\.)?iqihang\.com/`}

func init() {
	extractor.Register(&Qihang{}, extractor.SiteInfo{Name: "Qihang", URL: "iqihang.com", NeedAuth: true})
}

type Qihang struct{}

func (s *Qihang) Patterns() []string { return patterns }

func (s *Qihang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("qihang requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := qihangHeaders(opts.Cookies)
	uid, _ := fetchUID(c, h)
	cid, learnID, productID := parseIDs(rawURL)

	courses, _ := fetchCourseList(c, h)
	if cid == "" {
		for _, it := range courses {
			if (productID != "" && it.ProductID == productID) || (learnID != "" && it.LearnID == learnID) {
				cid, productID, learnID = it.CourseID, it.ProductID, it.LearnID
				break
			}
		}
	}
	if cid == "" {
		return nil, fmt.Errorf("qihang: cannot map URL to productCurriculumId")
	}
	title := titleFromCourse(c, h, productID, courses, cid)
	if title == "" {
		title = "qihang_" + cid
	}

	nodes, err := fetchNodes(c, fmt.Sprintf(info_url, cid), true, h)
	if err != nil {
		return nil, fmt.Errorf("qihang course/catalog: %w", err)
	}
	sourceNodes, _ := fetchNodes(c, fmt.Sprintf(source_url, cid), false, h)
	nodes = append(nodes, sourceNodes...)

	seen := map[string]bool{}
	var entries []*extractor.MediaInfo
	collectEntries(c, h, nodes, nil, uid, learnID, seen, &entries)
	if len(entries) == 0 {
		return nil, fmt.Errorf("qihang: no playable videos found from courseNodes/resourceList")
	}
	return &extractor.MediaInfo{Site: "qihang", Title: title, Entries: entries}, nil
}

type qCourse struct{ LearnID, ProductID, CourseID, Title string }

func fetchCourseList(c *util.Client, h map[string]string) ([]qCourse, error) {
	body, err := c.GetString(course_url, h)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []struct {
			ID                  any    `json:"id"`
			ProductID           any    `json:"productId"`
			ProductCurriculumID any    `json:"productCurriculumId"`
			ProductName         string `json:"productName"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	out := make([]qCourse, 0, len(resp.Data))
	for _, it := range resp.Data {
		out = append(out, qCourse{LearnID: jstr(it.ID), ProductID: jstr(it.ProductID), CourseID: jstr(it.ProductCurriculumID), Title: it.ProductName})
	}
	return out, nil
}

func titleFromCourse(c *util.Client, h map[string]string, productID string, courses []qCourse, cid string) string {
	if productID != "" {
		body, err := c.GetString(fmt.Sprintf(price_url, productID), h)
		if err == nil {
			var resp struct {
				Data struct {
					Name      string `json:"name"`
					SellPrice any    `json:"sellPrice"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(body), &resp) == nil && strings.TrimSpace(resp.Data.Name) != "" {
				return sanitize(resp.Data.Name)
			}
		}
	}
	for _, it := range courses {
		if it.CourseID == cid && it.Title != "" {
			return sanitize(it.Title)
		}
	}
	return ""
}

type qNode struct {
	Name              string      `json:"name"`
	Children          []qNode     `json:"children"`
	StudyResourceType int         `json:"studyResourceType"`
	ResourceList      []qResource `json:"resourceList"`
}
type qResource struct {
	Vid        any    `json:"vid"`
	ResourceID any    `json:"resourceId"`
	LectureURL string `json:"lectureUrl"`
}

func fetchNodes(c *util.Client, api string, catalog bool, h map[string]string) ([]qNode, error) {
	body, err := c.GetString(api, h)
	if err != nil {
		return nil, err
	}
	if catalog {
		var resp struct {
			Data struct {
				CourseNodes []qNode `json:"courseNodes"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			return nil, err
		}
		return resp.Data.CourseNodes, nil
	}
	var resp struct {
		Data []qNode `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func collectEntries(c *util.Client, h map[string]string, nodes []qNode, prefix []int, uid, learnID string, seen map[string]bool, entries *[]*extractor.MediaInfo) {
	for i, n := range nodes {
		idx := append(append([]int{}, prefix...), i+1)
		if len(n.Children) > 0 {
			collectEntries(c, h, n.Children, idx, uid, learnID, seen, entries)
		}
		if n.StudyResourceType != 2 && n.StudyResourceType != 3 || len(n.ResourceList) == 0 {
			continue
		}
		r := n.ResourceList[0]
		vid, liveID := jstr(r.Vid), jstr(r.ResourceID)
		key := vid + ":" + liveID + ":" + n.Name
		if key == "::" || seen[key] {
			continue
		}
		seen[key] = true
		mi := resolveVideo(c, h, idx, n.Name, vid, liveID, uid, learnID)
		if mi != nil {
			*entries = append(*entries, mi)
		}
	}
}

func resolveVideo(c *util.Client, h map[string]string, idx []int, name, vid, liveID, uid, learnID string) *extractor.MediaInfo {
	title := sanitize(fmt.Sprintf("[%s]-%s", joinInts(idx), strings.TrimSuffix(name, ".mp4")))
	if vid != "" {
		if u, err := shared.BokeCCResolve(c, vid, bokeccSiteID, h); err == nil && u != "" {
			return media(title, u, map[string]any{"video_id": vid})
		}
	}
	if liveID != "" {
		if u, audio, err := resolveLive(c, h, liveID, uid, learnID); err == nil && u != "" {
			m := media(title, u, map[string]any{"live_id": liveID})
			if audio != "" {
				st := m.Streams["best"]
				st.AudioURL = audio
				m.Streams["best"] = st
			}
			return m
		}
	}
	return nil
}

func resolveLive(c *util.Client, h map[string]string, liveID, uid, learnID string) (string, string, error) {
	body, err := c.GetString(fmt.Sprintf(live_url, liveID), h)
	if err != nil {
		return "", "", err
	}
	var resp struct {
		Data struct {
			ReplayURL string `json:"replayUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return "", "", err
	}
	roomID, userID, recordID := replayArgs(resp.Data.ReplayURL)
	if roomID == "" || userID == "" || recordID == "" {
		return "", "", fmt.Errorf("qihang live: replayUrl lacks roomid/userid/recordid")
	}
	pi, err := shared.CssLcloudResolvePlayInfo(c, shared.CssLcloudPayload{
		LiveRoomID: roomID, UserID: userID, AccessID: userID, RecordID: recordID,
		ViewerToken: uid + ":" + learnID, Referer: referer,
	})
	if err != nil {
		return "", "", err
	}
	return pi.VideoURL, pi.AudioURL, nil
}

func media(title, u string, extra map[string]any) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "qihang", Title: title, Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{u}, Format: pickFormat(u), Headers: map[string]string{"Referer": referer}}}, Extra: extra}
}

func qihangHeaders(j http.CookieJar) map[string]string {
	h := map[string]string{"Referer": referer}
	if token := cookieVal(j, []string{"https://www.iqihang.com/", "https://iqihang.com/"}, "accessToken"); token != "" {
		h["Authorization"] = "Bearer " + token
	}
	return h
}
func fetchUID(c *util.Client, h map[string]string) (string, error) {
	if h["Authorization"] == "" {
		return "", nil
	}
	body, err := c.GetString(user_info_url, h)
	if err != nil {
		return "", err
	}
	var resp struct {
		Data struct {
			ID any `json:"id"`
		} `json:"data"`
		ID any `json:"id"`
	}
	if json.Unmarshal([]byte(body), &resp) == nil {
		if id := first(jstr(resp.Data.ID), jstr(resp.ID)); id != "" {
			return id, nil
		}
	}
	if m := regexp.MustCompile(`"code"\s*:\s*0[\s\S]*?"id"\s*:\s*(\d+)`).FindStringSubmatch(body); len(m) > 1 {
		return m[1], nil
	}
	return "", nil
}

var (
	learnRe    = regexp.MustCompile(`/learn/(\d+)`)
	recordRe   = regexp.MustCompile(`/record/\d+/\d+/(\d+)`)
	playbackRe = regexp.MustCompile(`/playback/\d+/.*?/\d+/(\d+)`)
	productRe  = regexp.MustCompile(`[?&]courseId=(\d+)`)
)

func parseIDs(raw string) (cid, learnID, productID string) {
	if m := learnRe.FindStringSubmatch(raw); len(m) > 1 {
		learnID = m[1]
	}
	if m := recordRe.FindStringSubmatch(raw); len(m) > 1 {
		learnID = m[1]
	}
	if m := playbackRe.FindStringSubmatch(raw); len(m) > 1 {
		learnID = m[1]
	}
	if m := productRe.FindStringSubmatch(raw); len(m) > 1 {
		productID = m[1]
	}
	return "", learnID, productID
}
func replayArgs(raw string) (roomID, userID, recordID string) {
	u, err := url.Parse(strings.ReplaceAll(raw, "&amp;", "&"))
	if err == nil {
		q := u.Query()
		roomID, userID, recordID = q.Get("roomid"), q.Get("userid"), q.Get("recordid")
	}
	if roomID == "" {
		roomID = match1(raw, `roomid=(\w+)`)
	}
	if userID == "" {
		userID = match1(raw, `userid=(\w+)`)
	}
	if recordID == "" {
		recordID = match1(raw, `recordid=(\w+)`)
	}
	return
}
func cookieVal(j http.CookieJar, hosts []string, names ...string) string {
	if j == nil {
		return ""
	}
	for _, host := range hosts {
		u, _ := url.Parse(host)
		for _, ck := range j.Cookies(u) {
			for _, n := range names {
				if strings.EqualFold(ck.Name, n) {
					return strings.TrimSpace(ck.Value)
				}
			}
		}
	}
	return ""
}
func jstr(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && v != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func match1(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}
func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ".")
}

var badName = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)

func sanitize(s string) string {
	s = badName.ReplaceAllString(strings.TrimSpace(s), "_")
	if s == "" {
		return "未命名视频"
	}
	return s
}
func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
