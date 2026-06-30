// Package youzan implements an extractor for youzan.com knowledge shops.
package youzan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL        = "https://www.youzan.com"
	goodsURL          = "/wscvis/course/detail/goods.json"
	columnChaptersURL = "/wscvis/knowledge/getColumnChapters.json"
	columnContentsURL = "/wscvis/knowledge/contentAndLive.json"
	simpleURL         = "/wscvis/course/getSimple.json"
	liveLinkURL       = "/wscvis/knowledge/getLiveLink.json"
	eduLiveLinkURL    = "/wscvis/course/live/video/getEduLiveLink.json"
	roomURL           = "/wscvis/course/live/video/room"
	assetStateURL     = "/wscvis/course/detail/getAssetStateV2.json"
	tradeCreateURL    = "/wscvis/trade/create.json"
	getDetailURL      = "/wscvis/course/getDetail.json"
	memberCenterPath  = "/wscuser/membercenter"

	wechatMobileUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) " +
		"AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 " +
		"MicroMessenger/8.0.50(0x18003239) NetType/WIFI Language/zh_CN"
)

var (
	patterns     = []string{`(?:[\w-]+\.)?youzan\.com/`}
	mediaRe      = regexp.MustCompile(`(?i)https?://[^"'\s<>]+(?:\.m3u8|\.mp4|\.mp3|\.m4a|\.aac|\.wav)[^"'\s<>]*`)
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
	titleCleanRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
	whiteRe      = regexp.MustCompile(`\s+`)
)

func init() {
	extractor.Register(&Youzan{}, extractor.SiteInfo{Name: "Youzan", URL: "youzan.com", NeedAuth: true})
}

// Youzan is the extractor entry-point.
type Youzan struct{}

func (y *Youzan) Patterns() []string { return patterns }

// yzContext carries per-request state.
type yzContext struct {
	c           *util.Client
	shopHost    string
	shopBase    string
	kdtID       string
	alias       string
	columnAlias string
	page        string
	headers     map[string]string
	cookieStr   string
	colCache    map[string][]map[string]any // column items cache
}

type yzMedia struct {
	Title, URL, ContentType, UserAgent string
	Size                               int64
}

type yzDocument struct{ URL, Title, Type string }

type yzLesson struct {
	Alias     string
	Title     string
	Media     []yzMedia
	HTML      string
	Documents []yzDocument
}

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (y *Youzan) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("youzan requires login cookies")
	}
	ctx := &yzContext{c: util.NewClient(), colCache: map[string][]map[string]any{}}
	ctx.c.SetCookieJar(opts.Cookies)
	if err := ctx.configure(rawURL); err != nil {
		return nil, err
	}
	ctx.headers = ctx.buildHeaders(opts.Cookies, ctx.detailURL(ctx.alias))

	// Cookie validation
	if err := ctx.checkCookie(); err != nil {
		return nil, err
	}

	goods, err := ctx.getGoods(ctx.alias)
	if err != nil {
		return nil, err
	}
	data := safeMap(goods["data"])
	gd := safeMap(data["goodsData"])

	title := firstNonEmpty(resolveGoodsTitle(data), resolveTitle(goods), ctx.alias)
	price := resolvePrice(data)
	purchased := resolvePurchased(data, price)
	claimReq := resolveClaimRequired(data, price, purchased)

	goodsType := firstGoodsType(gd, safeMap(data["content"]), safeMap(data["column"]))

	// Auto-claim free course when needed
	if claimReq && price <= 0 && !purchased {
		if ctx.autoClaimFreeCourse(goods, goodsType) {
			purchased = true
		}
	}

	// Determine column alias for collection enumeration
	colAlias := ctx.columnAlias
	if colAlias == "" {
		colAlias = safeString(gd["columnAlias"])
	}
	items := ctx.getColumnItems(firstNonEmpty(colAlias, ctx.alias), goodsType)
	if len(items) > 0 {
		return ctx.buildCollectionInfo(title, colAlias, items, goodsType)
	}

	// Single item
	lesson := ctx.lessonFromDetail(ctx.alias, title)
	return ctx.lessonToInfo(lesson, title)
}

// ---------------------------------------------------------------------------
// URL configuration
// ---------------------------------------------------------------------------

func (x *yzContext) configure(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("youzan: invalid URL")
	}
	x.shopHost = u.Host
	x.shopBase = u.Scheme + "://" + u.Host
	q := u.Query()

	// Merge fragment query string
	if u.Fragment != "" {
		if idx := strings.Index(u.Fragment, "?"); idx >= 0 {
			if fq, e := url.ParseQuery(u.Fragment[idx+1:]); e == nil {
				for k, vs := range fq {
					if _, exists := q[k]; !exists {
						for _, v := range vs {
							q.Add(k, v)
						}
					}
				}
			}
		}
	}

	// Detect page type
	frag := u.Fragment
	switch {
	case containsAny(frag, "contentshow") || containsAny(u.Path, "contentshow"):
		x.page = "contentshow"
	case containsAny(frag, "columnshow") || containsAny(u.Path, "columnshow"):
		x.page = "columnshow"
	case containsAny(frag, "liveroom") || containsAny(u.Path, "liveroom"):
		x.page = "liveroom"
	case strings.Contains(u.Path, "/wscvis/course/detail/"):
		x.page = "detail"
	}

	x.alias = firstNonEmpty(q.Get("alias"), q.Get("courseAlias"), q.Get("goodsAlias"), q.Get("contentAlias"))
	x.columnAlias = firstNonEmpty(q.Get("columnAlias"), q.Get("fromColumn"))
	x.kdtID = firstNonEmpty(q.Get("kdt_id"), q.Get("kdtId"))

	// Extract alias from path segments
	if x.alias == "" && strings.Contains(u.Path, "/wscvis/course/detail/") {
		x.alias = strings.Trim(u.Path[strings.Index(u.Path, "/wscvis/course/detail/")+len("/wscvis/course/detail/"):], "/")
	}
	if x.alias == "" && strings.Contains(u.Path, "/wscvis/knowledge/") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		last := parts[len(parts)-1]
		if last != "knowledge" && last != "index" {
			x.alias = last
		}
	}
	if x.alias == "" {
		return fmt.Errorf("youzan: cannot parse alias from URL")
	}
	return nil
}

// ---------------------------------------------------------------------------
// URL builders
// ---------------------------------------------------------------------------

func (x *yzContext) apiURL(path string, params map[string]string) string {
	if strings.HasPrefix(path, "http") {
		return path
	}
	u, _ := url.Parse(strings.TrimRight(x.shopBase, "/") + path)
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (x *yzContext) detailURL(alias string) string {
	if x.shopBase == "" {
		return refererURL
	}
	p := map[string]string{}
	if x.kdtID != "" {
		p["kdt_id"] = x.kdtID
	}
	return x.apiURL("/wscvis/course/detail/"+alias, p)
}

func (x *yzContext) memberCenterURL() string {
	if x.shopHost == "" {
		return refererURL
	}
	p := map[string]string{}
	if x.kdtID != "" {
		p["kdt_id"] = x.kdtID
	}
	return x.apiURL(memberCenterPath, p)
}

func (x *yzContext) jsonParams(extra map[string]string) map[string]string {
	out := copyMap(extra)
	if x.kdtID != "" {
		if _, ok := out["kdtId"]; !ok {
			out["kdtId"] = x.kdtID
		}
	}
	return out
}

func (x *yzContext) legacyParams(extra map[string]string) map[string]string {
	out := copyMap(extra)
	if x.kdtID != "" {
		if _, ok := out["kdt_id"]; !ok {
			out["kdt_id"] = x.kdtID
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Headers
// ---------------------------------------------------------------------------

func (x *yzContext) buildHeaders(jar http.CookieJar, referer string) map[string]string {
	h := map[string]string{
		"accept":           "application/json, text/plain, */*",
		"referer":          referer,
		"x-requested-with": "XMLHttpRequest",
	}
	if x.shopBase != "" {
		h["origin"] = x.shopBase
	}
	var parts []string
	for _, raw := range []string{x.shopBase + "/", refererURL} {
		u, _ := url.Parse(raw)
		if u == nil {
			continue
		}
		for _, ck := range jar.Cookies(u) {
			parts = append(parts, ck.Name+"="+ck.Value)
			if ck.Name == "_kdt_id_" && x.kdtID == "" {
				x.kdtID = ck.Value
			}
		}
	}
	if x.kdtID != "" {
		parts = append(parts, "_kdt_id_="+x.kdtID)
	}
	if len(parts) > 0 {
		x.cookieStr = uniqueCookie(parts)
		h["cookie"] = x.cookieStr
	}
	return h
}

func (x *yzContext) hRef(ref string) map[string]string {
	h := copyMap(x.headers)
	if ref != "" {
		h["referer"] = ref
	}
	return h
}

func (x *yzContext) hRefUA(ref, ua string) map[string]string {
	h := x.hRef(ref)
	if ua != "" {
		h["User-Agent"] = ua
	}
	return h
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func (x *yzContext) requestText(path string, params map[string]string, ref string) (string, error) {
	return x.c.GetString(x.apiURL(path, params), x.hRef(ref))
}

func (x *yzContext) requestJSON(path string, params map[string]string, ref string) (map[string]any, error) {
	body, err := x.requestText(path, params, ref)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (x *yzContext) requestJSONUA(path string, params map[string]string, ref, ua string) map[string]any {
	body, err := x.c.GetString(x.apiURL(path, params), x.hRefUA(ref, ua))
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal([]byte(body), &out) != nil {
		return nil
	}
	return out
}

func (x *yzContext) postJSON(path string, data any, ref string) map[string]any {
	u := x.apiURL(path, nil)
	h := x.hRef(ref)
	h["content-type"] = "application/json; charset=utf-8"
	var buf []byte
	if data != nil {
		buf, _ = json.Marshal(data)
	} else {
		buf = []byte("{}")
	}
	resp, err := x.c.Post(u, bytes.NewReader(buf), h)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var out map[string]any
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return nil
	}
	return out
}

func (x *yzContext) requestVariants(path string, pList []map[string]string, ref string) map[string]any {
	var last map[string]any
	for _, p := range pList {
		d, err := x.requestJSON(path, p, ref)
		if err != nil {
			continue
		}
		if hasPayload(d) {
			return d
		}
		if last == nil && len(d) > 0 {
			last = d
		}
	}
	if last != nil {
		return last
	}
	return map[string]any{}
}

// ---------------------------------------------------------------------------
// Cookie check
// ---------------------------------------------------------------------------

func (x *yzContext) checkCookie() error {
	if x.cookieStr == "" {
		return nil
	}
	mcURL := x.memberCenterURL()
	resp, err := x.c.Get(mcURL, x.hRef(mcURL))
	if err != nil {
		return nil // non-fatal
	}
	defer resp.Body.Close()
	fin := ""
	if resp.Request != nil && resp.Request.URL != nil {
		fin = strings.ToLower(resp.Request.URL.String())
	}
	if strings.Contains(fin, "passport.youzan.com") || strings.Contains(fin, "login") {
		return fmt.Errorf("youzan: login cookie expired (redirected to login)")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Goods & asset state
// ---------------------------------------------------------------------------

func (x *yzContext) getGoods(alias string) (map[string]any, error) {
	if alias == "" {
		return nil, fmt.Errorf("youzan: no alias")
	}
	return x.requestJSON(goodsURL, x.jsonParams(map[string]string{"alias": alias}), x.detailURL(alias))
}

func (x *yzContext) getAssetState(alias, refAlias string) map[string]any {
	if alias == "" {
		alias = x.alias
	}
	if refAlias == "" {
		refAlias = alias
	}
	ts := fmt.Sprint(time.Now().UnixMilli())
	return x.requestVariants(assetStateURL, []map[string]string{
		x.legacyParams(map[string]string{"t_vis_get": ts, "aliasList": alias}),
		x.jsonParams(map[string]string{"t_vis_get": ts, "aliasList": alias}),
		{"t_vis_get": ts, "aliasList": alias},
	}, x.detailURL(refAlias))
}

func (x *yzContext) hasOwnedAsset(alias string) bool {
	if alias == "" {
		alias = x.alias
	}
	st := x.getAssetState(alias, "")
	for _, n := range walkMaps(st) {
		if v, ok := n["isOwnAsset"]; ok {
			if b, _ := v.(bool); b {
				return true
			}
			if f, ok := asFloat(v); ok && f != 0 {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Auto-claim free course  (POST /wscvis/trade/create.json)
// ---------------------------------------------------------------------------

func (x *yzContext) autoClaimFreeCourse(goods map[string]any, goodsType int) bool {
	if x.cookieStr == "" || x.alias == "" {
		return false
	}
	if x.hasOwnedAsset(x.alias) {
		return true
	}
	// Warm context
	_, _ = x.requestText("/wscvis/course/detail/"+x.alias, x.legacyParams(nil), x.detailURL(x.alias))

	data := safeMap(goods["data"])
	gd := safeMap(data["goodsData"])

	owlType := goodsType
	if owlType == 0 {
		owlType = 1
	}
	id := firstNonEmpty(safeString(gd["goodsId"]), safeString(gd["id"]), x.alias)
	skuID := firstNonEmpty(safeString(gd["collectionId"]), id)

	prod := map[string]any{"alias": x.alias, "id": id, "num": 1, "owlType": owlType}
	if skuID != "" && skuID != id {
		prod["skuId"] = skuID
	}

	btp, _ := json.Marshal(map[string]string{"platform": "h5", "biz": "wsc", "client": "pc", "dc_ps": "", "qr": ""})
	payload := map[string]any{
		"bizTracePoint":   string(btp),
		"umpInfo":         map[string]any{},
		"productInfoList": []any{prod},
	}
	res := x.postJSON(tradeCreateURL, payload, x.detailURL(x.alias))
	if code, ok := asFloat(res["code"]); ok && code == 0 {
		for i := 0; i < 6; i++ {
			if x.hasOwnedAsset(x.alias) {
				return true
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	return x.hasOwnedAsset(x.alias)
}

// ---------------------------------------------------------------------------
// Column enumeration  (columnChaptersURL + columnContentsURL)
// ---------------------------------------------------------------------------

func (x *yzContext) getColumnChapters(alias string) []any {
	if alias == "" {
		alias = x.alias
	}
	d, err := x.requestJSON(columnChaptersURL, x.jsonParams(map[string]string{"columnAlias": alias}), x.detailURL(alias))
	if err != nil {
		return nil
	}
	if items, ok := d["data"].([]any); ok {
		return items
	}
	return nil
}

func (x *yzContext) getColumnItems(alias string, goodsType int) []map[string]any {
	if alias == "" {
		return nil
	}
	if cached, ok := x.colCache[alias]; ok {
		return cached
	}
	var all []map[string]any
	for pg := 1; pg <= 100; pg++ {
		p := x.jsonParams(map[string]string{
			"columnAlias": alias,
			"pageNumber":  fmt.Sprint(pg),
			"sortType":    "0",
		})
		if goodsType > 0 {
			p["goodsType"] = fmt.Sprint(goodsType)
		}
		d, err := x.requestJSON(columnContentsURL, p, x.detailURL(alias))
		if err != nil {
			break
		}
		rd := safeMap(d["data"])
		var pageItems []any
		for _, k := range []string{"data", "content", "list", "items"} {
			if items, ok := rd[k].([]any); ok && len(items) > 0 {
				pageItems = items
				break
			}
		}
		if pageItems == nil {
			if items, ok := d["data"].([]any); ok {
				pageItems = items
			}
		}
		for _, it := range pageItems {
			if m, ok := it.(map[string]any); ok {
				all = append(all, m)
			}
		}
		if len(pageItems) == 0 {
			break
		}
		tp := 1
		if v, ok := asInt(safeMap(rd["pageable"])["totalPages"]); ok && v > 0 {
			tp = v
		} else if v, ok := asInt(rd["totalPages"]); ok && v > 0 {
			tp = v
		}
		if pg >= tp {
			break
		}
	}
	x.colCache[alias] = all
	return all
}

func (x *yzContext) chapterLookup(alias string) map[string]string {
	chs := x.getColumnChapters(alias)
	if len(chs) == 0 {
		return nil
	}
	lu := map[string]string{}
	for i, ch := range chs {
		m, ok := ch.(map[string]any)
		if !ok {
			continue
		}
		t := firstNonEmpty(safeString(m["name"]), safeString(m["title"]))
		id := firstNonEmpty(safeString(m["id"]), safeString(m["directoryId"]))
		if id != "" && t != "" {
			lu[id] = fmt.Sprintf("{%d}--%s", i+1, cleanTitle(t))
		}
	}
	return lu
}

// ---------------------------------------------------------------------------
// Media URL extraction
// ---------------------------------------------------------------------------

func (x *yzContext) buildMediaEntries(detail map[string]any, alias string) []yzMedia {
	seen := map[string]bool{}
	var out []yzMedia
	add := func(urls []string, ct, ua string) {
		for _, u := range urls {
			if u != "" && !seen[u] {
				seen[u] = true
				out = append(out, yzMedia{URL: u, ContentType: ct, UserAgent: ua})
			}
		}
	}
	add(extractMediaURLs(detail), "", "")
	add(x.wechatMobileURLs(alias), "", wechatMobileUA)
	add(x.assetMediaURLs(alias), "", "")
	add(x.liveMediaURLs(alias), "", "")
	add(x.pageMediaURLs(alias), "", "")

	// videoContentDTO with explicit size / content_type
	for _, n := range walkMaps(detail) {
		vcd := safeMap(n["videoContentDTO"])
		if len(vcd) == 0 {
			continue
		}
		u := safeString(vcd["url"])
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		var sz int64
		if f, ok := asFloat(vcd["videoWholeSize"]); ok {
			sz = int64(f)
		} else if f, ok := asFloat(vcd["size"]); ok {
			sz = int64(f)
		}
		out = append(out, yzMedia{URL: u, ContentType: strings.ToLower(safeString(vcd["content_type"])), Size: sz, UserAgent: safeString(vcd["user_agent"])})
	}
	return out
}

func (x *yzContext) wechatMobileURLs(alias string) []string {
	if alias == "" {
		alias = x.alias
	}
	if alias == "" {
		return nil
	}
	jp := x.jsonParams(map[string]string{"alias": alias})
	if x.columnAlias != "" {
		jp["fromColumn"] = x.columnAlias
	}
	lp := x.legacyParams(map[string]string{"alias": alias})
	seen := map[string]bool{}
	var out []string
	for _, path := range []string{goodsURL, getDetailURL} {
		for _, p := range []map[string]string{jp, lp} {
			d := x.requestJSONUA(path, p, x.detailURL(alias), wechatMobileUA)
			for _, u := range extractMediaURLs(d) {
				if !seen[u] {
					seen[u] = true
					out = append(out, u)
				}
			}
		}
	}
	return out
}

func (x *yzContext) assetMediaURLs(alias string) []string {
	checked := map[string]bool{}
	seen := map[string]bool{}
	var out []string
	for _, a := range []string{x.columnAlias, x.alias, alias} {
		a = strings.TrimSpace(a)
		if a == "" || checked[a] {
			continue
		}
		checked[a] = true
		for _, u := range extractMediaURLs(x.getAssetState(a, x.alias)) {
			if !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	return out
}

func (x *yzContext) liveMediaURLs(alias string) []string {
	if alias == "" {
		alias = x.alias
	}
	if alias == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, path := range []string{liveLinkURL, eduLiveLinkURL, simpleURL} {
		d, err := x.requestJSON(path, x.jsonParams(map[string]string{"alias": alias}), x.detailURL(alias))
		if err != nil {
			continue
		}
		for _, u := range extractMediaURLs(d) {
			if !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
		inner := safeMap(d["data"])
		for _, k := range []string{"link", "url"} {
			if v := safeString(inner[k]); v != "" && strings.HasPrefix(v, "http") && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	if text, err := x.requestText(roomURL, x.legacyParams(map[string]string{"alias": alias}), x.detailURL(alias)); err == nil {
		for _, m := range mediaRe.FindAllString(text, -1) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

func (x *yzContext) pageMediaURLs(alias string) []string {
	if alias == "" {
		alias = x.alias
	}
	if alias == "" {
		return nil
	}
	text, err := x.requestText("/wscvis/course/detail/"+alias, x.legacyParams(nil), x.detailURL(alias))
	if err != nil {
		return nil
	}
	return mediaRe.FindAllString(text, -1)
}

// ---------------------------------------------------------------------------
// HTML / document extraction
// ---------------------------------------------------------------------------

func extractHTMLContent(v any) string {
	for _, node := range iterValues(v) {
		m, ok := node.(map[string]any)
		if !ok {
			continue
		}
		for _, k := range []string{"content", "detail", "richText", "rich_text", "description", "introduce"} {
			s := strings.TrimSpace(safeString(m[k]))
			if s == "" || !strings.Contains(s, "<") || !strings.Contains(s, ">") {
				continue
			}
			plain := htmlTagRe.ReplaceAllString(s, "")
			if len(whiteRe.ReplaceAllString(plain, "")) >= 24 {
				return s
			}
		}
	}
	return ""
}

func extractDocuments(v any) []yzDocument {
	var docs []yzDocument
	seen := map[string]bool{}
	for _, node := range iterValues(v) {
		m, ok := node.(map[string]any)
		if !ok {
			continue
		}
		for _, lk := range []string{"documents", "docs"} {
			dl, ok := m[lk].([]any)
			if !ok {
				continue
			}
			for _, item := range dl {
				d, ok := item.(map[string]any)
				if !ok {
					continue
				}
				u := firstNonEmpty(safeString(d["url"]), safeString(d["fileUrl"]),
					safeString(d["docUrl"]), safeString(d["downloadUrl"]))
				if u == "" || seen[u] {
					continue
				}
				seen[u] = true
				t := firstNonEmpty(safeString(d["title"]), safeString(d["name"]), safeString(d["fileName"]))
				if t == "" {
					t = "courseware"
				}
				tp := strings.ToLower(firstNonEmpty(safeString(d["type"]), safeString(d["fileType"])))
				docs = append(docs, yzDocument{URL: u, Title: t, Type: tp})
			}
		}
	}
	return docs
}

// ---------------------------------------------------------------------------
// Lesson building
// ---------------------------------------------------------------------------

func (x *yzContext) lessonFromDetail(alias, fallback string) yzLesson {
	if alias == "" {
		alias = x.alias
	}
	goods, err := x.getGoods(alias)
	if err != nil {
		return yzLesson{Alias: alias, Title: fallback}
	}
	data := safeMap(goods["data"])
	t := firstNonEmpty(resolveGoodsTitle(data), resolveTitle(goods), fallback)
	return yzLesson{
		Alias:     alias,
		Title:     t,
		Media:     x.buildMediaEntries(goods, alias),
		HTML:      extractHTMLContent(goods),
		Documents: extractDocuments(goods),
	}
}

func (x *yzContext) lessonToInfo(l yzLesson, title string) (*extractor.MediaInfo, error) {
	var children []*extractor.MediaInfo
	ref := x.detailURL(l.Alias)

	for i, m := range l.Media {
		name := title
		if len(l.Media) > 1 {
			name = fmt.Sprintf("[%02d.%02d]--%s", 0, i+1, title)
		}
		hdrs := map[string]string{"Referer": ref}
		if m.UserAgent != "" {
			hdrs["User-Agent"] = m.UserAgent
		}
		children = append(children, &extractor.MediaInfo{
			Site:  "youzan",
			Title: cleanTitle(name),
			Streams: map[string]extractor.Stream{"default": {
				Quality: "source", URLs: []string{m.URL},
				Format: pickFormat(m.URL, m.ContentType),
				Size:   m.Size, Headers: hdrs,
			}},
		})
	}

	for _, doc := range l.Documents {
		dn := cleanTitle(fmt.Sprintf("(%s)--%s", title, firstNonEmpty(doc.Title, "courseware")))
		children = append(children, &extractor.MediaInfo{
			Site:  "youzan",
			Title: dn,
			Streams: map[string]extractor.Stream{"default": {
				Quality: "source", URLs: []string{doc.URL},
				Format: guessDocFormat(doc.URL), Headers: map[string]string{"Referer": ref},
			}},
		})
	}

	extra := map[string]any{}
	if l.HTML != "" {
		extra["html"] = l.HTML
		extra["html_blocked"] = "html-to-pdf conversion requires wkhtmltopdf; raw HTML preserved"
	}

	if len(children) == 0 {
		if l.HTML != "" {
			return &extractor.MediaInfo{Site: "youzan", Title: cleanTitle(title), Extra: extra}, nil
		}
		return nil, fmt.Errorf("youzan: no media URLs found for alias %s", l.Alias)
	}
	if len(children) == 1 {
		children[0].Title = cleanTitle(title)
		if len(extra) > 0 {
			children[0].Extra = extra
		}
		return children[0], nil
	}
	info := &extractor.MediaInfo{Site: "youzan", Title: cleanTitle(title), Entries: children}
	if len(extra) > 0 {
		info.Extra = extra
	}
	return info, nil
}

// ---------------------------------------------------------------------------
// Collection (column) building
// ---------------------------------------------------------------------------

func (x *yzContext) buildCollectionInfo(courseTitle, colAlias string, items []map[string]any, goodsType int) (*extractor.MediaInfo, error) {
	lu := x.chapterLookup(firstNonEmpty(colAlias, x.alias))
	var entries []*extractor.MediaInfo

	for i, item := range items {
		ia := firstNonEmpty(safeString(item["alias"]), safeString(item["redirectAlias"]), safeString(item["contentAlias"]))
		it := firstNonEmpty(safeString(item["title"]), safeString(item["name"]))
		if ia == "" {
			continue
		}
		prefix := ""
		did := firstNonEmpty(safeString(item["directoryId"]), safeString(item["directory_id"]))
		if did != "" && lu != nil {
			if ch, ok := lu[did]; ok {
				prefix = ch + "/"
			}
		}
		lesson := x.lessonFromDetail(ia, it)
		if len(lesson.Media) == 0 && lesson.HTML == "" && len(lesson.Documents) == 0 {
			continue
		}
		et := firstNonEmpty(lesson.Title, it, fmt.Sprintf("[%02d]", i+1))
		if prefix != "" {
			et = prefix + et
		}
		info, err := x.lessonToInfo(lesson, et)
		if err != nil {
			continue
		}
		entries = append(entries, info)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("youzan: no downloadable items in column %s", firstNonEmpty(colAlias, x.alias))
	}
	return &extractor.MediaInfo{Site: "youzan", Title: cleanTitle(courseTitle), Entries: entries}, nil
}

// ---------------------------------------------------------------------------
// Price / purchase resolution (grounded in source _resolve_price etc.)
// ---------------------------------------------------------------------------

func priceToYuan(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p == float64(int64(p)) {
		return p / 100
	}
	return p
}

func coercePrice(val any) (float64, bool) {
	if val == nil {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		if v < 0 {
			return 0, false
		}
		return priceToYuan(v), true
	case string:
		v = strings.TrimSpace(strings.NewReplacer(",", "", "￥", "", "¥", "", "元", "").Replace(v))
		if v == "" {
			return 0, false
		}
		if strings.EqualFold(v, "free") || v == "免费" {
			return 0, true
		}
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil && f >= 0 {
			return priceToYuan(f), true
		}
	}
	return 0, false
}

func priceFromKeys(node map[string]any, keys []string) *float64 {
	for _, k := range keys {
		if v, ok := node[k]; ok {
			if f, ok := coercePrice(v); ok {
				r := f
				return &r
			}
		}
	}
	return nil
}

func searchPrice(node map[string]any, keys []string) *float64 {
	for _, m := range walkMaps(node) {
		if p := priceFromKeys(m, keys); p != nil {
			return p
		}
	}
	return nil
}

var (
	pricePrimary   = []string{"price", "sellPrice", "sellingPrice", "salePrice", "discountPrice", "currentPrice", "finalPrice", "activityPrice", "goodsPrice", "payPrice"}
	priceSecondary = []string{"minPrice", "maxPrice", "originPrice", "originalPrice", "linePrice", "tagPrice"}
)

func resolvePrice(data map[string]any) float64 {
	gd := safeMap(data["goodsData"])
	content := safeMap(data["content"])
	column := safeMap(data["column"])
	for _, keys := range [][]string{pricePrimary, priceSecondary} {
		for _, n := range []map[string]any{data, gd, content, column} {
			if p := priceFromKeys(n, keys); p != nil {
				return *p
			}
		}
		for _, n := range []map[string]any{data, gd, content, column} {
			if p := searchPrice(n, keys); p != nil {
				return *p
			}
		}
	}
	return 0
}

func resolvePurchased(data map[string]any, price float64) bool {
	gd := safeMap(data["goodsData"])
	content := safeMap(data["content"])
	column := safeMap(data["column"])
	for _, n := range []map[string]any{data, gd, content, column} {
		if v, ok := n["isOwnAsset"]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
			if f, ok := asFloat(v); ok {
				return f != 0
			}
		}
	}
	for _, n := range []map[string]any{data, gd, content, column} {
		if v, ok := n["needOrder"]; ok {
			if b, ok := v.(bool); ok && b {
				return false
			}
		}
	}
	return price <= 0
}

func resolveClaimRequired(data map[string]any, price float64, purchased bool) bool {
	if price > 0 || purchased {
		return false
	}
	gd := safeMap(data["goodsData"])
	content := safeMap(data["content"])
	column := safeMap(data["column"])
	for _, n := range []map[string]any{data, gd, content, column} {
		if v, ok := n["needOrder"]; ok {
			if b, ok := v.(bool); ok && b {
				return true
			}
		}
	}
	return false
}

func resolveGoodsTitle(data map[string]any) string {
	gd := safeMap(data["goodsData"])
	return firstNonEmpty(safeString(data["title"]), safeString(gd["title"]),
		safeString(safeMap(data["content"])["title"]), safeString(safeMap(data["column"])["title"]))
}

func firstGoodsType(nodes ...map[string]any) int {
	for _, n := range nodes {
		if v, ok := asInt(n["goodsType"]); ok && v > 0 {
			return v
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Pure utilities
// ---------------------------------------------------------------------------

func resolveTitle(data map[string]any) string {
	for _, node := range walkMaps(data) {
		if t := firstString(node, "title", "name", "alias"); t != "" {
			return htmlTagRe.ReplaceAllString(t, "")
		}
	}
	return ""
}

func extractMediaURLs(v any) []string {
	seen := map[string]bool{}
	var out []string
	appendURL := func(raw string) {
		raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
		if raw == "" || !strings.HasPrefix(strings.ToLower(raw), "http") {
			return
		}
		low := strings.ToLower(raw)
		if !mediaRe.MatchString(raw) && !hasYouzanMediaHint(low) {
			return
		}
		if !seen[raw] {
			seen[raw] = true
			out = append(out, raw)
		}
	}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			text := strings.ReplaceAll(t, `\/`, `/`)
			for _, m := range mediaRe.FindAllString(text, -1) {
				appendURL(m)
			}
			appendURL(text)
		case []any:
			for _, it := range t {
				walk(it)
			}
		case map[string]any:
			for k, v := range t {
				kl := strings.ToLower(k)
				if strings.Contains(kl, "url") || strings.Contains(kl, "source") {
					walk(v)
				}
			}
			for _, v := range t {
				walk(v)
			}
		}
	}
	walk(v)
	return out
}

func hasYouzanMediaHint(low string) bool {
	for _, marker := range []string{
		".m3u8", ".mp4", ".mp3", ".m4a", ".aac", ".wav",
		"format=m3u8", "format=mp4", "type=m3u8", "type=mp4",
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}

func iterValues(v any) []any {
	var out []any
	var walk func(any)
	walk = func(x any) {
		out = append(out, x)
		switch t := x.(type) {
		case map[string]any:
			for _, val := range t {
				walk(val)
			}
		case []any:
			for _, val := range t {
				walk(val)
			}
		}
	}
	walk(v)
	return out
}

func walkMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
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
	return out
}

func hasPayload(d map[string]any) bool {
	if len(d) == 0 {
		return false
	}
	if v := d["data"]; v != nil {
		switch t := v.(type) {
		case string:
			return t != ""
		case []any:
			return len(t) > 0
		case map[string]any:
			return len(t) > 0
		default:
			return true
		}
	}
	for _, k := range []string{"goodsData", "content", "live", "list", "items", "url", "link", "videoSource"} {
		if v := d[k]; v != nil {
			switch t := v.(type) {
			case string:
				if t != "" {
					return true
				}
			case []any:
				if len(t) > 0 {
					return true
				}
			case map[string]any:
				if len(t) > 0 {
					return true
				}
			default:
				return true
			}
		}
	}
	return false
}

func uniqueCookie(parts []string) string {
	seen := map[string]string{}
	var order []string
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			if _, ok := seen[kv[0]]; !ok {
				order = append(order, kv[0])
			}
			seen[kv[0]] = kv[1]
		}
	}
	var out []string
	for _, k := range order {
		out = append(out, k+"="+seen[k])
	}
	return strings.Join(out, "; ")
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(fmt.Sprint(m[k])); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }

func pickFormat(u, ct string) string {
	low := strings.ToLower(u + " " + ct)
	switch {
	case strings.Contains(low, ".m3u8") || strings.Contains(low, "format=m3u8") || strings.Contains(low, "type=m3u8"):
		return "m3u8"
	case strings.Contains(low, ".mp3") || strings.Contains(low, "audio/mpeg"):
		return "mp3"
	case strings.Contains(low, ".m4a"):
		return "m4a"
	case strings.Contains(low, ".aac"):
		return "aac"
	case strings.Contains(low, ".wav"):
		return "wav"
	}
	return "mp4"
}

func guessDocFormat(u string) string {
	low := strings.ToLower(u)
	for _, ext := range []string{"pdf", "pptx", "ppt", "docx", "doc", "xlsx", "xls", "zip", "rar"} {
		if strings.Contains(low, "."+ext) {
			return ext
		}
	}
	return "bin"
}

func safeMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func safeString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	s := fmt.Sprint(v)
	if s == "<nil>" {
		return ""
	}
	return strings.TrimSpace(s)
}
func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f, true
		}
	case string:
		var f float64
		if _, err := fmt.Sscanf(t, "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}
func asInt(v any) (int, bool) {
	if f, ok := asFloat(v); ok {
		return int(f), true
	}
	return 0, false
}
func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func containsAny(s, sub string) bool { return strings.Contains(s, sub) }
