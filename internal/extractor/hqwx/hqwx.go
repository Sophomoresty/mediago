// Package hqwx implements source-aligned extraction for hqwx.com (环球网校).
package hqwx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	referer = "https://user.hqwx.com/"

	check_url                 = "https://japi.hqwx.com/uc/study/v2/getList"
	url_course_list           = "https://japi.hqwx.com/uc/study/v2/getList"
	url_stages                = "https://japi.hqwx.com/al/v3/getStagesByProduct"
	url_schedules             = "https://adminapi.hqwx.com/goods-siteapp/app/v1/course-schedules/list"
	url_lessons               = "https://adminapi.hqwx.com/goods-siteapp/app/v2/course-lessons/list"
	url_stage_tasks           = "https://japi.hqwx.com/al/v3/selfTask/getStageTasks"
	url_resource              = "https://japi.hqwx.com/al/userKnowledge/resource"
	url_resource_batch        = "https://japi.hqwx.com/al/userKnowledge/resourceBatch"
	url_subtitle              = "https://japi.hqwx.com/resourceVideo/getSubtitleUrl"
	url_last_video_log        = "https://japi.hqwx.com/uc/study/getLastUserVideoLogByGoodsId"
	url_course_detail         = "https://japi.hqwx.com/uc/study/getCourseDetail"
	url_goods_plan_categories = "https://japi.hqwx.com/uc/study/listUserGoodsPlanTotalCategorySort"
	url_category_plan         = "https://japi.hqwx.com/uc/v2/study/getCategoryStudyPlanGroupInfo"
	url_lesson_list_v2        = "https://japi.hqwx.com/uc/v2/study/getLessonList"
	url_lesson_list_v7        = "https://japi.hqwx.com/uc/v7/study/getLessonList"
	url_order_list            = "https://japi.hqwx.com/buy/order/getUserOrderList?stateList=0&stateList=150&stateList=180&stateList=185&stateList=190&stateList=200&stateList=400"
	url_price                 = "https://kjapi.98809.com/web/goods/getGoodsDetail?appId=%s&appid=%s&_appid=%s&pschId=%s&schId=%s&orgId=%s&org_id=%s&platform=web&siteId=300&passport=%s&edu24ol_token=%s&gidList=%s&callback=jsonp_%s"

	DEFAULT_APPID       = "wwwedu24ol"
	DEFAULT_ORG_ID      = "2"
	DEFAULT_OS          = "3"
	DEFAULT_V           = "1.0.0"
	DEFAULT_SCH_ID      = "2"
	DEFAULT_PSCH_ID     = "14"
	DEFAULT_PLATFORM    = "web"
	DEFAULT_CATEGORY_ID = "5608"
	USER_AGENT          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"

	TYPE_STAGE_TASK      = "type13"
	TYPE_SCHEDULE_LESSON = "type2"
	TYPE_OPEN_COURSE     = "type_open"
	TYPE_STUDY_PLAN      = "type_study_plan"
	TYPE_UNKNOWN         = "unknown"
)

var (
	patterns = []string{`\s*((https?://(?:[\w-]+\.)*hqwx\.com.*?goods(Id)?=\d+)|(https?://(?:[\w-]+\.)*hqwx\.com.*?product(Id)?=\d+)|(https?://(?:[\w-]+\.)*hqwx\.com/v2/course/detail/\d+/\d+\.html)|(https?://(?:[\w-]+\.)*hqwx\.com(?:[/?#].*)?)|(#小程序://环球网校))`}

	detailRe       = regexp.MustCompile(`/v2/course/detail/(\d+)/(\d+)\.html`)
	goodsParamRe   = regexp.MustCompile(`(?i)(?:[?&#](?:goodsId|goods)=)(\d+)`)
	productParamRe = regexp.MustCompile(`(?i)(?:[?&#](?:productId|product)=)(\d+)`)
	courseParamRe  = regexp.MustCompile(`(?i)(?:[?&#](?:courseId|class_id)=)(\d+)`)
)

func init() {
	extractor.Register(&Hqwx{}, extractor.SiteInfo{Name: "Hqwx", URL: "hqwx.com", NeedAuth: true})
}

type Hqwx struct{}

func (h *Hqwx) Patterns() []string { return patterns }

type hqwxCtx struct {
	c       *util.Client
	headers map[string]string
	cookie  string

	passport    string
	goodsID     string
	productID   string
	cid         string
	title       string
	courseType  string
	courseList  []map[string]any
	course      map[string]any
	detail      map[string]any
	stageCache  []map[string]any
	schedCache  []map[string]any
	planCats    []map[string]any
	planGroups  map[string][]map[string]any
	planLessons map[string][]map[string]any
	resource    map[string]map[string]any
	subtitles   map[string]string
}

func (h *Hqwx) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("hqwx requires login cookies")
	}
	x, err := newCtx(opts.Cookies)
	if err != nil {
		return nil, err
	}
	if err := x.prepare(rawURL); err != nil {
		return nil, err
	}
	items, err := x.loadItems()
	if err != nil {
		return nil, err
	}
	return x.mediaFromItems(items)
}

func newCtx(jar http.CookieJar) (*hqwxCtx, error) {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookie := cookieHeader(jar, []string{
		referer,
		"https://www.hqwx.com/",
		"https://japi.hqwx.com/",
		"https://adminapi.hqwx.com/",
	})
	if strings.TrimSpace(cookie) == "" {
		return nil, fmt.Errorf("hqwx requires non-empty login cookie jar")
	}
	cookies := parseCookieHeader(cookie)
	passport := firstNonEmpty(cookies["passport"], cookies["passportCors"])
	headers := map[string]string{
		"User-Agent": USER_AGENT,
		"Accept":     "application/json, text/plain, */*",
		"Origin":     "https://user.hqwx.com",
		"Referer":    referer,
		"Cookie":     cookie,
	}
	if passport != "" {
		headers["edu24ol-token"] = passport
	}
	return &hqwxCtx{
		c:           c,
		headers:     headers,
		cookie:      cookie,
		passport:    passport,
		courseType:  TYPE_UNKNOWN,
		planGroups:  map[string][]map[string]any{},
		planLessons: map[string][]map[string]any{},
		resource:    map[string]map[string]any{},
		subtitles:   map[string]string{},
	}, nil
}

func (x *hqwxCtx) prepare(rawURL string) error {
	x.parseCourseIDs(rawURL)
	if x.goodsID == "" && x.productID == "" {
		return fmt.Errorf("cannot parse hqwx goodsId/productId from URL: %s", rawURL)
	}
	if course := x.findCourseByID(firstNonEmpty(x.productID, x.goodsID)); len(course) > 0 {
		x.course = course
	}
	if len(x.course) > 0 {
		if x.goodsID == "" {
			x.goodsID = str(x.course["goodsId"])
		}
		if x.productID == "" {
			x.productID = firstNonEmpty(str(x.course["oneProductId"]), str(x.course["productId"]), str(x.course["courseId"]))
		}
		x.title = cleanName(str(x.course["goodsName"]))
	}
	if x.productID == "" {
		x.productID = x.goodsID
	}
	x.cid = firstNonEmpty(x.productID, x.goodsID)
	if x.title == "" {
		x.title = "hqwx_" + firstNonEmpty(x.goodsID, x.productID)
	}
	ctype, err := x.detectCourseType()
	if err != nil {
		return err
	}
	x.courseType = ctype
	if x.courseType == TYPE_UNKNOWN {
		return fmt.Errorf("hqwx: unsupported or empty course structure for goodsId=%s productId=%s", x.goodsID, x.productID)
	}
	return nil
}

func (x *hqwxCtx) parseCourseIDs(rawURL string) {
	if m := detailRe.FindStringSubmatch(rawURL); len(m) == 3 {
		x.goodsID = m[1]
		x.productID = m[2]
		return
	}
	if m := goodsParamRe.FindStringSubmatch(rawURL); len(m) == 2 {
		x.goodsID = m[1]
	}
	if m := productParamRe.FindStringSubmatch(rawURL); len(m) == 2 {
		x.productID = m[1]
	}
	if x.productID == "" {
		if m := courseParamRe.FindStringSubmatch(rawURL); len(m) == 2 {
			x.productID = m[1]
		}
	}
}

func (x *hqwxCtx) baseParams() map[string]string {
	return map[string]string{
		"edu24ol_token": x.passport,
		"passport":      x.passport,
		"platform":      DEFAULT_PLATFORM,
		"pschId":        DEFAULT_PSCH_ID,
		"schId":         DEFAULT_SCH_ID,
		"v":             DEFAULT_V,
		"_v":            DEFAULT_V,
		"os":            DEFAULT_OS,
		"_os":           DEFAULT_OS,
		"org_id":        DEFAULT_ORG_ID,
		"_org_id":       DEFAULT_ORG_ID,
		"appid":         DEFAULT_APPID,
		"_appid":        DEFAULT_APPID,
	}
}

func (x *hqwxCtx) loadJSONGet(endpoint string, params map[string]string, headers map[string]string, out any) (map[string]any, error) {
	if params != nil {
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		endpoint += "?" + values.Encode()
	}
	if headers == nil {
		headers = x.headers
	}
	body, err := x.c.GetString(endpoint, headers)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", endpoint, err)
	}
	if out != nil {
		if err := json.Unmarshal([]byte(body), out); err != nil {
			return nil, fmt.Errorf("decode %s: %w", endpoint, err)
		}
	}
	return root, nil
}

func (x *hqwxCtx) loadJSONPost(endpoint string, data map[string]string, headers map[string]string, out any) (map[string]any, error) {
	if headers == nil {
		headers = x.headers
	}
	body, err := x.c.PostForm(endpoint, data, headers)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", endpoint, err)
	}
	if out != nil {
		if err := json.Unmarshal([]byte(body), out); err != nil {
			return nil, fmt.Errorf("decode %s: %w", endpoint, err)
		}
	}
	return root, nil
}

func (x *hqwxCtx) adminAPIHeaders() map[string]string {
	headers := cloneHeaders(x.headers)
	headers["edu24ol-token"] = x.passport
	return headers
}

func (x *hqwxCtx) mediaFromItems(items []hqwxItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, item := range items {
		entry, err := x.mediaEntry(item)
		if err != nil || entry == nil {
			continue
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("hqwx: no playable videos or files for course type %s", x.courseType)
	}
	return &extractor.MediaInfo{Site: "hqwx", Title: x.title, Entries: entries}, nil
}

func (x *hqwxCtx) mediaEntry(item hqwxItem) (*extractor.MediaInfo, error) {
	mediaURL := item.URL
	if item.Kind == "video" && mediaURL == "" {
		resourceInfo, err := x.resolveResourceInfo(item)
		if err != nil {
			return nil, err
		}
		mediaURL = pickVideoURL(resourceInfo)
		if item.SubtitleResID == "" {
			item.SubtitleResID = pickSubtitleResID(resourceInfo, item.Raw)
		}
	}
	if mediaURL == "" {
		return nil, nil
	}
	format := mediaFormat(mediaURL)
	headers := map[string]string{"Referer": referer, "User-Agent": USER_AGENT, "Cookie": x.cookie}
	streamKey := format
	quality := "best"
	if item.Kind == "file" {
		streamKey = "file"
		quality = "file"
	}
	entry := &extractor.MediaInfo{
		Site:  "hqwx",
		Title: firstNonEmpty(cleanName(item.Name), "hqwx_"+item.Kind),
		Streams: map[string]extractor.Stream{
			streamKey: {Quality: quality, URLs: []string{mediaURL}, Format: format, Headers: headers, NeedMerge: format == "m3u8"},
		},
		Extra: map[string]any{
			"course_type":     x.courseType,
			"goods_id":        x.goodsID,
			"product_id":      x.productID,
			"resource_id":     item.ResourceID,
			"playback_id":     item.PlaybackID,
			"subtitle_res_id": item.SubtitleResID,
			"kind":            item.Kind,
		},
	}
	if item.SubtitleResID != "" {
		entry.Extra["subtitle_res_id"] = item.SubtitleResID
	}
	return entry, nil
}

func (x *hqwxCtx) requestRaw(endpoint string, headers map[string]string) ([]byte, error) {
	if headers == nil {
		headers = x.headers
	}
	resp, err := x.c.Get(endpoint, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	return io.ReadAll(resp.Body)
}
