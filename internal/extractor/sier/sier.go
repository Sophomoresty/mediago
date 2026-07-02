// Package sier implements an extractor for sieredu.com courses.
package sier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	user_agent             = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	referer                = "https://player.sieredu.com/"
	user_info_api          = "https://www.sieredu.com/web/user/getUserInfo"
	course_list_api        = "https://www.sieredu.com/web/uc/course/myCourse"
	plan_api               = "https://www.sieredu.com/web/course/queryPlanList"
	catalog_api            = "https://www.sieredu.com/web/course/catalog/getCourseCatalogDetail"
	check_play_api         = "https://www.sieredu.com/web/uc/play/checkPlay"
	load_play_data_api     = "https://www.sieredu.com/web/uc/play/loadPlayData"
	token_api              = "https://api2.sieredu.com/v1/video/c/videoFile/getToken"
	legacy_token_api       = "https://www.sieredu.com/web/video/videoFile/getToken"
	open_course_detail_api = "https://www.sieredu.com/web/opencourse/openCourseCouMaterialDetail"
	open_course_check_api  = "https://www.sieredu.com/web/play/checkOpenCoursePlay"
	product_detail_api     = "https://www.sieredu.com/web/product/detail"
	getplayinfo_api        = "https://playvideo.vodplayvideo.net/getplayinfo/v4/%s/%s"
	SIER_APP_SECRET        = "e1018d3bb5664bada75ef6a619a07900"
	SIER_APP_KEY           = "10000"
	SIER_TOKEN_AES_KEY_B64 = "3q2+7JIh4SLfKp9mAXFv7A=="
	SIER_VOD_DEFAULT_APPID = "1500015546"
)

var patterns = []string{`(?:[\w-]+\.)?sieredu\.com/`}

func init() {
	extractor.Register(&Sier{}, extractor.SiteInfo{Name: "Sier", URL: "sieredu.com", NeedAuth: true})
}

type Sier struct{}

func (s *Sier) Patterns() []string { return patterns }

func (s *Sier) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("sier requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	ck := cookieInfoFromJar(opts.Cookies)
	h := sierHeaders(ck, referer)
	openID := match1(rawURL, `[?&]openCourseId=(\d+)`)
	courseID := first(match1(rawURL, `[?&]courseId=(\d+)`), match1(rawURL, `/course/(\d+)`))
	if openID != "" {
		return extractOpenCourse(c, h, openID)
	}
	if courseID == "" {
		courses, _ := fetchCourseList(c, h)
		if len(courses) > 0 {
			courseID = courses[0].ID
		}
	}
	if courseID == "" {
		return nil, fmt.Errorf("cannot parse sier courseId/openCourseId from URL")
	}
	return extractNormalCourse(c, h, courseID)
}

type cookieInfo struct{ Cookie, SID, DeviceID string }
type courseRef struct{ ID, Title string }
type videoInfo struct {
	VideoID, CatalogID, MaterialID, VerificationCode, Title, DirectURL string
	CourseID, BuyCourseID, PrevCatalogID, OpenCourseID                 string
	Open                                                               bool
}
type fileInfo struct {
	Name, URL, Format, CatalogID, MaterialID string
}
type playInfo struct {
	URL      string
	Size     int64
	M3U8Text string
}

func extractOpenCourse(c *util.Client, h map[string]string, id string) (*extractor.MediaInfo, error) {
	detail, err := requestJSON(c, "POST", open_course_detail_api, map[string]string{"id": id}, nil, h, referer)
	if err != nil {
		return nil, fmt.Errorf("sier open course detail: %w", err)
	}
	entity := unwrapMap(detail)
	title := sanitize(first(textAt(entity, "name", "courseName", "openCourseName", "title"), "sier_open_"+id))
	materials := extractLists(entity, "openCourseMaterialList", "materialList", "list")
	var videos []videoInfo
	var files []fileInfo
	for i, m := range materials {
		kind := strings.ToLower(textAt(m, "typeKey", "type", "materialTypeKey"))
		direct := videoPlayURL(m)
		if direct != "" || strings.Contains(kind, "video") {
			videos = append(videos, videoInfo{VideoID: first(textAt(m, "videoId", "fileId"), textAt(unwrapMap(m["playUrl"]), "videoId", "fileId")), MaterialID: first(textAt(m, "id", "materialId"), fmt.Sprint(i+1)), VerificationCode: textAt(m, "verificationCode"), Title: fmt.Sprintf("[%d]--%s", i+1, first(textAt(m, "name", "title", "materialName"), "视频")), DirectURL: direct, OpenCourseID: id, Open: true})
			continue
		}
		if f := fileFromNode(m, "资料", first(textAt(m, "id", "materialId"), fmt.Sprint(i+1))); f.URL != "" {
			f.Name = fmt.Sprintf("(%d)--%s", len(files)+1, sanitize(first(f.Name, "资料")))
			files = append(files, f)
		}
	}
	return buildCourse(c, h, title, id, videos, files)
}
func extractNormalCourse(c *util.Client, h map[string]string, id string) (*extractor.MediaInfo, error) {
	title := "sier_" + id
	courses, _ := fetchCourseList(c, h)
	for _, it := range courses {
		if it.ID == id && it.Title != "" {
			title = it.Title
			break
		}
	}
	plan, err := requestJSON(c, "POST", plan_api, map[string]string{"courseId": id, "sceneId": "0"}, nil, h, "https://study.sieredu.com/")
	if err != nil {
		return nil, fmt.Errorf("sier plan list: %w", err)
	}
	// Fetch product detail for pricing/entitlement if product_id is available.
	productID := extractProductID(plan, id)
	price := fetchProductPrice(c, h, productID)
	var videos []videoInfo
	var files []fileInfo
	for _, p := range extractLists(unwrapMap(plan), "list", "records", "courseList") {
		for _, cat := range extractLists(p, "catalogList", "children", "nodeList") {
			catalogID := first(textAt(cat, "catalogId", "id"), textAt(unwrapMap(cat["resource"]), "catalogId", "id"))
			if catalogID == "" {
				continue
			}
			detail, _ := requestJSON(c, "POST", catalog_api, nil, map[string]any{"courseId": id, "catalogId": catalogID}, h, "https://study.sieredu.com/")
			detailMap := unwrapMap(detail)
			collected := collectVideos(detailMap, id, catalogID)
			files = append(files, collectFiles(detailMap, catalogID)...)
			if len(collected) == 0 {
				collected = collectVideos(cat, id, catalogID)
			}
			files = append(files, collectFiles(cat, catalogID)...)
			videos = append(videos, collected...)
		}
	}
	if len(videos) == 0 {
		videos = collectVideos(unwrapMap(plan), id, "")
	}
	if len(files) == 0 {
		files = collectFiles(unwrapMap(plan), "")
	}
	info, err := buildCourse(c, h, sanitize(title), id, videos, files)
	if err != nil {
		return nil, err
	}
	if price > 0 {
		if info.Extra == nil {
			info.Extra = map[string]any{}
		}
		info.Extra["price"] = price
	}
	return info, nil
}
func buildCourse(c *util.Client, h map[string]string, title, courseID string, videos []videoInfo, files []fileInfo) (*extractor.MediaInfo, error) {
	seen := map[string]bool{}
	var entries []*extractor.MediaInfo
	for i, v := range videos {
		key := first(v.VideoID, v.DirectURL, v.CatalogID, v.MaterialID)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		play := resolveVideo(c, h, v)
		if play.URL == "" {
			continue
		}
		name := sanitize(first(v.Title, fmt.Sprintf("[%d]--视频", i+1)))
		format := pickFormat(play.URL)
		stream := extractor.Stream{Quality: "best", URLs: []string{play.URL}, Format: format, Size: play.Size, Headers: map[string]string{"Referer": referer}}
		if format == "m3u8" {
			stream.NeedMerge = true
		}
		extra := map[string]any{"video_id": v.VideoID, "catalog_id": v.CatalogID, "material_id": v.MaterialID}
		if play.M3U8Text != "" {
			extra["m3u8_text"] = play.M3U8Text
			extra["source_type"] = "m3u8_text"
		}
		entries = append(entries, &extractor.MediaInfo{Site: "sier", Title: name, Streams: map[string]extractor.Stream{"best": stream}, Extra: extra})
	}
	for i, f := range files {
		key := first(f.URL, f.CatalogID, f.MaterialID)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		format := first(f.Format, pickFormat(f.URL))
		name := sanitize(first(f.Name, fmt.Sprintf("(%d)--资料", i+1)))
		stream := extractor.Stream{Quality: "best", URLs: []string{f.URL}, Format: format, Headers: map[string]string{"Referer": referer}}
		entries = append(entries, &extractor.MediaInfo{Site: "sier", Title: name, Streams: map[string]extractor.Stream{"best": stream}, Extra: map[string]any{"type": "file", "catalog_id": f.CatalogID, "material_id": f.MaterialID}})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("sier: no playable videos/materials found from catalog/play APIs")
	}
	return &extractor.MediaInfo{Site: "sier", Title: title, Entries: entries, Extra: map[string]any{"course_id": courseID}}, nil
}
func fetchCourseList(c *util.Client, h map[string]string) ([]courseRef, error) {
	resp, err := requestJSON(c, "POST", course_list_api, map[string]string{"subjectId": "0"}, nil, h, "https://study.sieredu.com/")
	if err != nil {
		return nil, err
	}
	var out []courseRef
	for _, it := range extractLists(unwrapMap(resp), "excellentCourseList", "courseList", "list", "records") {
		if id := first(textAt(it, "courseId", "id")); id != "" {
			out = append(out, courseRef{ID: id, Title: textAt(it, "courseName", "name", "title")})
		}
	}
	return out, nil
}

// fetchProductPrice fetches the product detail from the product/detail API to
// extract pricing info for the course package (source: Sier_Course._get_course_price).
func fetchProductPrice(c *util.Client, h map[string]string, productID string) float64 {
	if productID == "" {
		return 0
	}
	resp, err := requestJSON(c, "POST", product_detail_api, map[string]string{"id": productID}, nil, h, "https://study.sieredu.com/")
	if err != nil {
		return 0
	}
	entity := unwrapMap(resp)
	return extractPrice(entity)
}

// extractProductID finds a product_id from the plan response or course data.
func extractProductID(plan any, courseID string) string {
	m := unwrapMap(plan)
	if id := first(textAt(m, "productId", "product_id")); id != "" {
		return id
	}
	for _, list := range extractLists(m, "list", "records", "courseList") {
		if id := first(textAt(list, "productId", "product_id")); id != "" {
			return id
		}
	}
	return ""
}

// extractPrice looks for standard price keys in a map.
func extractPrice(m map[string]any) float64 {
	for _, key := range []string{"price", "salePrice", "sellPrice", "minPrice", "payPrice", "coursePrice", "productPrice", "actualPrice", "discountPrice"} {
		if v := textAt(m, key); v != "" {
			var f float64
			if _, err := fmt.Sscanf(v, "%f", &f); err == nil && f > 0 {
				return f
			}
		}
	}
	return 0
}

func collectVideos(v any, courseID, fallbackCatalog string) []videoInfo {
	var out []videoInfo
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			kind := strings.ToLower(first(textAt(t, "discriminator", "materialTypeKey", "typeKey", "resourceType", "type"), textAt(unwrapMap(t["material"]), "typeKey")))
			res, mat := unwrapMap(t["resource"]), unwrapMap(t["material"])
			vid := first(textAt(res, "videoId", "fileId", "id"), textAt(mat, "videoId", "fileId"), textAt(t, "videoId", "fileId", "resourceId"))
			direct := first(videoPlayURL(t), videoPlayURL(res), videoPlayURL(mat))
			if vid != "" || direct != "" || strings.Contains(kind, "video") || strings.Contains(kind, "live") || kind == "tc" {
				catalogID := first(textAt(t, "catalogId", "id"), fallbackCatalog)
				out = append(out, videoInfo{
					VideoID:          vid,
					CatalogID:        catalogID,
					CourseID:         courseID,
					BuyCourseID:      first(textAt(t, "buyCourseId", "buy_course_id"), courseID),
					PrevCatalogID:    first(textAt(t, "prevCatalogId", "prev_catalog_id"), "0"),
					VerificationCode: first(textAt(t, "verificationCode"), textAt(res, "verificationCode"), textAt(mat, "verificationCode")),
					Title:            first(textAt(t, "catalogName", "lessonName", "name", "title"), textAt(res, "name", "title"), textAt(mat, "name", "title"), "视频"),
					DirectURL:        direct,
				})
			}
			for _, k := range []string{"children", "nodeList", "childList", "syllabus", "courseList", "unitList", "catalogList"} {
				walk(t[k])
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}
func resolveVideo(c *util.Client, h map[string]string, v videoInfo) playInfo {
	if strings.HasPrefix(v.DirectURL, "http") {
		return playInfo{URL: v.DirectURL}
	}
	checkURL, params := check_play_api, map[string]string{
		"catalogId":     v.CatalogID,
		"courseId":      v.CourseID,
		"buyCourseId":   first(v.BuyCourseID, v.CourseID),
		"prevCatalogId": first(v.PrevCatalogID, "0"),
	}
	if v.Open {
		checkURL, params = open_course_check_api, map[string]string{"openCourseId": v.OpenCourseID, "materialId": v.MaterialID}
	}
	checked, _ := requestJSON(c, "POST", checkURL, params, nil, h, referer)
	entity := unwrapMap(checked)
	if sign := textAt(entity, "sign"); sign != "" {
		loaded, _ := requestJSON(c, "POST", load_play_data_api, map[string]string{"sign": sign}, nil, h, referer)
		entity = mergeMaps(entity, unwrapMap(loaded))
	}
	if u := first(videoPlayURL(entity), findURL(entity)); u != "" {
		return playInfo{URL: u}
	}
	fileID := first(v.VideoID, textAt(entity, "videoId", "fileId"))
	verify := first(v.VerificationCode, textAt(entity, "verificationCode"))
	if fileID == "" || verify == "" {
		return playInfo{}
	}
	tokenInfo := getTokenInfo(c, h, fileID, verify)
	psign := decryptPsign(tokenInfo)
	if psign == "" {
		psign = first(textAt(tokenInfo, "psign", "pSign", "sign"), textAt(entity, "psign", "pSign"))
	}
	appID := first(textAt(tokenInfo, "appId"), SIER_VOD_DEFAULT_APPID)
	fileID = first(textAt(tokenInfo, "fileId"), fileID)
	if psign == "" {
		return playInfo{}
	}
	return requestVODPlayInfo(c, h, appID, fileID, psign)
}
func getTokenInfo(c *util.Client, h map[string]string, fileID, verification string) map[string]any {
	payload := map[string]any{"verificationCode": verification, "fileId": fileID}
	for _, api := range []string{token_api, legacy_token_api} {
		resp, err := requestJSON(c, "POST", api, nil, payload, h, referer)
		if err == nil {
			m := unwrapMap(resp)
			if len(m) > 0 {
				return m
			}
		}
	}
	return map[string]any{}
}
func requestVODPlayInfo(c *util.Client, h map[string]string, appID, fileID, psign string) playInfo {
	api := fmt.Sprintf(getplayinfo_api, url.PathEscape(appID), url.PathEscape(fileID))
	for _, overlay := range sierVODOverlays {
		playURL := buildSierPlayInfoURL(api, psign, overlay)
		body, err := c.GetString(playURL, map[string]string{"Referer": referer, "User-Agent": user_agent})
		if err != nil {
			continue
		}
		var resp any
		if err := json.Unmarshal([]byte(body), &resp); err != nil || !sierPlayInfoOK(resp) {
			continue
		}
		if play := extractSierPlayInfo(c, h, resp, overlay); play.URL != "" {
			return play
		}
	}
	return playInfo{}
}
func requestJSON(c *util.Client, method, api string, params map[string]string, jsonBody any, h map[string]string, ref string) (any, error) {
	u, err := url.Parse(api)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	hh := cloneHeaders(h)
	if ref != "" {
		hh["Referer"] = ref
		if strings.Contains(ref, "www.sieredu.com") || strings.Contains(ref, "study.sieredu.com") {
			hh["Origin"] = "https://www.sieredu.com"
		}
	}
	var body string
	if strings.EqualFold(method, "POST") && jsonBody != nil {
		b, _ := json.Marshal(jsonBody)
		body = string(b)
		hh["Content-Type"] = "application/json;charset=UTF-8"
	}
	applySierSignature(method, u, params, body, hh)
	if strings.EqualFold(method, "POST") {
		var r io.Reader = strings.NewReader("")
		if body != "" {
			r = bytes.NewReader([]byte(body))
		}
		resp, err := c.Post(u.String(), r, hh)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, u.String())
		}
		rb, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		body = string(rb)
	} else {
		body, err = c.GetString(u.String(), hh)
		if err != nil {
			return nil, err
		}
	}
	var out any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}
