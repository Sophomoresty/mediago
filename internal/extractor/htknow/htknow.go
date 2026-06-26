// Package htknow implements source-aligned Htknow course extraction.
package htknow

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL = "https://learn.htknow.com"

	checkCookieURL       = "https://saas.clientapi.htknow.com/pc_view/learn/list"
	courseListURL        = "https://saas.clientapi.htknow.com/learn/list_v2"
	singleURL            = "https://saas.clientapi.htknow.com/course/single_detail"
	columnURL            = "https://saas.clientapi.htknow.com/course/column_course_detail"
	seriesURL            = "https://saas.clientapi.htknow.com/course/series_course_detail"
	liveInfoURL          = "https://saas.clientapi.htknow.com/live/live_wx/playback_list"
	columnInfoURL        = "https://saas.clientapi.htknow.com/course/column_course_list"
	seriesInfoURL        = "https://saas.clientapi.htknow.com/course/series_course_list"
	videoInfoURL         = "https://saas.clientapi.htknow.com/course/column_play_details"
	pcVideoInfoURL       = "https://saas.clientapi.htknow.com/pc_view/course/column_play_details"
	answerTagURL         = "https://saas.clientapi.htknow.com/pc_view/quest/get_quest_tag_list"
	answerNumURL         = "https://saas.clientapi.htknow.com/pc_view/quest/get_quest_num_list"
	answerListURL        = "https://saas.clientapi.htknow.com/pc_view/quest/get_quest_list"
	answerCreatePaperURL = "https://saas.clientapi.htknow.com/pc_view/quest/create_question_paper"
)

var patterns = []string{`(?:[\w-]+\.)?htknow\.com/`}

func init() {
	extractor.Register(&Htknow{}, extractor.SiteInfo{Name: "Htknow", URL: "htknow.com", NeedAuth: true})
}

type Htknow struct{}

func (s *Htknow) Patterns() []string { return patterns }

type htCtx struct {
	c           *util.Client
	headers     map[string]string
	cookies     map[string]string
	userID      string
	loginUserID string
	customID    string
	baseKey     string
	accounts    []string
}

type course struct{ id, mainProductID, typ, title, userID string }
type source struct {
	name       string
	url        string
	kind       string
	html       string
	answerHTML string
	extra      map[string]any
}

func (s *Htknow) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("htknow requires login cookies")
	}
	ctx, err := newCtx(opts.Cookies)
	if err != nil {
		return nil, err
	}
	cid := parseCourseID(rawURL)
	c, err := ctx.selectCourse(cid)
	if err != nil {
		return nil, err
	}
	ctx.userID = c.userID
	sources, err := ctx.sourcesForCourse(c)
	if err != nil {
		return nil, err
	}
	return mediaFromSources(c.title, sources)
}

func newCtx(jar http.CookieJar) (*htCtx, error) {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookies := cookieMap(jar, []string{refererURL, "https://saas.clientapi.htknow.com/"})
	token, customID, baseKey := cookies["token"], cookies["custom_id"], cookies["base_KEY"]
	userID := userIDFromCookie(cookies["user"])
	if token == "" || customID == "" || baseKey == "" || userID == "" {
		return nil, fmt.Errorf("htknow: missing token/custom_id/base_KEY/user cookie")
	}
	h := map[string]string{"authorization": "Bearer " + token, "referer": refererURL, "cookie": cookieHeader(cookies), "Content-Type": "application/json"}
	ctx := &htCtx{c: c, headers: h, cookies: cookies, userID: userID, loginUserID: userID, customID: customID, baseKey: baseKey}
	if err := ctx.checkCookie(); err != nil {
		return nil, err
	}
	ctx.accounts = accountIDs(cookies, userID)
	return ctx, nil
}

func (x *htCtx) checkCookie() error {
	payload := map[string]any{"product_version": "v1", "user_id": x.userID, "custom_id": x.customID, "version": "v1", "app_name": "pc_view"}
	root, err := x.postJSON(checkCookieURL, payload)
	if err != nil {
		return err
	}
	if intVal(root["code"]) != 200 {
		return fmt.Errorf("htknow cookie check failed: code=%v", root["code"])
	}
	return nil
}

func (x *htCtx) selectCourse(cid string) (course, error) {
	list, err := x.courseList()
	if err != nil {
		return course{}, err
	}
	if len(list) == 0 {
		return course{}, fmt.Errorf("htknow: empty course list")
	}
	if cid != "" {
		for _, c := range list {
			if c.id == cid {
				return c, nil
			}
		}
		return course{}, fmt.Errorf("htknow: course %s not found", cid)
	}
	return list[0], nil
}

func (x *htCtx) courseList() ([]course, error) {
	ids := x.accounts
	if len(ids) == 0 {
		ids = []string{x.userID}
	}
	seen := map[string]bool{}
	var out []course
	for _, uid := range ids {
		for page := 1; page < 99; page++ {
			payload := map[string]any{"user_id": uid, "custom_id": x.customID, "version": "v1", "page": page}
			root, err := x.postJSON(courseListURL, payload)
			if err != nil {
				return nil, err
			}
			items := listAt(root, "result")
			if len(items) == 0 {
				break
			}
			for _, it := range items {
				id := str(it["product_id"])
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				out = append(out, course{id: id, mainProductID: str(it["main_product_id"]), typ: str(it["type_desc"]), title: str(it["title"]), userID: uid})
			}
		}
	}
	return out, nil
}

func (x *htCtx) sourcesForCourse(c course) ([]source, error) {
	switch c.typ {
	case "直播课":
		return x.liveSources(c)
	case "视频", "音频", "图文":
		return x.singleSources(c)
	case "专栏":
		return x.columnSources(c)
	case "系列课":
		return x.seriesSources(c)
	default:
		return x.columnSources(c)
	}
}

func (x *htCtx) singleSources(c course) ([]source, error) {
	ptype := map[string]string{"视频": "4", "音频": "3", "图文": "1"}[c.typ]
	payload := basePayload(x.userID, x.customID, c.id)
	payload["product_version"] = "v1"
	payload["product_type"] = ptype
	root, err := x.postJSON(singleURL, payload)
	if err != nil {
		return nil, err
	}
	detail := mapAt(root, "result", "detail")
	url := x.videoURL(str(detail["product_token"]))
	if url == "" && str(detail["pay_content"]) == "" {
		return nil, fmt.Errorf("htknow: empty single detail")
	}
	return []source{{name: firstNonEmpty(c.title, c.id), url: url, kind: c.typ, html: str(detail["pay_content"])}}, nil
}

func (x *htCtx) liveSources(c course) ([]source, error) {
	payload := map[string]any{"user_id": x.userID, "custom_id": x.customID, "version": "v1", "app_name": "wx", "product_id": c.id}
	root, err := x.postJSON(liveInfoURL, payload)
	if err != nil {
		return nil, err
	}
	var out []source
	for i, it := range listAt(root, "result", "list") {
		if u := str(it["video_url"]); u != "" {
			out = append(out, source{name: fmt.Sprintf("[%d]--%s", i+1, trimMP4(str(it["title"]))), url: u, kind: "直播课"})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("htknow: empty live replay list")
	}
	return out, nil
}

func (x *htCtx) columnSources(c course) ([]source, error) {
	payload := basePayload(x.userID, x.customID, c.id)
	if c.mainProductID != "" {
		payload["main_product_id"] = c.mainProductID
	}
	root, err := x.postJSON(columnInfoURL, payload)
	if err != nil {
		return nil, err
	}
	var out []source
	for i, it := range listAt(root, "result", "list") {
		if src := x.sourceFromProduct(c, it, fmt.Sprintf("[%d]--%s", i+1, trimMP4(str(it["title"])))); src.hasContent() {
			out = append(out, src)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("htknow: empty column list")
	}
	return out, nil
}

func (x *htCtx) seriesSources(c course) ([]source, error) {
	payload := map[string]any{"user_id": x.userID, "custom_id": x.customID, "app_name": "wx", "version": "v1", "product_id": c.id}
	root, err := x.postJSON(seriesInfoURL, payload)
	if err != nil {
		return nil, err
	}
	var out []source
	for i, group := range listAt(root, "result", "list") {
		for j, it := range listAt(group, "article_list") {
			name := fmt.Sprintf("[%d.%d]--%s", i+1, j+1, trimMP4(str(it["title"])))
			if src := x.sourceFromProduct(c, it, name); src.hasContent() {
				out = append(out, src)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("htknow: empty series list")
	}
	return out, nil
}

func (x *htCtx) sourceFromProduct(c course, it map[string]any, name string) source {
	token := str(it["product_token"])
	htmlText := str(it["pay_content"])
	productID := firstNonEmpty(str(it["id"]), str(it["product_id"]))
	productType := str(it["product_type"])
	columnID := str(it["series_id"])
	if productType == "9" {
		return x.answerSource(c, columnID, productID, productType, name)
	}
	url := x.videoURL(token)
	if url == "" {
		fetchedURL, fetchedHTML := x.fetchProductURL(c, str(it["series_id"]), str(it["id"]), str(it["product_type"]))
		if fetchedURL != "" {
			url = fetchedURL
		}
		if fetchedHTML != "" {
			htmlText = fetchedHTML
		}
	}
	return source{name: name, url: url, kind: c.typ, html: htmlText}
}

func (s source) hasContent() bool {
	return strings.TrimSpace(s.url) != "" || strings.TrimSpace(s.html) != "" || strings.TrimSpace(s.answerHTML) != ""
}

func (x *htCtx) fetchProductURL(c course, columnID, productID, productType string) (string, string) {
	payload := basePayload(x.userID, x.customID, productID)
	payload["column_id"] = columnID
	payload["product_type"] = firstNonEmpty(productType, "4")
	payload["product_version"] = "v1"
	if c.typ == "系列课" {
		payload["big_series_id"] = c.id
	}
	if c.mainProductID != "" {
		payload["main_product_id"] = c.mainProductID
	}
	for _, endpoint := range []string{videoInfoURL, pcVideoInfoURL, videoInfoURL} {
		root, err := x.postJSON(endpoint, payload)
		if err != nil {
			continue
		}
		token := firstNonEmpty(strAt(root, "result", "article_detail", "product_token"), strAt(root, "result", "detail", "product_token"))
		htmlText := firstNonEmpty(strAt(root, "result", "article_detail", "pay_content"), strAt(root, "result", "detail", "pay_content"))
		if u := x.videoURL(token); u != "" || htmlText != "" {
			return u, htmlText
		}
		if x.loginUserID != "" && x.loginUserID != x.userID {
			payload["user_id"] = x.loginUserID
		}
	}
	return "", ""
}

func (x *htCtx) videoURL(productToken string) string {
	if productToken == "" || x.baseKey == "" {
		return ""
	}
	plainJSON, err := decodeB64(productToken)
	if err != nil {
		return ""
	}
	var raw map[string]string
	if err := json.Unmarshal(plainJSON, &raw); err != nil {
		return ""
	}
	cipherText, err := decodeB64(raw["value"])
	if err != nil {
		return ""
	}
	iv, err := decodeB64(raw["iv"])
	if err != nil {
		return ""
	}
	key, err := decodeB64(x.baseKey)
	if err != nil {
		return ""
	}
	block, err := aes.NewCipher(key)
	if err != nil || len(iv) != block.BlockSize() || len(cipherText)%block.BlockSize() != 0 {
		return ""
	}
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(cipherText, cipherText)
	text := strings.TrimSpace(string(pkcs7Unpad(cipherText)))
	if strings.HasPrefix(text, "http") {
		return text
	}
	return ""
}

func (x *htCtx) postJSON(endpoint string, payload map[string]any) (map[string]any, error) {
	b, _ := json.Marshal(payload)
	resp, err := x.c.Post(endpoint, bytes.NewReader(b), x.headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	return root, nil
}

func mediaFromSources(title string, srcs []source) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	mk := func(s source) *extractor.MediaInfo {
		extra := map[string]any{}
		for k, v := range s.extra {
			extra[k] = v
		}
		if s.html != "" {
			extra["html_content"] = s.html
		}
		if s.answerHTML != "" {
			extra["html_content"] = s.answerHTML
			extra["answer_html"] = true
			htmlURL := htmlDataURL(s.answerHTML)
			return &extractor.MediaInfo{Site: "htknow", Title: s.name, Streams: map[string]extractor.Stream{"document": {Quality: firstNonEmpty(s.kind, "答题HTML"), URLs: []string{htmlURL}, Format: "html", Headers: map[string]string{"Referer": refererURL}}}, Extra: extra}
		}
		if strings.TrimSpace(s.url) == "" {
			htmlURL := htmlDataURL(s.html)
			return &extractor.MediaInfo{Site: "htknow", Title: s.name, Streams: map[string]extractor.Stream{"document": {Quality: firstNonEmpty(s.kind, "html"), URLs: []string{htmlURL}, Format: "html", Headers: map[string]string{"Referer": refererURL}}}, Extra: extra}
		}
		format := strings.TrimPrefix(strings.ToLower(path.Ext(strings.Split(s.url, "?")[0])), ".")
		if format == "" {
			format = "mp4"
		}
		return &extractor.MediaInfo{Site: "htknow", Title: s.name, Streams: map[string]extractor.Stream{"default": {Quality: s.kind, URLs: []string{s.url}, Format: format, Headers: map[string]string{"Referer": refererURL}}}, Extra: extra}
	}
	for _, src := range srcs {
		if src.hasContent() {
			entries = append(entries, mk(src))
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("htknow: no playable video URL, html_content, or answer HTML")
	}
	if len(entries) == 1 {
		entries[0].Title = firstNonEmpty(entries[0].Title, title)
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "htknow", Title: title, Entries: entries}, nil
}
