package xiaoetech

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/util"
)

var richtextIframeRe = regexp.MustCompile(`(?i)https?://iframe\.xiaoeknow\.com/page/\?[^"'<>\s]+`)

func compactMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		switch t := v.(type) {
		case nil:
			continue
		case string:
			if strings.TrimSpace(t) == "" {
				continue
			}
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func fetchEntitlement(c *util.Client, jar http.CookieJar, ctx xetCtx) map[string]any {
	if ctx.cid == "" {
		return nil
	}
	out := map[string]any{}
	if price := fetchXETPrice(c, jar, ctx); price > 0 {
		out["price"] = price
	}
	purchased, checked, typeName := fetchXETPurchased(c, jar, ctx)
	if checked {
		out["purchased"] = purchased
	}
	if typeName != "" {
		out["resource_type_name"] = typeName
	}
	if orderPrice, ok := fetchXETOrderPrice(c, jar, ctx); ok {
		out["purchased"] = true
		if cur, _ := out["price"].(float64); orderPrice > 0 && (cur == 0 || orderPrice < cur) {
			out["price"] = orderPrice
		}
	}
	return compactMap(out)
}

func fetchXETPrice(c *util.Client, jar http.CookieJar, ctx xetCtx) float64 {
	root := postXETFormRoot(c, jar, ctx, priceURL, pcPriceURL, map[string]string{"resource_id": ctx.cid})
	price := centsValue(valueByPath(root, "data", "price"))
	if activity := fetchXETActivityPrice(c, jar, ctx); activity > 0 && (price == 0 || activity < price) {
		price = activity
	}
	return price
}

func fetchXETActivityPrice(c *util.Client, jar http.CookieJar, ctx xetCtx) float64 {
	if ctx.appID == "" {
		return 0
	}
	root := postXETFormRoot(c, jar, ctx, activityPriceURL, "", map[string]string{"spu_id": ctx.cid})
	return centsValue(valueByPath(root, "data", "marketing_info", "price_params"))
}

func fetchXETPurchased(c *util.Client, jar http.CookieJar, ctx xetCtx) (bool, bool, string) {
	root := postXETFormRoot(c, jar, ctx, purchasedURL, pcPurchasedURL, map[string]string{"app_id": ctx.appID, "resource_id": ctx.cid})
	data, _ := valueByPath(root, "data").(map[string]any)
	if len(data) == 0 {
		purchased, ok := fetchXETNewPurchased(c, jar, ctx)
		return purchased, ok, ""
	}
	typeName := firstNonEmpty(anyText(data["resource_type_name"]), anyText(data["resourceTypeName"]))
	if hasAnyKey(data, "is_permission", "isPermission") {
		purchased := numericTruthy(firstNonEmpty(anyText(data["is_permission"]), anyText(data["isPermission"])))
		if !purchased {
			if fallback, ok := fetchXETNewPurchased(c, jar, ctx); ok {
				purchased = fallback
			}
		}
		return purchased, true, typeName
	}
	if hasAnyKey(data, "is_buy", "isBuy") {
		purchased := numericTruthy(firstNonEmpty(anyText(data["is_buy"]), anyText(data["isBuy"])))
		if !purchased {
			if fallback, ok := fetchXETNewPurchased(c, jar, ctx); ok {
				purchased = fallback
			}
		}
		return purchased, true, typeName
	}
	purchased, ok := fetchXETNewPurchased(c, jar, ctx)
	return purchased, ok, typeName
}

func fetchXETNewPurchased(c *util.Client, jar http.CookieJar, ctx xetCtx) (bool, bool) {
	root := postXETFormRoot(c, jar, ctx, newPurchasedURL, pcNewPurchasedURL, map[string]string{"resource_id": ctx.cid})
	if root == nil {
		return false, false
	}
	code := strings.TrimSpace(fmt.Sprint(root["code"]))
	return code == "0" || strings.EqualFold(code, "0.0"), true
}

func fetchXETOrderPrice(c *util.Client, jar http.CookieJar, ctx xetCtx) (float64, bool) {
	var root map[string]any
	if ctx.pc && ctx.domain != "" {
		api := fmt.Sprintf(pcOrderURL, ctx.domain)
		body, err := c.GetString(api, headers(jar, referer(ctx)))
		if err == nil {
			_ = json.Unmarshal([]byte(body), &root)
		}
	} else if ctx.appID != "" {
		api := fmt.Sprintf(orderURL, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault))
		body, err := c.PostForm(api, map[string]string{
			"bizData[purchase_name]": "",
			"bizData[state]":         "1",
			"bizData[page_size]":     "99",
			"bizData[page_index]":    "1",
		}, headers(jar, referer(ctx)))
		if err == nil {
			_ = json.Unmarshal([]byte(body), &root)
		}
	}
	if root == nil {
		return 0, false
	}
	orders := append(listUnder(root["data"], "order_list"), listUnder(root["data"], "list")...)
	if len(orders) == 0 {
		orders = mapsFromAny(root["data"])
	}
	for _, order := range orders {
		if !matchesXETResource(order, ctx.cid) {
			continue
		}
		price := centsValue(firstPresent(order, "price", "order_price", "orderPrice", "pay_price", "payPrice"))
		if price == 0 {
			price = floatValue(firstPresent(order, "current_price", "currentPrice", "real_price", "realPrice"))
		}
		return price, true
	}
	return 0, false
}

func postXETFormRoot(c *util.Client, jar http.CookieJar, ctx xetCtx, h5Tpl, pcTpl string, plainForm map[string]string) map[string]any {
	api := ""
	form := plainForm
	if ctx.pc && pcTpl != "" && ctx.domain != "" {
		api = fmt.Sprintf(pcTpl, ctx.domain)
	} else if ctx.appID != "" && h5Tpl != "" {
		api = fmt.Sprintf(h5Tpl, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault))
		form = wrapBizData(plainForm)
	}
	if api == "" {
		return nil
	}
	body, err := c.PostForm(api, form, headers(jar, referer(ctx)))
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return nil
	}
	return root
}

func supplementalItems(c *util.Client, jar http.CookieJar, ctx xetCtx, it xetItem) []xetItem {
	switch normType(firstNonEmpty(it.typ, ctx.typ)) {
	case "live":
		out := pptChildren(c, jar, ctx, it.id)
		out = append(out, liveTextRichtextChildren(c, jar, ctx, it)...)
		return uniqueItems(out)
	case "text":
		return richtextChildrenFromDetail(c, jar, ctx, it)
	default:
		return nil
	}
}

func pptChildren(c *util.Client, jar http.CookieJar, ctx xetCtx, videoID string) []xetItem {
	if ctx.appID == "" || videoID == "" {
		return nil
	}
	api := fmt.Sprintf(pptListURL, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault), videoID)
	body, err := c.GetString(api, headers(jar, referer(ctx)))
	if err != nil {
		return nil
	}
	var root any
	if json.Unmarshal([]byte(body), &root) != nil {
		return nil
	}
	var out []xetItem
	for i, m := range mapsUnder(root) {
		u := normalizeURL(firstNonEmpty(val(m, "current_image_url"), val(m, "image_url"), val(m, "url")))
		if u == "" || !strings.HasPrefix(u, "http") {
			continue
		}
		raw := copyMap(m)
		raw["url"] = u
		raw["_parent_id"] = videoID
		out = append(out, xetItem{
			id:     firstNonEmpty(val(m, "id"), fmt.Sprintf("%s_ppt_%d", videoID, i+1)),
			title:  cleanXETTitle(firstNonEmpty(val(m, "title"), fmt.Sprintf("课件_%d", i+1))),
			typ:    "file",
			appID:  ctx.appID,
			userID: ctx.userID,
			raw:    raw,
		})
	}
	return uniqueItems(out)
}

func richtextChildrenFromDetail(c *util.Client, jar http.CookieJar, ctx xetCtx, it xetItem) []xetItem {
	htmlText := firstNonEmpty(val(it.raw, "content"), val(it.raw, "detail"), val(it.raw, "html"), val(it.raw, "text"))
	if htmlText == "" {
		root := postXETFormRoot(c, jar, ctx, textURL, pcTextURL, map[string]string{"resource_id": it.id})
		htmlText = firstNonEmpty(
			deepText(root["data"], "content", "detail", "html", "rich_text", "richText"),
			fetchFirstTextURL(c, jar, ctx, root["data"]),
		)
	}
	out := []xetItem{}
	if htmlItem := richtextHTMLItem(ctx, it, htmlText); htmlItem.id != "" {
		out = append(out, htmlItem)
	}
	out = append(out, collectRichtextItems(c, jar, ctx, htmlText, it.title, it.id)...)
	return uniqueItems(out)
}

func richtextHTMLItem(ctx xetCtx, it xetItem, htmlText string) xetItem {
	if strings.TrimSpace(htmlText) == "" {
		return xetItem{}
	}
	raw := copyMap(it.raw)
	raw["url"] = "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(htmlText))
	raw["_parent_id"] = it.id
	raw["_source_type"] = "html_text"
	return xetItem{
		id:     firstNonEmpty(it.id, ctx.cid) + "_html",
		title:  cleanXETTitle(firstNonEmpty(it.title, ctx.title, it.id, "图文")),
		typ:    "file",
		appID:  firstNonEmpty(it.appID, ctx.appID),
		userID: firstNonEmpty(it.userID, ctx.userID),
		raw:    raw,
	}
}

func liveTextRichtextChildren(c *util.Client, jar http.CookieJar, ctx xetCtx, it xetItem) []xetItem {
	if ctx.appID == "" || it.id == "" {
		return nil
	}
	api := fmt.Sprintf(liveTextTabURL, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault), ctx.appID, it.id)
	body, err := c.GetString(api, headers(jar, referer(ctx)))
	if err != nil {
		return nil
	}
	var root any
	if json.Unmarshal([]byte(body), &root) != nil {
		return nil
	}
	var out []xetItem
	seen := map[string]bool{}
	for _, rawTextURL := range iterValuesByKey(root, "text_url") {
		textURL := normalizeURL(rawTextURL)
		if textURL == "" || seen[textURL] {
			continue
		}
		seen[textURL] = true
		htmlText, err := c.GetString(textURL, headers(jar, referer(ctx)))
		if err != nil {
			continue
		}
		out = append(out, collectRichtextItems(c, jar, ctx, htmlText, it.title, it.id)...)
	}
	return uniqueItems(out)
}

func fetchFirstTextURL(c *util.Client, jar http.CookieJar, ctx xetCtx, data any) string {
	for _, rawURL := range iterValuesByKey(data, "text_url") {
		u := normalizeURL(rawURL)
		if u == "" {
			continue
		}
		body, err := c.GetString(u, headers(jar, referer(ctx)))
		if err == nil && body != "" {
			return body
		}
	}
	return ""
}

func collectRichtextItems(c *util.Client, jar http.CookieJar, ctx xetCtx, htmlText, defaultTitle, sourceID string) []xetItem {
	if htmlText == "" {
		return nil
	}
	var out []xetItem
	videoIDs := extractRichtextFileIDs(htmlText, "3")
	if len(videoIDs) > 0 {
		info := fetchRichtextInfo(c, jar, ctx, htmlVideoURL, videoIDs)
		for i, id := range videoIDs {
			m := info[id]
			u := normalizeURL(firstNonEmpty(val(m, "video_url"), val(m, "file_url"), val(m, "url")))
			if u == "" || !isMediaURL(u) {
				continue
			}
			raw := copyMap(m)
			raw["url"] = u
			raw["_parent_id"] = sourceID
			title := cleanXETTitle(firstNonEmpty(val(m, "video_title"), val(m, "title"), val(m, "name"), defaultTitle, fmt.Sprintf("richtext_video_%d", i+1)))
			out = append(out, xetItem{id: firstNonEmpty(id, u), title: title, typ: "video", appID: ctx.appID, userID: ctx.userID, raw: raw})
		}
	}
	audioIDs := extractRichtextFileIDs(htmlText, "2")
	if len(audioIDs) > 0 {
		info := fetchRichtextInfo(c, jar, ctx, htmlAudioURL, audioIDs)
		for i, id := range audioIDs {
			m := info[id]
			u := normalizeURL(firstNonEmpty(val(m, "audio_url"), val(m, "file_url"), val(m, "url")))
			if u == "" || !isMediaURL(u) {
				continue
			}
			raw := copyMap(m)
			raw["url"] = u
			raw["_parent_id"] = sourceID
			title := cleanXETTitle(firstNonEmpty(val(m, "audio_title"), val(m, "title"), val(m, "name"), defaultTitle, fmt.Sprintf("richtext_audio_%d", i+1)))
			out = append(out, xetItem{id: firstNonEmpty(id, u), title: title, typ: "audio", appID: ctx.appID, userID: ctx.userID, raw: raw})
		}
	}
	return uniqueItems(out)
}

func extractRichtextFileIDs(htmlText, targetType string) []string {
	htmlText = html.UnescapeString(htmlText)
	ids := []string{}
	seen := map[string]bool{}
	for _, raw := range richtextIframeRe.FindAllString(htmlText, -1) {
		raw = normalizeURL(raw)
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		q := u.Query()
		id := firstNonEmpty(q.Get("id"), firstRegex(`(?i)(?:[?&]|&amp;)id=([-\w]+)`, raw))
		typ := firstNonEmpty(q.Get("type"), firstRegex(`(?i)(?:[?&]|&amp;)type=(\d+)`, raw))
		if id != "" && typ == targetType && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func fetchRichtextInfo(c *util.Client, jar http.CookieJar, ctx xetCtx, api string, ids []string) map[string]map[string]any {
	out := map[string]map[string]any{}
	if len(ids) == 0 {
		return out
	}
	for _, endpoint := range uniqueStrings([]string{api + "?app_id=" + url.QueryEscape(ctx.appID), api}) {
		payload, _ := json.Marshal(map[string]any{"id": ids})
		resp, err := c.Post(endpoint, strings.NewReader(string(payload)), jsonHeaders(jar, referer(ctx)))
		if err != nil {
			continue
		}
		var root map[string]any
		err = json.NewDecoder(resp.Body).Decode(&root)
		_ = resp.Body.Close()
		if err != nil {
			continue
		}
		data, _ := root["data"].(map[string]any)
		for key, value := range data {
			if m, ok := value.(map[string]any); ok {
				out[key] = m
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return out
}

func decodeXETVideoURLField(data any) string {
	for _, m := range mapsUnder(data) {
		for _, key := range []string{"video_urls", "videoUrls", "play_urls", "playUrls"} {
			if u := decodeXETVideoURLValue(m[key]); u != "" {
				return u
			}
		}
	}
	return ""
}

func decodeXETVideoURLValue(v any) string {
	if u := firstMediaURL(v); u != "" {
		return u
	}
	s := anyText(v)
	if s == "" {
		return ""
	}
	candidates := []string{s, html.UnescapeString(s), normalizeURL(s)}
	if unescaped, err := url.QueryUnescape(s); err == nil {
		candidates = append(candidates, unescaped)
	}
	if unquoted, err := strconv.Unquote(s); err == nil {
		candidates = append(candidates, unquoted)
	}
	for _, candidate := range candidates {
		if u := decodeXETVideoURLString(candidate); u != "" {
			return u
		}
	}
	return ""
}

func decodeXETVideoURLString(s string) string {
	s = strings.TrimSpace(normalizeURL(s))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "__ba") {
		if u := normalizeURL(decryptLookbackPrivateURL(s)); isMediaURL(u) {
			return u
		}
	}
	if u := firstURLInString(s); u != "" {
		return u
	}
	for _, raw := range decodeJSONCandidates(s) {
		if u := firstMediaURL(raw); u != "" {
			return u
		}
		if u := firstURLInString(fmt.Sprint(raw)); u != "" {
			return u
		}
	}
	return ""
}

func decodeJSONCandidates(s string) []any {
	var out []any
	tryJSON := func(raw string) {
		var v any
		if json.Unmarshal([]byte(raw), &v) == nil {
			out = append(out, v)
		}
	}
	tryJSON(s)
	for _, enc := range []struct {
		raw     bool
		urlSafe bool
	}{
		{raw: false, urlSafe: false},
		{raw: true, urlSafe: false},
		{raw: false, urlSafe: true},
		{raw: true, urlSafe: true},
	} {
		padded := s
		if !enc.raw {
			if pad := len(padded) % 4; pad != 0 {
				padded += strings.Repeat("=", 4-pad)
			}
		}
		var (
			b   []byte
			err error
		)
		switch {
		case enc.raw && enc.urlSafe:
			b, err = base64.RawURLEncoding.DecodeString(s)
		case enc.raw:
			b, err = base64.RawStdEncoding.DecodeString(s)
		case enc.urlSafe:
			b, err = base64.URLEncoding.DecodeString(padded)
		default:
			b, err = base64.StdEncoding.DecodeString(padded)
		}
		if err == nil && len(b) > 0 {
			tryJSON(string(b))
		}
	}
	return out
}

func iterValuesByKey(data any, targetKey string) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if value, ok := x[targetKey]; ok {
				if s := anyText(value); s != "" {
					out = append(out, s)
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(data)
	return out
}

func valueByPath(v any, path ...string) any {
	cur := v
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[key]
	}
	return cur
}

func firstPresent(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok && v != nil {
			return v
		}
	}
	return nil
}

func hasAnyKey(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func matchesXETResource(m map[string]any, id string) bool {
	if id == "" {
		return false
	}
	for _, key := range []string{"resource_id", "resourceId", "product_id", "productId", "goods_id", "goodsId", "id"} {
		if anyText(m[key]) == id {
			return true
		}
	}
	return false
}

func numericTruthy(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "yes"
}

func centsValue(v any) float64 {
	n := floatValue(v)
	if n <= 0 {
		return 0
	}
	return round2(n / 100)
}

func floatValue(v any) float64 {
	s := anyText(v)
	if s == "" {
		return 0
	}
	s = strings.NewReplacer(",", "", "¥", "", "￥", "", "元", "").Replace(s)
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func round2(v float64) float64 {
	n, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", v), 64)
	return n
}

func anyText(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func sortedMapsByStartAt(in []map[string]any) []map[string]any {
	out := append([]map[string]any{}, in...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := val(out[i], "start_at"), val(out[j], "start_at")
		if a == b {
			return i < j
		}
		return a < b
	})
	return out
}
