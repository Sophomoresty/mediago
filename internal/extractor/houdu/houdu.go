// Package houdu implements source-aligned Houdu course extraction.
package houdu

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	COURSE_TYPE_LIVE           = "1"
	COURSE_TYPE_RECORDED       = "2"
	COURSE_TYPE_AI_INTERACTIVE = "3"
	DEFAULT_HIDDEN_PRICE       = 999

	referer        = "https://s.houduweilai.com/"
	token_key      = "user-token"
	user_info_key  = "user-info"
	check_url      = "https://api.houduweilai.com/mini/student/othersStudents"
	api_url_format = "https://api.houduweilai.com%s"
	USER_AGENT     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	// Baijiayun VOD resolution via shared.BaijiayunResolveVOD — calls
	// https://www.baijiayun.com/vod/video/getPlayUrl with vid + token.
	baijiayunVODPath = "/vod/video/getPlayUrl"
)

var (
	patterns = []string{`\s*((?P<hd_login>https?://s\.houduweilai\.com/login(?:[/?#].*)?)|(?P<hd>https?://(?:[\w-]+\.)*houduweilai\.com(?:[/?#].*)?))`}

	PRICE_KEYS      = []string{"price", "sale_price", "salePrice", "real_price", "realPrice", "course_price", "coursePrice", "pay_price", "payPrice", "origin_price", "originPrice", "original_price", "originalPrice", "discount_price", "discountPrice", "market_price", "marketPrice"}
	FILE_LIST_KEYS  = []string{"file_list", "files", "material_list", "materials", "resource_list", "resources", "download_list", "downloads"}
	CHILD_LIST_KEYS = []string{"group_list", "chapter_list", "catalog_list", "module_list", "lesson_list", "lessons", "children", "child_list"}
)

func init() {
	extractor.Register(&Houdu{}, extractor.SiteInfo{Name: "Houdu", URL: "houduweilai.com", NeedAuth: true})
}

type Houdu struct{}

func (s *Houdu) Patterns() []string { return patterns }

type hdCtx struct {
	c       *util.Client
	headers map[string]string
	cookie  string
	token   string

	cid            string
	title          string
	courseType     string
	price          float64
	purchased      bool
	courseList     []hdCourse
	courseMap      map[string]hdCourse
	selectedCourse hdCourse
	detailCache    map[string]map[string]any
}

type hdCourse struct {
	CourseID, Title, CourseType string
	PackageList                 []string
	Price                       float64
	Purchased                   bool
	Raw                         map[string]any
}

type hdSource struct {
	Name, URL, Kind, Format string
	NeedMerge               bool
	Extra                   map[string]any
}

func (s *Houdu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("houdu requires login cookies")
	}
	x, err := newCtx(opts.Cookies)
	if err != nil {
		return nil, err
	}
	if err := x.prepare(rawURL); err != nil {
		return nil, err
	}
	sources, err := x.loadSources()
	if err != nil {
		return nil, err
	}
	return x.mediaFromSources(sources)
}

func newCtx(jar http.CookieJar) (*hdCtx, error) {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookie := cookieHeader(jar, []string{referer, "https://api.houduweilai.com/", "https://h5.houduweilai.com/"})
	cookies := parseCookieHeader(cookie)
	auth := firstNonEmpty(cookies["authorization"], cookies["Authorization"], cookies[token_key], cookies["token"], cookies["user_token"])
	if auth == "" {
		return nil, fmt.Errorf("houdu: missing authorization/user-token cookie")
	}
	if unescaped, err := url.QueryUnescape(auth); err == nil {
		auth = unescaped
	}
	headers := map[string]string{
		"x-service-name": "phoenix",
		"x-channel":      "HDSPC",
		"x-app-id":       "cdc8d9bd665a7a390e88b9a5ddcd23c1",
		"content-type":   "application/json",
		"Referer":        referer,
		"Origin":         "https://s.houduweilai.com",
		"Accept":         "application/json, text/plain, */*",
		"User-Agent":     USER_AGENT,
		"cookie":         cookie,
		"authorization":  auth,
		"Authorization":  auth,
	}
	return &hdCtx{c: c, headers: headers, cookie: cookie, token: auth, courseMap: map[string]hdCourse{}, detailCache: map[string]map[string]any{}, purchased: true}, nil
}

func (x *hdCtx) checkCookie() error {
	root, err := x.requestHoudu("/mini/student/othersStudents", map[string]any{}, "user")
	if err != nil {
		return err
	}
	if str(root["code"]) != "0" {
		return fmt.Errorf("houdu cookie check failed: code=%v", root["code"])
	}
	data := asMap(root["data"])
	if len(data) == 0 {
		return fmt.Errorf("houdu cookie check failed: empty data")
	}
	return nil
}

func (x *hdCtx) requestHoudu(path string, body map[string]any, serviceName string) (map[string]any, error) {
	if body == nil {
		body = map[string]any{}
	}
	traceID := genTraceID()
	headers := cloneHeaders(x.headers)
	headers["x-service-name"] = firstNonEmpty(serviceName, "phoenix")
	headers["x-token"] = genSign(body, traceID)
	headers["x-trace-id"] = traceID
	if x.token != "" {
		headers["Authorization"] = x.token
		headers["authorization"] = x.token
	}
	return x.postJSON(fmt.Sprintf(api_url_format, path), body, headers)
}

func (x *hdCtx) postJSON(endpoint string, body map[string]any, headers map[string]string) (map[string]any, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := x.c.Post(endpoint, bytes.NewReader(payload), headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	var root map[string]any
	if err := json.Unmarshal(buf, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", endpoint, err)
	}
	return root, nil
}

func genTraceID() string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	n := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(60466176)
	buf := make([]byte, 5)
	for i := 4; i >= 0; i-- {
		buf[i] = alphabet[n%36]
		n /= 36
	}
	return fmt.Sprintf("%d%s", time.Now().UnixMilli(), string(buf))
}

func genSign(body map[string]any, traceID string) string {
	keys := make([]string, 0, len(body))
	for k, v := range body {
		if v != nil {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := body[k]
		switch v.(type) {
		case map[string]any, []any, []map[string]any:
			b, _ := json.Marshal(v)
			parts = append(parts, k+"="+string(b))
		default:
			parts = append(parts, k+"="+str(v))
		}
	}
	raw := fmt.Sprintf("%s&secret=&H~QOhulgE@dg8+4mY@Uh9tYVb0(n&X6&traceId=%s", strings.Join(parts, "&"), traceID)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

var idRe = regexp.MustCompile(`(?i)(?:class[_-]?id|course[_-]?id|cid|id)=([0-9]+)|/(?:class|course|detail|learn)/(\d+)`)
