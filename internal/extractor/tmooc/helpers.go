package tmooc

import (
	"fmt"
	"html"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func headersFromJar(j http.CookieJar) map[string]string {
	h := map[string]string{"Origin": "https://tts10.tmooc.cn", "Referer": referer, "Accept-Language": "zh-CN,zh;q=0.9", "Accept": "application/json, text/plain, */*", "User-Agent": USER_AGENT}
	var parts []string
	for _, host := range []string{home_url, "https://uc.tmooc.cn/", tts_home_url, "https://ttsservice.tmooc.cn/"} {
		u, _ := url.Parse(host)
		for _, ck := range j.Cookies(u) {
			parts = append(parts, ck.Name+"="+ck.Value)
			if strings.EqualFold(ck.Name, "authorization") || strings.EqualFold(ck.Name, "token") {
				h["authorization"] = ck.Value
			}
		}
	}
	h["cookie"] = strings.Join(parts, "; ")
	h["Cookie"] = h["cookie"]
	return h
}
func media(site, title, u string, extra map[string]any) *extractor.MediaInfo {
	format := pickFormat(u)
	return &extractor.MediaInfo{Site: site, Title: sanitize(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{u}, Format: format, NeedMerge: format == "m3u8", Headers: map[string]string{"Referer": referer, "User-Agent": USER_AGENT}}}, Extra: extra}
}

var attrRe = regexp.MustCompile(`(?is)([\w:-]+)\s*=\s*(?:"([^"]*?)"|'([^']*?)')`)

func parseAttrs(s string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(s, -1) {
		val := m[2]
		if val == "" {
			val = m[3]
		}
		out[strings.ToLower(m[1])] = html.UnescapeString(val)
	}
	return out
}
func extractList(v any) []map[string]any {
	if l, ok := v.([]any); ok {
		return maps(l)
	}
	m := unwrapMap(v)
	for _, k := range []string{"list", "records", "rows", "courseList", "vailidVersionList", "bigStageList"} {
		if l, ok := m[k].([]any); ok {
			return maps(l)
		}
	}
	return nil
}
func containsID(m map[string]any, id string) bool {
	for _, v := range collectIDs(m) {
		if v == id {
			return true
		}
	}
	return false
}
func firstID(m map[string]any) string {
	ids := collectIDs(m)
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}
func collectIDs(m map[string]any) []string {
	var out []string
	var walk func(map[string]any)
	walk = func(x map[string]any) {
		for _, k := range []string{"studentClassroomId", "studentClassId", "stuClassId", "id", "classId", "courseId", "versionId", "version_id", "courseVersionId", "validVersionId", "vailidVersionId"} {
			if v := textAt(x, k); v != "" {
				out = append(out, v)
			}
		}
		for _, k := range []string{"vailidVersion", "validVersion", "courseInfo", "courseVersionInfo", "orderInfo", "goodsInfo", "course"} {
			if mm, ok := x[k].(map[string]any); ok {
				walk(mm)
			}
		}
	}
	walk(m)
	return unique(out)
}
func extractCourseTitle(m map[string]any) string {
	for _, x := range append([]map[string]any{m}, nestedMaps(m)...) {
		if s := textAt(x, "courseVersion", "courseName", "name", "title", "versionName", "courseTitle"); s != "" {
			return sanitize(s)
		}
	}
	return ""
}
func nestedMaps(m map[string]any) []map[string]any {
	var out []map[string]any
	for _, k := range []string{"vailidVersion", "validVersion", "courseInfo", "courseVersionInfo", "orderInfo", "goodsInfo", "course"} {
		if mm, ok := m[k].(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}
func unwrapMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		if d, ok := m["data"].(map[string]any); ok {
			return d
		}
		return m
	}
	return map[string]any{}
}
func maps(in []any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, v := range in {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func findURL(v any) string {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range []string{"playUrl", "videoUrl", "url", "m3u8", "m3u8Url", "play_url"} {
			if u := textAt(t, k); strings.HasPrefix(u, "http") {
				return u
			}
		}
		for _, x := range t {
			if u := findURL(x); u != "" {
				return u
			}
		}
	case []any:
		for _, x := range t {
			if u := findURL(x); u != "" {
				return u
			}
		}
	}
	return ""
}
func findText(v any, keys ...string) string {
	switch t := v.(type) {
	case map[string]any:
		if s := textAt(t, keys...); s != "" {
			return s
		}
		for _, x := range t {
			if s := findText(x, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, x := range t {
			if s := findText(x, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}
func textAt(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "<nil>" {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}
func mergeHeaders(a, b map[string]string) map[string]string {
	out := clone(a)
	for k, v := range b {
		out[k] = v
	}
	return out
}
func clone(h map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[k] = v
	}
	return out
}
func cleanText(s string) string {
	return strings.TrimSpace(html.UnescapeString(regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(s, "")))
}
func joinInts(prefix []int, fallback int) string {
	if len(prefix) == 0 {
		return fmt.Sprint(fallback)
	}
	parts := make([]string, len(prefix))
	for i, v := range prefix {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ".")
}
func match1(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(html.UnescapeString(m[1]))
	}
	return ""
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func sanitize(s string) string {
	s = html.UnescapeString(strings.TrimSpace(s))
	return regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`).ReplaceAllString(s, "_")
}
func pickFormat(u string) string {
	p := strings.ToLower(strings.SplitN(strings.SplitN(u, "?", 2)[0], "#", 2)[0])
	if i := strings.LastIndex(p, "."); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return "mp4"
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func extractToken(v any) string {
	for _, key := range []string{"token", "Authorization", "authorization", "accessToken", "access_token"} {
		if s := findText(v, key); s != "" {
			return strings.TrimPrefix(strings.TrimSpace(s), "Bearer ")
		}
	}
	return ""
}

func setTokenHeaders(h map[string]string, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	h["Authorization"] = token
	h["authorization"] = token
}

func setStuClassHeaders(h map[string]string, stuClassID string) {
	stuClassID = strings.TrimSpace(stuClassID)
	if stuClassID == "" {
		return
	}
	h["X-Stu-Class-Id"] = stuClassID
	h["x-stu-class-id"] = stuClassID
}

func extractCoursePrice(m map[string]any) float64 {
	for _, x := range append([]map[string]any{m}, nestedMaps(m)...) {
		for _, key := range []string{"price", "coursePrice", "payPrice", "originalPrice", "discountPrice", "discountBuyPrice", "markedPrice"} {
			if price := normalizePrice(x[key]); price > 0 {
				return price
			}
		}
	}
	return 0
}

func extractWebCoursePrice(page string) float64 {
	for _, pat := range []string{
		`priceStr["']?\s*[:=]\s*["']?([0-9]+(?:\.[0-9]+)?)`,
		`discountBuyPrice["']?\s*[:=]\s*["']?([0-9]+(?:\.[0-9]+)?)`,
		`discountPrice["']?\s*[:=]\s*["']?([0-9]+(?:\.[0-9]+)?)`,
		`markedPrice["']?\s*[:=]\s*["']?([0-9]+(?:\.[0-9]+)?)`,
		`price["']?\s*[:=]\s*["']?([0-9]+(?:\.[0-9]+)?)`,
	} {
		if price := normalizePrice(match1(page, pat)); price > 0 {
			return price
		}
	}
	return 0
}

func normalizePrice(value any) float64 {
	if value == nil {
		return 0
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return 0
	}
	replacer := strings.NewReplacer(",", "", "¥", "", "￥", "", "元", "")
	text = strings.TrimSpace(replacer.Replace(text))
	price, err := strconv.ParseFloat(text, 64)
	if err != nil || price < 0 {
		return 0
	}
	if price >= 1000 && math.Trunc(price) == price {
		price /= 100
	}
	return math.Round(price*100) / 100
}

func extractCoursePurchased(m map[string]any) bool {
	for _, x := range append([]map[string]any{m}, nestedMaps(m)...) {
		for _, key := range []string{"purchased", "isBuy", "is_buy", "isPurchased", "currentUserBuy", "current_user_buy", "buyStatus", "orderStatus"} {
			if value, ok := x[key]; ok && value != nil {
				return coerceBool(value, true)
			}
		}
	}
	return true
}

func coerceBool(value any, def bool) bool {
	if value == nil {
		return def
	}
	if b, ok := value.(bool); ok {
		return b
	}
	text := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	switch text {
	case "", "<nil>", "none", "null":
		return def
	case "0", "false", "no", "n":
		return false
	case "1", "true", "yes", "y":
		return true
	default:
		return true
	}
}
