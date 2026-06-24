// Package plaso implements an extractor for plaso.cn courses.
package plaso

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	referer           = "https://www.plaso.cn"
	course_url        = "https://www.plaso.cn/gt/servlet/group/getGroupsByActive"
	course_list_url   = "https://www.plaso.cn/course/api/v1/m/package/student/list"
	package_list_url  = "https://www.plaso.cn/course/api/v1/m/package/list"
	history_list_url  = "https://www.plaso.cn/liveclassgo/api/v1/history/listRecord"
	homework_list_url = "https://www.plaso.cn/homework/student/studentHomeworks"
	share_url         = "https://www.plaso.cn/sc/nc/newGetShareInfo"
	file_url          = "https://www.plaso.cn/yxt/servlet/file/preview/getfileinfo"
	file_info_url     = "https://www.plaso.cn/yxt/servlet/file/getfileinfo"
	info_url          = "https://www.plaso.cn/cs/xfilegroup/getXFileGroupInfo"
	package_url       = "https://www.plaso.cn/course/api/v1/nct/m/package/task/list"
	dir_info_url      = "https://www.plaso.cn/yxt/servlet/bigDir/getXfgTask"
	m3u8_url          = "https://www.plaso.cn/yxt/servlet/ali/getPlayInfo"
	poly_sign_url     = "https://www.plaso.cn/yxt/servlet/file/preview/getPolyvVidInfoV2"
	m3u8_sign_url     = "https://www.plaso.cn/yxt/servlet/org/nc/polyvViewSign"
	poly_video_url    = "https://api.polyv.net/v2/video/5153980715/get-video-info"
	sts_url           = "https://www.plaso.cn/yxt/servlet/stsHelper/stsInfo"
	sts_preview_url   = "https://www.plaso.cn/yxt/servlet/stsHelper/preview/stsInfo"
)

var patterns = []string{
	`(?:[\w-]+\.)?plaso\.cn/`,
	`(?:[\w-]+\.)?aiwenyun\.cn/`,
}

func init() {
	extractor.Register(&Plaso{}, extractor.SiteInfo{Name: "Plaso", URL: "plaso.cn", NeedAuth: true})
}

type Plaso struct{}

func (s *Plaso) Patterns() []string { return patterns }

type fileItem struct{ ID, MyID, Location, LocationPath, Name, Type, URL, Vid string }
type courseInfo struct{ ID, Title string }

var (
	sfidRe  = regexp.MustCompile(`[?&](?:sfId|sfid|fileId|fid)=([\w-]+)`)
	mediaRe = regexp.MustCompile(`https?://[^"'\s]+\.(?:m3u8|mp4|mp3)(?:\?[^"'\s]*)?`)
)

func (s *Plaso) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("plaso requires login cookies")
	}
	if isBlockedPlasoVariant(rawURL) {
		return nil, fmt.Errorf("plaso variant blocked: needs host-specific API mapping for Aiwenyun/Jhpy source")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := headers()
	cid := parseCID(rawURL)
	title := "plaso_" + first(cid, "course")
	var files []fileItem
	if cid != "" {
		if sharedItem, _ := fetchShareOrFile(c, h, cid); sharedItem.ID != "" || sharedItem.Location != "" || sharedItem.Vid != "" {
			files = append(files, sharedItem)
			title = first(sharedItem.Name, title)
		}
	}
	if len(files) == 0 {
		courses := fetchCourseList(c, h)
		if cid == "" && len(courses) > 0 {
			cid = courses[0].ID
		}
		for _, co := range courses {
			if co.ID == cid {
				title = first(co.Title, title)
				break
			}
		}
		files = append(files, fetchPackageFiles(c, h, cid)...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("plaso: no file/task records found from share/package APIs")
	}
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	blockedLocalLocation := false
	for i, f := range files {
		if (f.Location != "" || f.LocationPath != "") && f.ID == "" && f.URL == "" && f.Vid == "" {
			blockedLocalLocation = true
		}
		mi := resolveFile(c, h, f, i+1)
		if mi == nil {
			continue
		}
		u := firstStreamURL(mi)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		entries = append(entries, mi)
	}
	if len(entries) == 0 {
		if blockedLocalLocation {
			return nil, fmt.Errorf("plaso local location playback blocked: needs STS/plist sandbox flow from Plaso_Local source")
		}
		return nil, fmt.Errorf("plaso: no playable m3u8/mp4/file URLs resolved from file records")
	}
	return &extractor.MediaInfo{Site: "plaso", Title: clean(title), Entries: entries, Extra: map[string]any{"course_id": cid}}, nil
}

func fetchCourseList(c *util.Client, h map[string]string) []courseInfo {
	var out []courseInfo
	apis := []struct {
		URL  string
		Data map[string]string
	}{
		{course_url, map[string]string{}},
		{course_list_url, map[string]string{"pageSize": "200", "pageNum": "1"}},
		{package_list_url, map[string]string{"pageSize": "200", "pageNum": "1", "search": ""}},
		{history_list_url, map[string]string{"pageSize": "200", "indexStart": "0"}},
	}
	seen := map[string]bool{}
	for _, api := range apis {
		v, err := postJSON(c, api.URL, api.Data, h)
		if err != nil {
			continue
		}
		walk(v, func(m map[string]any) {
			id := firstText(m, "id", "originId", "packageId", "fileGroupId", "fileId")
			title := firstText(m, "title", "groupName", "name", "packageName")
			if id != "" && title != "" && !seen[id] {
				seen[id] = true
				out = append(out, courseInfo{ID: id, Title: title})
			}
		})
	}
	return out
}

func fetchPackageFiles(c *util.Client, h map[string]string, cid string) []fileItem {
	if cid == "" {
		return nil
	}
	var out []fileItem
	for _, req := range []struct {
		u string
		d map[string]string
	}{
		{package_url, map[string]string{"packageId": cid, "id": cid, "fileGroupId": cid}},
		{info_url, map[string]string{"fileGroupId": cid, "id": cid}},
		{dir_info_url, map[string]string{"fileGroupId": cid, "id": cid, "hiddenTask": "false", "sourceWay": "course"}},
	} {
		v, err := postJSON(c, req.u, req.d, h)
		if err != nil {
			continue
		}
		walk(v, func(m map[string]any) {
			if item := buildFileItem(m); item.ID != "" || item.Location != "" || item.URL != "" || item.Vid != "" {
				out = append(out, item)
			}
		})
		if len(out) > 0 {
			break
		}
	}
	return dedupe(out)
}

func fetchShareOrFile(c *util.Client, h map[string]string, id string) (fileItem, error) {
	for _, req := range []struct {
		u string
		d map[string]string
	}{
		{share_url, map[string]string{"sfId": id, "shareKey": id}},
		{file_url, map[string]string{"fileId": id, "id": id}},
		{file_info_url, map[string]string{"fileId": id, "id": id}},
	} {
		v, err := postJSON(c, req.u, req.d, h)
		if err != nil {
			continue
		}
		best := fileItem{}
		walk(v, func(m map[string]any) {
			if best.ID == "" && (firstText(m, "fileId", "id", "_id", "originId", "location", "vid") != "") {
				best = buildFileItem(m)
			}
		})
		if best.ID != "" || best.Location != "" || best.Vid != "" {
			return best, nil
		}
	}
	return fileItem{}, fmt.Errorf("plaso share/file id not found")
}

func resolveFile(c *util.Client, h map[string]string, f fileItem, idx int) *extractor.MediaInfo {
	name := clean(first(f.Name, fmt.Sprintf("plaso_%d", idx)))
	candidates := []string{f.URL}
	if f.ID != "" {
		candidates = append(candidates, fetchAliPlayURL(c, h, f.ID), fetchPolyvURL(c, h, f.ID, f.Vid))
	}
	if f.Location != "" || f.LocationPath != "" {
		if f.ID == "" && f.URL == "" && f.Vid == "" {
			return nil
		}
	}
	for _, u := range candidates {
		u = normalizeURL(u)
		if u == "" {
			continue
		}
		if strings.HasPrefix(u, "http") && (looksMedia(u) || f.Type != "") {
			return &extractor.MediaInfo{Site: "plaso", Title: name, Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{u}, Format: formatOf(u), Headers: h}}, Extra: map[string]any{"file_id": f.ID, "my_id": f.MyID, "location": f.Location, "file_type": f.Type}}
		}
	}
	return nil
}

func fetchAliPlayURL(c *util.Client, h map[string]string, fileID string) string {
	v, err := postJSON(c, m3u8_url, map[string]string{"fileId": fileID, "id": fileID}, h)
	if err != nil {
		return ""
	}
	return first(findFirst(v, "hdPlayUrl"), findFirst(v, "sdPlayUrl"), findFirst(v, "ldPlayUrl"), findFirst(v, "playUrl"), findFirst(v, "m3u8Url"))
}

func fetchPolyvURL(c *util.Client, h map[string]string, fileID, vid string) string {
	if vid == "" {
		v, _ := postJSON(c, poly_sign_url, map[string]string{"fileId": fileID, "id": fileID}, h)
		vid = findFirst(v, "vid")
	}
	if vid != "" {
		if sec, err := shared.PolyvResolveSecure(c, vid, h); err == nil {
			if u, e := shared.PolyvPickBestManifest(sec); e == nil {
				return u
			}
		}
	}
	body, err := c.PostForm(poly_video_url, map[string]string{"vid": vid}, h)
	if err != nil {
		return ""
	}
	if m := mediaRe.FindString(body); m != "" {
		return strings.ReplaceAll(m, `\/`, `/`)
	}
	v, _ := postJSON(c, m3u8_sign_url, map[string]string{"fileId": fileID, "vid": vid}, h)
	return findFirst(v, "playUrl", "m3u8Url", "url")
}

func buildFileItem(m map[string]any) fileItem {
	return fileItem{ID: firstText(m, "fileId", "id", "originId", "_id"), MyID: firstText(m, "myid", "myId", "my_id"), Location: firstText(m, "location"), LocationPath: firstText(m, "locationPath", "location_path"), Name: firstText(m, "name", "title", "file_name"), Type: strings.ToLower(firstText(m, "type", "file_type")), URL: firstText(m, "url", "file_url", "downloadUrl", "playUrl", "m3u8Url"), Vid: firstText(m, "vid")}
}

func postJSON(c *util.Client, api string, data map[string]string, h map[string]string) (any, error) {
	body, err := c.PostForm(api, data, h)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func parseCID(rawURL string) string {
	if m := sfidRe.FindStringSubmatch(rawURL); m != nil {
		return m[1]
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return first(u.Query().Get("sfId"), u.Query().Get("sfid"), u.Query().Get("fileId"), u.Query().Get("fid"), u.Query().Get("id"), u.Query().Get("packageId"))
}

func headers() map[string]string {
	return map[string]string{"Referer": referer, "Origin": referer, "Accept": "application/json, text/plain, */*"}
}
func isBlockedPlasoVariant(rawURL string) bool {
	l := strings.ToLower(rawURL)
	return strings.Contains(l, "aiwenyun.cn/") || strings.Contains(l, "jhpy.plaso.cn/")
}
func walk(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for _, x := range t {
			walk(x, fn)
		}
	case []any:
		for _, x := range t {
			walk(x, fn)
		}
	}
}
func firstText(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
func findFirst(v any, keys ...string) string {
	out := ""
	walk(v, func(m map[string]any) {
		if out == "" {
			out = firstText(m, keys...)
		}
	})
	return out
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func clean(s string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s), " .")
}
func normalizeURL(u string) string {
	u = strings.TrimSpace(strings.ReplaceAll(u, `\/`, `/`))
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}
func looksMedia(u string) bool {
	l := strings.ToLower(u)
	return strings.Contains(l, ".m3u8") || strings.Contains(l, ".mp4") || strings.Contains(l, ".mp3")
}
func formatOf(u string) string {
	l := strings.ToLower(u)
	if strings.Contains(l, ".m3u8") {
		return "m3u8"
	}
	if strings.Contains(l, ".mp3") {
		return "mp3"
	}
	return "mp4"
}
func firstStreamURL(mi *extractor.MediaInfo) string {
	if mi == nil {
		return ""
	}
	if st, ok := mi.Streams["best"]; ok && len(st.URLs) > 0 {
		return st.URLs[0]
	}
	return ""
}
func dedupe(in []fileItem) []fileItem {
	seen := map[string]bool{}
	out := in[:0]
	for _, f := range in {
		k := f.ID + "|" + f.Location + "|" + f.URL + "|" + f.Vid
		if k == "|||" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, f)
	}
	return out
}
