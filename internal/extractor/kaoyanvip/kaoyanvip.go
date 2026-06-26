// Package kaoyanvip implements an extractor for kaoyanvip.cn (考研VIP/研途) courses.
//
// API chain ported from decompiled Mooc/Courses/Kaoyanvip/Kaoyanvip_Course.pyc:
//
//	https://ytky.kaoyanvip.cn/api/v1/account/auth/user/info
//	https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse
//	https://api.kaoyanvip.cn/learn/v1/delivery/pc/my_delivery/info/?my_delivery_id={cid}
//	https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse/{cid}
//	https://api.kaoyanvip.cn/learn/v1/delivery/my_unified_outline/structure/?my_delivery_id={cid}&delivery_outline_id={outline_id}
//	https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse/{cid}/{outline_id}/no_stage/
//	https://ytky.kaoyanvip.cn/api/v1/living/{room_id}/records
//	https://hls.videocc.net/{path1}/{path2}/{vid}.m3u8
//	https://dpv.videocc.net/{path1}/{path2}/{vid}_{quality}.mp4
//	https://api.polyv.net/live/inner/v3/channel/playback/get-video-by-vid?vid={vid}&timestamp={timestamp}&channelId={channel_id}&sign={sign}&appId={app_id}
//	https://ytky.kaoyanvip.cn/api/v1/course/pc/video/play/token/?user_id={user_id}&vid={vid}
package kaoyanvip

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	urlReferer        = "https://www.kaoyanvip.cn"
	urlUserInfo       = "https://ytky.kaoyanvip.cn/api/v1/account/auth/user/info"
	urlOrder          = "https://ytky.kaoyanvip.cn/api/v1/course/myorder?page=1&size=99"
	urlCourse         = "https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse"
	urlInfo           = "https://api.kaoyanvip.cn/learn/v1/delivery/pc/my_delivery/info/?my_delivery_id=%s"
	urlInfoUUID       = "https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse/%s"
	urlOutline        = "https://api.kaoyanvip.cn/learn/v1/delivery/my_unified_outline/structure/?my_delivery_id=%s&delivery_outline_id=%s"
	urlOutlineUUID    = "https://ytky.kaoyanvip.cn/api/v1/course/pc/mycourse/%s/%s/no_stage/"
	urlLiveRecords    = "https://ytky.kaoyanvip.cn/api/v1/living/%s/records"
	urlVideoM3U8      = "https://hls.videocc.net/%s/%s/%s.m3u8"
	urlVideoMP4       = "https://dpv.videocc.net/%s/%s/%s_%s.mp4"
	urlLivePlay       = "https://api.polyv.net/live/inner/v3/channel/playback/get-video-by-vid?vid=%s&timestamp=%s&channelId=%s&sign=%s&appId=%s"
	urlKeyToken       = "https://ytky.kaoyanvip.cn/api/v1/course/pc/video/play/token/?user_id=%s&vid=%s"
	urlTimestamp      = "https://acs.m.taobao.com/gw/mtop.common.getTimestamp/"
	polyvLiveAppID    = "fjd1n2k14a"
	polyvLiveSignTmpl = "polyv_playback_api_innerappId%schannelId%stimestamp%svid%spolyv_playback_api_inner"
)

var patterns = []string{`(?:[\w-]+\.)?kaoyanvip\.cn/`}

func init() {
	extractor.Register(&Kaoyanvip{}, extractor.SiteInfo{Name: "Kaoyanvip", URL: "kaoyanvip.cn", NeedAuth: true})
}

type Kaoyanvip struct{}

func (k *Kaoyanvip) Patterns() []string { return patterns }

var kaoyanIDRe = regexp.MustCompile(`(?i)(?:my_delivery_id|delivery_id)=([0-9]+)|uuid=([0-9A-Za-z_-]+)|/detail/([0-9A-Za-z_-]+)|/learn[^?#]*[?&]uuid=([0-9A-Za-z_-]+)`)

func (k *Kaoyanvip) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("kaoyanvip requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	token := pcsiteTokenFromJar(opts.Cookies)
	if token == "" {
		return nil, fmt.Errorf("kaoyanvip requires Pcsite-Token cookie")
	}
	headers := kaoyanHeaders(opts.Cookies, token)
	unionUUID, err := checkKaoyanLogin(c, headers)
	if err != nil {
		return nil, err
	}

	parsedID, parsedUUID, productID := parseKaoyanIDs(rawURL)
	courses, err := fetchKaoyanCourses(c, headers)
	if err != nil {
		return nil, err
	}
	cid, title, isDelivery := chooseKaoyanCourse(courses, parsedID, parsedUUID, productID)
	if cid == "" {
		cid = firstText(parsedID, parsedUUID)
		isDelivery = parsedUUID == ""
	}
	if cid == "" && len(courses) == 1 {
		cid, title, isDelivery = courses[0].ID, courses[0].Title, courses[0].IsDelivery
	}
	if cid == "" {
		return nil, fmt.Errorf("cannot parse kaoyanvip delivery id/uuid from URL: %s", rawURL)
	}

	outlineRoots, err := fetchKaoyanOutlineRoots(c, headers, cid, isDelivery)
	if err != nil {
		return nil, err
	}
	items := collectKaoyanItems(outlineRoots)
	entries := make([]*extractor.MediaInfo, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		key := item.Kind + ":" + item.VideoID + ":" + item.RoomID
		if seen[key] {
			continue
		}
		seen[key] = true
		entry, err := buildKaoyanEntry(c, headers, item, unionUUID, i+1)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("kaoyanvip: no playable video/live entries for course=%s", cid)
	}

	// Material/file download (source _download_files: my_outline_resource + pc/material)
	if isDelivery {
		materialEntries := fetchKaoyanMaterials(c, headers, cid)
		entries = append(entries, materialEntries...)
	}

	if title == "" {
		title = "kaoyanvip_" + cid
	}
	return &extractor.MediaInfo{Site: "kaoyanvip", Title: title, Entries: entries, Extra: map[string]any{"course_id": cid, "is_delivery": isDelivery, "unionuuid": unionUUID}}, nil
}

func kaoyanHeaders(jar http.CookieJar, token string) map[string]string {
	return map[string]string{
		"x-25-product-acceptable": "1",
		"platform":                "pc",
		"referer":                 urlReferer,
		"Referer":                 urlReferer,
		"cookie":                  cookieString(jar, "https", "www.kaoyanvip.cn"),
		"Token":                   token,
		"Accept":                  "application/json, text/plain, */*",
	}
}

func checkKaoyanLogin(c *util.Client, headers map[string]string) (string, error) {
	body, err := c.GetString(urlUserInfo, headers)
	if err != nil {
		return "", fmt.Errorf("kaoyanvip user info: %w", err)
	}
	var out struct {
		Code int `json:"code"`
		Data struct {
			UnionUUID string `json:"unionuuid"`
			UserID    string `json:"user_id"`
			ID        any    `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return "", fmt.Errorf("kaoyanvip user info parse: %w", err)
	}
	if out.Code != 20000 {
		return "", fmt.Errorf("kaoyanvip requires valid Pcsite-Token (code=%d)", out.Code)
	}
	return firstText(out.Data.UnionUUID, out.Data.UserID, out.Data.ID), nil
}

type kaoyanEnvelope[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

func kaoyanGetJSON[T any](c *util.Client, apiURL string, headers map[string]string) (T, error) {
	var zero T
	body, err := c.GetString(apiURL, headers)
	if err != nil {
		return zero, err
	}
	var out kaoyanEnvelope[T]
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return zero, fmt.Errorf("kaoyanvip parse %s: %w", apiURL, err)
	}
	if out.Code != 0 && out.Code != 200 && out.Code != 20000 {
		return zero, fmt.Errorf("kaoyanvip API code=%d msg=%s", out.Code, out.Msg)
	}
	return out.Data, nil
}

type kaoyanCourse struct {
	ID         string
	Title      string
	ProductID  string
	IsDelivery bool
}

func fetchKaoyanCourses(c *util.Client, headers map[string]string) ([]kaoyanCourse, error) {
	data, err := kaoyanGetJSON[any](c, urlCourse, headers)
	if err != nil {
		return nil, fmt.Errorf("kaoyanvip mycourse: %w", err)
	}
	var courses []kaoyanCourse
	for _, rec := range extractRecords(data) {
		deliveryID := firstText(rec["my_delivery_id"], rec["delivery_id"])
		uuid := firstText(rec["uuid"], rec["course_id"])
		id := firstText(deliveryID, uuid)
		if id == "" {
			continue
		}
		courses = append(courses, kaoyanCourse{ID: id, Title: firstText(rec["title"], rec["name"]), ProductID: firstText(rec["product_id"]), IsDelivery: deliveryID != ""})
	}
	return courses, nil
}

func chooseKaoyanCourse(courses []kaoyanCourse, deliveryID, uuid, productID string) (id, title string, isDelivery bool) {
	for _, course := range courses {
		if deliveryID != "" && course.ID != deliveryID {
			continue
		}
		if uuid != "" && course.ID != uuid {
			continue
		}
		if productID != "" && course.ProductID != productID {
			continue
		}
		return course.ID, course.Title, course.IsDelivery
	}
	return "", "", deliveryID != ""
}

func fetchKaoyanOutlineRoots(c *util.Client, headers map[string]string, cid string, isDelivery bool) ([]any, error) {
	var roots []any
	if isDelivery {
		info, err := kaoyanGetJSON[map[string]any](c, fmt.Sprintf(urlInfo, url.QueryEscape(cid)), headers)
		if err != nil {
			return nil, fmt.Errorf("kaoyanvip delivery info: %w", err)
		}
		for _, outline := range extractRecords(firstNonNil(info["outlines"], info["outline"])) {
			outlineID := firstText(outline["delivery_outline_id"], outline["outline_id"], outline["id"])
			if outlineID == "" {
				continue
			}
			data, err := kaoyanGetJSON[any](c, fmt.Sprintf(urlOutline, url.QueryEscape(cid), url.QueryEscape(outlineID)), headers)
			if err == nil {
				roots = append(roots, data)
			}
		}
	} else {
		info, err := kaoyanGetJSON[map[string]any](c, fmt.Sprintf(urlInfoUUID, url.PathEscape(cid)), headers)
		if err != nil {
			return nil, fmt.Errorf("kaoyanvip uuid info: %w", err)
		}
		for _, outline := range extractRecords(firstNonNil(info["outline"], info["outlines"])) {
			outlineID := firstText(outline["subject_id"], outline["outline_id"], outline["id"])
			if outlineID == "" {
				continue
			}
			data, err := kaoyanGetJSON[any](c, fmt.Sprintf(urlOutlineUUID, url.PathEscape(cid), url.PathEscape(outlineID)), headers)
			if err == nil {
				roots = append(roots, data)
			}
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("kaoyanvip: empty outline roots")
	}
	return roots, nil
}

type kaoyanItem struct {
	Kind    string
	Title   string
	VideoID string
	RoomID  string
}

func collectKaoyanItems(roots []any) []kaoyanItem {
	var items []kaoyanItem
	for _, root := range roots {
		walkKaoyan(root, nil, &items)
	}
	return items
}

func walkKaoyan(v any, prefix []int, items *[]kaoyanItem) {
	switch x := v.(type) {
	case []any:
		for i, item := range x {
			walkKaoyan(item, append(prefix, i+1), items)
		}
	case map[string]any:
		if sections := extractRecords(x["course_sections"]); len(sections) > 0 {
			for i, section := range sections {
				parseKaoyanSection(section, append(prefix, i+1), items)
			}
		}
		parseKaoyanSection(x, prefix, items)
		for _, key := range []string{"children", "child", "stage", "stages", "outline", "outlines", "course_sections", "list", "items", "results"} {
			if child, ok := x[key]; ok {
				walkKaoyan(child, prefix, items)
			}
		}
	}
}

func parseKaoyanSection(section map[string]any, prefix []int, items *[]kaoyanItem) {
	title := firstText(section["name"], section["title"], section["stage_name"], "未命名")
	if video, ok := section["video"].(map[string]any); ok {
		if vid := firstText(video["video_id"], video["vid"], video["file_id"]); vid != "" {
			*items = append(*items, kaoyanItem{Kind: "video", Title: formatIndexedTitle(prefix, title), VideoID: vid})
		}
	}
	if living, ok := section["living"].(map[string]any); ok {
		roomID := firstText(living["classroom"], living["room_id"], living["roomId"])
		if rec, ok := living["record"].(map[string]any); ok {
			if vid := firstText(rec["video_id"], rec["vid"]); vid != "" {
				*items = append(*items, kaoyanItem{Kind: "live", Title: formatIndexedTitle(prefix, title), VideoID: vid, RoomID: roomID})
			}
		}
	}
	if vid := firstText(section["video_id"], section["vid"]); vid != "" && section["video"] == nil {
		*items = append(*items, kaoyanItem{Kind: "video", Title: formatIndexedTitle(prefix, title), VideoID: vid})
	}
}

func buildKaoyanEntry(c *util.Client, headers map[string]string, item kaoyanItem, unionUUID string, index int) (*extractor.MediaInfo, error) {
	var playURL string
	var extraText string
	var err error
	if item.Kind == "live" {
		playURL, err = resolveKaoyanLiveURL(c, headers, item.VideoID, item.RoomID)
	} else {
		playURL, extraText, err = resolveKaoyanVideoURL(c, headers, item.VideoID, unionUUID)
	}
	if err != nil || playURL == "" {
		if err == nil {
			err = fmt.Errorf("empty playback URL")
		}
		return nil, err
	}
	format := mediaExt(playURL)
	stream := extractor.Stream{Quality: "best", URLs: []string{playURL}, Format: format, Headers: map[string]string{"Referer": urlReferer}}
	if format == "m3u8" {
		stream.NeedMerge = true
	}
	extra := map[string]any{"video_id": item.VideoID, "room_id": item.RoomID, "type": item.Kind}
	if extraText != "" {
		extra["m3u8_text"] = extraText
	}
	return &extractor.MediaInfo{Site: "kaoyanvip", Title: firstText(item.Title, fmt.Sprintf("[%d]--未命名", index)), Streams: map[string]extractor.Stream{"best": stream}, Extra: extra}, nil
}

func resolveKaoyanVideoURL(c *util.Client, headers map[string]string, videoID, unionUUID string) (string, string, error) {
	vid := strings.Split(videoID, "_")[0]
	if len(vid) < 10 {
		return "", "", fmt.Errorf("kaoyanvip invalid polyv vid=%s", videoID)
	}
	master := fmt.Sprintf(urlVideoM3U8, vid[:10], vid[len(vid)-1:], vid)
	body, err := c.GetString(master, headers)
	if err != nil {
		return fmt.Sprintf(urlVideoMP4, vid[:10], vid[len(vid)-1:], vid, "3"), "", nil
	}
	selected := master
	variants := m3u8VariantRe.FindAllStringSubmatch(body, -1)
	if len(variants) > 0 {
		choice := variants[len(variants)-1][1]
		base := master[:strings.LastIndex(master, "/")+1]
		selected = base + strings.TrimSpace(choice)
	}
	extraText := ""
	if unionUUID != "" {
		if variantBody, err := c.GetString(selected, headers); err == nil {
			if keyMatch := m3u8KeyURIRe.FindStringSubmatch(variantBody); len(keyMatch) > 1 {
				if token := fetchKaoyanKeyToken(c, headers, unionUUID, videoID); token != "" {
					keyWithToken := keyMatch[1] + "?token=" + url.QueryEscape(token)
					extraText = strings.ReplaceAll(variantBody, keyMatch[1], keyWithToken)
				}
			}
		}
	}
	return selected, extraText, nil
}

var (
	m3u8VariantRe = regexp.MustCompile(`#EXT-X-STREAM-INF.*?\n([^#\n][^\n]*)`)
	m3u8KeyURIRe  = regexp.MustCompile(`URI="(.*?)"`)
)

func fetchKaoyanKeyToken(c *util.Client, headers map[string]string, unionUUID, videoID string) string {
	body, err := c.GetString(fmt.Sprintf(urlKeyToken, url.QueryEscape(unionUUID), url.QueryEscape(videoID)), headers)
	if err != nil {
		return ""
	}
	if m := regexp.MustCompile(`"data"\s*:\s*"([\w-]+)"`).FindStringSubmatch(body); len(m) > 1 {
		return m[1]
	}
	var out kaoyanEnvelope[string]
	if err := json.Unmarshal([]byte(body), &out); err == nil && out.Data != "" {
		return out.Data
	}
	return ""
}

func resolveKaoyanLiveURL(c *util.Client, headers map[string]string, videoID, roomID string) (string, error) {
	if roomID == "" {
		return "", fmt.Errorf("kaoyanvip live missing room_id")
	}
	recordsBody, err := c.GetString(fmt.Sprintf(urlLiveRecords, url.PathEscape(roomID)), headers)
	if err != nil {
		return "", fmt.Errorf("kaoyanvip live records: %w", err)
	}
	m := regexp.MustCompile(`"plv_channel"\s*:\s*(\d+)`).FindStringSubmatch(recordsBody)
	if len(m) < 2 {
		return "", fmt.Errorf("kaoyanvip live records missing plv_channel")
	}
	channelID := m[1]
	timestamp := fetchKaoyanTimestamp(c)
	signSeed := fmt.Sprintf(polyvLiveSignTmpl, polyvLiveAppID, channelID, timestamp, videoID)
	sum := md5.Sum([]byte(signSeed))
	sign := strings.ToUpper(hex.EncodeToString(sum[:]))
	apiURL := fmt.Sprintf(urlLivePlay, url.QueryEscape(videoID), url.QueryEscape(timestamp), url.QueryEscape(channelID), url.QueryEscape(sign), url.QueryEscape(polyvLiveAppID))
	data, err := kaoyanGetJSON[map[string]any](c, apiURL, headers)
	if err != nil {
		return "", fmt.Errorf("kaoyanvip live polyv: %w", err)
	}
	playURL := firstText(data["fileUrl"], data["url"])
	if playURL == "" {
		return "", fmt.Errorf("kaoyanvip live polyv returned no fileUrl/url")
	}
	return playURL, nil
}

func fetchKaoyanTimestamp(c *util.Client) string {
	body, err := c.GetString(urlTimestamp, nil)
	if err == nil {
		if m := regexp.MustCompile(`"t"\s*:\s*"(\d+)"`).FindStringSubmatch(body); len(m) > 1 {
			return m[1]
		}
	}
	return strconv.FormatInt(time.Now().UnixMilli(), 10)
}

func parseKaoyanIDs(rawURL string) (deliveryID, uuid, productID string) {
	if m := kaoyanIDRe.FindStringSubmatch(rawURL); len(m) > 0 {
		deliveryID = firstText(m[1])
		uuid = firstText(m[2], m[4])
		if deliveryID == "" && uuid == "" {
			productID = firstText(m[3])
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		q := u.Query()
		deliveryID = firstText(deliveryID, q.Get("my_delivery_id"), q.Get("delivery_id"))
		uuid = firstText(uuid, q.Get("uuid"))
		productID = firstText(productID, q.Get("product_id"), q.Get("pid"))
	}
	return
}

func extractRecords(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"course", "results", "list", "items", "outlines", "outline", "children", "course_sections", "data"} {
			if recs := extractRecords(x[key]); len(recs) > 0 {
				return recs
			}
		}
	}
	return nil
}

func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func pcsiteTokenFromJar(jar http.CookieJar) string {
	for _, host := range []string{"www.kaoyanvip.cn", "kaoyanvip.cn", "ytky.kaoyanvip.cn"} {
		for _, ck := range jar.Cookies(&url.URL{Scheme: "https", Host: host}) {
			if ck.Name == "Pcsite-Token" && ck.Value != "" {
				return ck.Value
			}
		}
	}
	return ""
}

func cookieString(jar http.CookieJar, scheme, host string) string {
	cookies := jar.Cookies(&url.URL{Scheme: scheme, Host: host})
	parts := make([]string, 0, len(cookies))
	for _, ck := range cookies {
		if ck.Value != "" {
			parts = append(parts, ck.Name+"="+ck.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func formatIndexedTitle(prefix []int, title string) string {
	if len(prefix) == 0 {
		return title
	}
	parts := make([]string, len(prefix))
	for i, n := range prefix {
		parts[i] = strconv.Itoa(n)
	}
	return fmt.Sprintf("[%s]--%s", strings.Join(parts, "."), title)
}

func mediaExt(u string) string {
	lu := strings.ToLower(u)
	switch {
	case strings.Contains(lu, ".m3u8") || strings.Contains(strings.TrimSpace(u), "#EXTM3U"):
		return "m3u8"
	case strings.Contains(lu, ".mp3"):
		return "mp3"
	default:
		return "mp4"
	}
}

func firstText(values ...any) string {
	for _, v := range values {
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}

func stringValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return strings.TrimSpace(x.String())
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return strings.TrimSpace(fmt.Sprint(x))
	default:
		return ""
	}
}

func fetchKaoyanMaterials(c *util.Client, headers map[string]string, cid string) []*extractor.MediaInfo {
	var entries []*extractor.MediaInfo
	seen := map[string]bool{}

	addFile := func(name, link string) {
		if link == "" || seen[link] {
			return
		}
		seen[link] = true
		ext := "file"
		if idx := strings.LastIndex(link, "."); idx > 0 {
			if q := strings.Index(link[idx:], "?"); q > 0 {
				ext = link[idx+1 : idx+q]
			} else {
				ext = link[idx+1:]
			}
		}
		if name == "" {
			name = "material"
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:  "kaoyanvip",
			Title: name,
			Streams: map[string]extractor.Stream{
				ext: {Quality: "source", URLs: []string{link}, Format: ext, Headers: map[string]string{"Referer": "https://www.kaoyanvip.cn/"}},
			},
		})
	}

	sourceURL := fmt.Sprintf("https://api.kaoyanvip.cn/learn/v1/delivery/my_outline_resource/?my_delivery_id=%s&resource_type=material", cid)
	if body, err := c.GetString(sourceURL, headers); err == nil {
		var resp struct{ Data []struct{ CourseSections []struct{ Materials []struct{ Title, DownloadLink string } } } }
		if json.Unmarshal([]byte(body), &resp) == nil {
			for _, d := range resp.Data {
				for _, sec := range d.CourseSections {
					for _, m := range sec.Materials {
						addFile(m.Title, m.DownloadLink)
					}
				}
			}
		}
	}

	fileURL := fmt.Sprintf("https://api.kaoyanvip.cn/learn/v1/delivery/pc/material/?my_delivery_id=%s", cid)
	if body, err := c.GetString(fileURL, headers); err == nil {
		var resp struct {
			Data struct {
				MaterialList []struct {
					Name string `json:"name"`
					Link string `json:"file_url"`
				}
			}
		}
		if json.Unmarshal([]byte(body), &resp) == nil {
			for _, m := range resp.Data.MaterialList {
				addFile(m.Name, m.Link)
			}
		}
	}

	return entries
}
