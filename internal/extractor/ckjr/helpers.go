package ckjr

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

func parseRoute(raw string) routeInfo {
	if m := routeRe.FindStringSubmatch(raw); m != nil {
		kind := routeKindFromPath(m[2], m[3])
		cfg := routeCfg[kind]
		cfg.RawURL = raw
		cfg.BaseURL = routeBaseURL(raw)
		cfg.Company = m[1]
		q, err := url.ParseQuery(m[4])
		if err != nil {
			q = url.Values{}
		}
		cfg.Query = valuesToMap(q)
		cfg.ID = firstNonEmpty(q.Get(cfg.IDKey), q.Get("courseId"), q.Get("liveId"), q.Get("extId"), q.Get("datumId"), q.Get("combosId"), q.Get("testId"))
		return cfg
	}
	kind := "video"
	cfg := routeCfg[kind]
	cfg.RawURL = raw
	cfg.BaseURL = routeBaseURL(raw)
	cfg.Query = extractRouteQuery(raw)
	if company := extractRouteCompany(raw); company != "" {
		cfg.Company = company
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "livepersonaldetail") {
		cfg = routeCfg["livePersonal"]
		cfg.RawURL = raw
		cfg.BaseURL = routeBaseURL(raw)
		cfg.Query = extractRouteQuery(raw)
		cfg.Company = extractRouteCompany(raw)
	} else if strings.Contains(lower, "voice") {
		cfg = routeCfg["voice"]
		cfg.RawURL = raw
		cfg.BaseURL = routeBaseURL(raw)
		cfg.Query = extractRouteQuery(raw)
		cfg.Company = extractRouteCompany(raw)
	} else if strings.Contains(lower, "imgtext") {
		cfg = routeCfg["imgText"]
		cfg.RawURL = raw
		cfg.BaseURL = routeBaseURL(raw)
		cfg.Query = extractRouteQuery(raw)
		cfg.Company = extractRouteCompany(raw)
	} else if strings.Contains(lower, "live") {
		cfg = routeCfg["live"]
		cfg.RawURL = raw
		cfg.BaseURL = routeBaseURL(raw)
		cfg.Query = extractRouteQuery(raw)
		cfg.Company = extractRouteCompany(raw)
	} else if strings.Contains(lower, "column") {
		cfg = routeCfg["column"]
		cfg.RawURL = raw
		cfg.BaseURL = routeBaseURL(raw)
		cfg.Query = extractRouteQuery(raw)
		cfg.Company = extractRouteCompany(raw)
	} else if strings.Contains(lower, "datum") {
		cfg = routeCfg["datum"]
		cfg.RawURL = raw
		cfg.BaseURL = routeBaseURL(raw)
		cfg.Query = extractRouteQuery(raw)
		cfg.Company = extractRouteCompany(raw)
	} else if strings.Contains(lower, "package") {
		cfg = routeCfg["package"]
		cfg.RawURL = raw
		cfg.BaseURL = routeBaseURL(raw)
		cfg.Query = extractRouteQuery(raw)
		cfg.Company = extractRouteCompany(raw)
	} else if strings.Contains(lower, "testpaper") {
		cfg = routeCfg["testPaper"]
		cfg.RawURL = raw
		cfg.BaseURL = routeBaseURL(raw)
		cfg.Query = extractRouteQuery(raw)
		cfg.Company = extractRouteCompany(raw)
	}
	u, err := url.Parse(raw)
	if err == nil && u != nil {
		q := u.Query()
		cfg.ID = firstNonEmpty(q.Get(cfg.IDKey), q.Get("courseId"), q.Get("liveId"), q.Get("extId"), q.Get("datumId"), q.Get("combosId"), q.Get("prodId"), q.Get("productId"), q.Get("id"))
	}
	if cfg.ID == "" {
		cfg.ID = extractFirst(idRe, raw)
	}
	return cfg
}

func routeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	base := strings.SplitN(raw, "#", 2)[0]
	if !strings.Contains(base, "://") {
		return ""
	}
	return base
}

func extractRouteCompany(raw string) string {
	if m := regexp.MustCompile(`(?i)/kpv2p/([\w-]+)`).FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func extractRouteQuery(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if i := strings.Index(raw, "#"); i >= 0 {
		frag := raw[i+1:]
		if j := strings.Index(frag, "?"); j >= 0 {
			q, err := url.ParseQuery(frag[j+1:])
			if err != nil {
				return nil
			}
			return valuesToMap(q)
		}
	}
	if u, err := url.Parse(raw); err == nil {
		return valuesToMap(u.Query())
	}
	return nil
}

func valuesToMap(values url.Values) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for k, vals := range values {
		if len(vals) > 0 && vals[0] != "" {
			out[k] = vals[0]
		}
	}
	return out
}

func resourceParams(r routeInfo) map[string]string {
	params := map[string]string{
		r.IDKey:         r.ID,
		"id":            r.ID,
		"courseId":      r.ID,
		"prodId":        r.ID,
		"productId":     r.ID,
		"prodType":      r.ProdType,
		"courseType":    r.CourseTyp,
		"type":          r.CourseTyp,
		"page":          "1",
		"pageNum":       "1",
		"pageSize":      "100",
		"limit":         "100",
		"size":          "100",
		"hasPermission": "1",
	}
	switch r.Kind {
	case "video", "voice", "imgText":
		params["ckFrom"] = "5"
		params["extId"] = "-1"
		params["isSkeleton"] = "1"
		params["sortInColumn"] = "asc"
	case "column":
		params["ckFrom"] = "9"
		params["cId"] = "-1"
		params["extId"] = r.ID
		params["columnDetailId"] = "0"
		params["columnDetailld"] = "0"
		params["columnPermission"] = "1"
		params["isCoursePage"] = "0"
		params["name"] = ""
		params["sort"] = "asc"
	case "datum":
		params["ckFrom"] = "8"
		params["datumId"] = r.ID
	case "live":
		params["ckFrom"] = "51"
		params["liveId"] = r.ID
	case "livePersonal":
		params["ckFrom"] = "180"
		params["liveId"] = r.ID
	case "package":
		params["ckFrom"] = "61"
		params["combosId"] = r.ID
	case "testPaper":
		params["ckFrom"] = "125"
		params["testId"] = r.ID
	}
	for k, v := range r.Query {
		if v != "" {
			params[k] = v
		}
	}
	params[r.IDKey] = r.ID
	return params
}

func qcloudAuth(node map[string]any) map[string]string {
	appID := textValue(node, "app_id", "appId", "appID", "appid", "app")
	fileID := textValue(node, "fileID", "fileId", "file_id", "fileid", "vodVideoId", "vod_video_id", "vid")
	psign := textValue(node, "psign", "pSign", "p_sign", "playAuth", "play_auth", "sign", "token")
	if appID == "" || fileID == "" || psign == "" {
		return nil
	}
	return map[string]string{"app_id": appID, "file_id": fileID, "psign": psign}
}

func directMediaURL(v any) string {
	return findMediaURL(v)
}

func findMediaURL(v any) string {
	switch x := v.(type) {
	case string:
		u := normalizeMediaText(x)
		if isMediaURL(u) {
			return u
		}
		if dec := ckjrDecryptURL(u); dec != "" {
			return dec
		}
	case map[string]any:
		for _, k := range []string{"playUrl", "playurl", "videoUrl", "video_url", "m3u8Url", "m3u8_url", "audioUrl", "audio_url", "downloadUrl", "fileUrl", "file_url", "url", "path", "src"} {
			if u := findMediaURL(x[k]); u != "" {
				return u
			}
		}
		for _, vv := range x {
			if u := findMediaURL(vv); u != "" {
				return u
			}
		}
	case []any:
		for _, vv := range x {
			if u := findMediaURL(vv); u != "" {
				return u
			}
		}
	}
	return ""
}

func walkMaps(v any) []map[string]any {
	var out []map[string]any
	switch x := v.(type) {
	case map[string]any:
		out = append(out, x)
		for _, vv := range x {
			out = append(out, walkMaps(vv)...)
		}
	case []any:
		for _, vv := range x {
			out = append(out, walkMaps(vv)...)
		}
	}
	return out
}

func ckjrHeaders(raw string) map[string]string {
	h := map[string]string{"Accept": "application/json, text/plain, */*", "User-Agent": ckjrUA, "Referer": raw, "X-From": ckjrFromApp, "X-Trace-Ch": "1"}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		h["Origin"] = u.Scheme + "://" + u.Host
	} else {
		h["Origin"] = url0
	}
	return h
}

func ckjrCookieHeader(jar http.CookieJar, rawURL string) string {
	if jar == nil {
		return ""
	}
	origins := []string{rawURL, url0, "https://ckjr001.com/", "https://www.ckjr001.com/"}
	if u, err := url.Parse(rawURL); err == nil && u.Scheme != "" && u.Host != "" {
		origins = append([]string{u.Scheme + "://" + u.Host + "/"}, origins...)
	}
	seen := map[string]bool{}
	var parts []string
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			continue
		}
		for _, cookie := range jar.Cookies(u) {
			if cookie.Name == "" || seen[cookie.Name] {
				continue
			}
			seen[cookie.Name] = true
			parts = append(parts, cookie.Name+"="+cookie.Value)
		}
	}
	return strings.Join(parts, "; ")
}

type ckjrAuthState struct {
	Token     string
	UserID    string
	CompanyID string
	StoreID   string
	AESKey    string
	AESIV     string
	Headers   map[string]string
}

func ckjrAuthFromCookies(jar http.CookieJar, rawURL string, route routeInfo) ckjrAuthState {
	state := ckjrAuthState{Headers: map[string]string{}}
	if jar == nil {
		return state
	}
	origins := []string{rawURL, route.BaseURL, url0, "https://ckjr001.com/", "https://www.ckjr001.com/"}
	if route.Company != "" {
		origins = append(origins, url0+"/kpv2p/"+route.Company+"/")
	}
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			continue
		}
		for _, cookie := range jar.Cookies(u) {
			ckjrMergeAuthCookie(&state, cookie.Name, cookie.Value)
		}
	}
	return state
}

func ckjrMergeAuthCookie(state *ckjrAuthState, name, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	lowerName := strings.ToLower(strings.TrimSpace(name))
	switch lowerName {
	case "token", "ckjr_token", "authorization", "auth", "access_token":
		if state.Token == "" {
			state.Token = strings.TrimPrefix(value, "Bearer ")
		}
	case "userid", "user_id", "uid":
		if state.UserID == "" {
			state.UserID = value
		}
	case "companyid", "company_id", "company":
		if state.CompanyID == "" {
			state.CompanyID = value
		}
	case "x-store-id", "x_store_id", "storeid", "store_id":
		if state.StoreID == "" {
			state.StoreID = value
		}
	case "aeskey", "aes_key":
		if state.AESKey == "" {
			state.AESKey = value
		}
	case "aesvi", "aesiv", "aes_iv":
		if state.AESIV == "" {
			state.AESIV = value
		}
	case "x-dmp", "x_dmp":
		if state.Headers["X-DMP"] == "" {
			state.Headers["X-DMP"] = value
		}
	}
	if strings.Contains(value, "{") {
		ckjrMergeAuthJSON(state, value)
		return
	}
	if decoded, err := url.QueryUnescape(value); err == nil && decoded != value && strings.Contains(decoded, "{") {
		ckjrMergeAuthJSON(state, decoded)
		return
	}
	if raw, err := base64.StdEncoding.DecodeString(value + strings.Repeat("=", (4-len(value)%4)%4)); err == nil && json.Valid(raw) {
		ckjrMergeAuthJSON(state, string(raw))
	}
}

func ckjrMergeAuthJSON(state *ckjrAuthState, text string) {
	var payload any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &payload); err != nil {
		return
	}
	for _, node := range walkMaps(payload) {
		state.Token = firstNonEmpty(state.Token, textValue(node, "token", "accessToken", "access_token", "authorization", "Authorization"))
		state.UserID = firstNonEmpty(state.UserID, textValue(node, "userId", "user_id", "uid", "u"))
		state.CompanyID = firstNonEmpty(state.CompanyID, textValue(node, "companyId", "company_id", "company", "c"))
		state.StoreID = firstNonEmpty(state.StoreID, textValue(node, "X-STORE-ID", "x-store-id", "x_store_id", "storeId", "store_id"))
		state.AESKey = firstNonEmpty(state.AESKey, textValue(node, "aesKey", "aes_key"))
		state.AESIV = firstNonEmpty(state.AESIV, textValue(node, "aesVi", "aesIV", "aes_iv"))
		for _, k := range []string{"authorization", "Authorization", "X-DMP", "x-dmp", "X-STORE-ID", "x-store-id"} {
			if v := textValue(node, k); v != "" {
				switch strings.ToLower(k) {
				case "authorization":
					state.Headers["Authorization"] = v
				case "x-dmp":
					state.Headers["X-DMP"] = v
				case "x-store-id":
					state.Headers["X-STORE-ID"] = v
				}
			}
		}
	}
}

func ckjrApplyAuth(headers map[string]string, route *routeInfo, auth ckjrAuthState) {
	for k, v := range auth.Headers {
		if strings.TrimSpace(v) != "" && headers[k] == "" {
			headers[k] = v
		}
	}
	if auth.Token != "" && headers["Authorization"] == "" {
		if strings.HasPrefix(strings.ToLower(auth.Token), "bearer ") {
			headers["Authorization"] = auth.Token
		} else {
			headers["Authorization"] = "Bearer " + auth.Token
		}
	}
	if auth.StoreID != "" && headers["X-STORE-ID"] == "" {
		headers["X-STORE-ID"] = auth.StoreID
	}
	if route != nil {
		if route.Company == "" && auth.CompanyID != "" {
			route.Company = auth.CompanyID
		}
		if route.Query == nil {
			route.Query = map[string]string{}
		}
		if auth.CompanyID != "" && route.Query["companyId"] == "" {
			route.Query["companyId"] = auth.CompanyID
		}
		if auth.UserID != "" && route.Query["userId"] == "" {
			route.Query["userId"] = auth.UserID
		}
	}
	if headers["X-DMP"] == "" && auth.UserID != "" && auth.CompanyID != "" {
		if routePath := ckjrRoutePath(route); routePath != "" {
			headers["X-DMP"] = fmt.Sprintf("u=%s&c=%s&url=%s&chl=gxh", auth.UserID, auth.CompanyID, url.QueryEscape(routePath))
		}
	}
}

func ckjrRoutePath(route *routeInfo) string {
	if route == nil {
		return ""
	}
	if route.RawURL != "" {
		if i := strings.Index(route.RawURL, "#"); i >= 0 {
			pathValue := strings.TrimPrefix(route.RawURL[i+1:], "/")
			if pathValue != "" {
				return "/" + pathValue
			}
		}
	}
	if route.ID == "" {
		return ""
	}
	switch route.Kind {
	case "column":
		return fmt.Sprintf("/homePage/column/columnDetail?cId=-1&ckFrom=9&extId=%s", route.ID)
	case "datum":
		return fmt.Sprintf("/homePage/datum/datumDetail?datumId=%s&ckFrom=8", route.ID)
	case "livePersonal":
		return fmt.Sprintf("/homePage/live/livePersonalDetail?liveId=%s&ckFrom=180", route.ID)
	case "live":
		return fmt.Sprintf("/homePage/live/liveDetail?liveId=%s&ckFrom=51", route.ID)
	case "package":
		return fmt.Sprintf("/homePage/package/packageDetail?combosId=%s", route.ID)
	case "testPaper":
		return fmt.Sprintf("/homePage/testPaper/testDetail?testId=%s&ckFrom=125", route.ID)
	case "voice":
		return fmt.Sprintf("/homePage/course/voice?courseId=%s&ckFrom=5&extId=-1", route.ID)
	case "imgText":
		return fmt.Sprintf("/homePage/course/imgText?courseId=%s&ckFrom=5&extId=-1", route.ID)
	default:
		return fmt.Sprintf("/homePage/course/video?courseId=%s&ckFrom=5&extId=-1", route.ID)
	}
}

func routeKindFromPath(path, courseKind string) string {
	if courseKind != "" {
		return courseKind
	}
	switch {
	case strings.Contains(path, "column"):
		return "column"
	case strings.Contains(path, "datum"):
		return "datum"
	case strings.Contains(path, "package"):
		return "package"
	case strings.Contains(path, "testPaper"):
		return "testPaper"
	case strings.Contains(path, "livePersonal"):
		return "livePersonal"
	case strings.Contains(path, "live"):
		return "live"
	}
	return "video"
}

func responseHasPayload(v any) bool {
	if m, ok := v.(map[string]any); ok {
		if code, ok := m["code"]; ok && fmt.Sprint(code) != "0" && fmt.Sprint(code) != "200" {
			return findMediaURL(v) != ""
		}
	}
	return len(walkMaps(v)) > 0
}

func dedupeEntries(in []*extractor.MediaInfo) []*extractor.MediaInfo {
	seen := map[string]bool{}
	var out []*extractor.MediaInfo
	for _, e := range in {
		if e == nil {
			continue
		}
		key := e.Title
		for _, s := range e.Streams {
			if len(s.URLs) > 0 {
				key = s.URLs[0]
				break
			}
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}

func normalizeMediaText(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, `\/`, "/")
	s = strings.ReplaceAll(s, `\u002F`, "/")
	s = strings.ReplaceAll(s, `\u002f`, "/")
	s = strings.ReplaceAll(s, `\u003A`, ":")
	s = strings.ReplaceAll(s, `\u003a`, ":")
	s = strings.ReplaceAll(s, `\u003D`, "=")
	s = strings.ReplaceAll(s, `\u003d`, "=")
	s = strings.ReplaceAll(s, `\u0026`, "&")
	s = strings.ReplaceAll(s, `\u003F`, "?")
	s = strings.ReplaceAll(s, `\u003f`, "?")
	if strings.HasPrefix(s, "//") {
		s = "https:" + s
	}
	if m := mediaRe.FindStringSubmatch(s); m != nil {
		s = m[0]
	}
	return strings.TrimRight(s, `"' )],;`)
}

func isMediaURL(s string) bool {
	lower := strings.ToLower(s)
	if strings.HasPrefix(strings.TrimSpace(lower), "#extm3u") {
		return true
	}
	if !strings.HasPrefix(lower, "http") {
		return false
	}
	for _, ext := range []string{".m3u8", ".mp4", ".m4v", ".mov", ".flv", ".mp3", ".m4a", ".aac", ".wav", ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".zip", ".rar", ".7z", ".txt", ".csv", ".jpeg", ".jpg", ".png", ".gif", ".webp"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return strings.Contains(lower, "mime_type=video_mp4") || strings.Contains(lower, "video/mp4") || strings.Contains(lower, "audio/mp4") || strings.Contains(lower, "audio/mpeg") || strings.Contains(lower, "application/pdf")
}

func pickFormat(mediaURL string) string {
	lower := strings.ToLower(mediaURL)
	switch {
	case strings.HasPrefix(lower, "data:text/html"):
		return "html"
	case strings.HasPrefix(lower, "data:application/vnd.apple.mpegurl"):
		return "m3u8"
	case strings.Contains(lower, ".m3u8"):
		return "m3u8"
	case strings.Contains(lower, ".mp3") || strings.Contains(lower, ".m4a") || strings.Contains(lower, ".aac") || strings.Contains(lower, ".wav"):
		return "audio"
	case strings.Contains(lower, ".pdf"), strings.Contains(lower, "application/pdf"):
		return "pdf"
	}
	if u, err := url.Parse(mediaURL); err == nil {
		ext := strings.TrimPrefix(strings.ToLower(path.Ext(u.Path)), ".")
		switch ext {
		case "doc", "docx", "ppt", "pptx", "xls", "xlsx", "zip", "rar", "7z", "txt", "csv", "jpeg", "jpg", "png", "gif", "webp":
			return ext
		}
	}
	return "mp4"
}

func pickCandidateFormat(cand mediaCandidate) string {
	if cand.Format != "" {
		return strings.TrimPrefix(strings.ToLower(cand.Format), ".")
	}
	format := pickFormat(cand.URL)
	if cand.Kind == "file" && format == "mp4" && !looksLikeVideoURL(cand.URL) {
		if ext := fileFormatFromURL(cand.URL); ext != "" {
			return ext
		}
		if ext := fileFormatFromKey(cand.SourceKey); ext != "" {
			return ext
		}
		return "bin"
	}
	return format
}

func mediaKindFromFormat(format string) string {
	switch strings.ToLower(format) {
	case "audio":
		return "audio"
	case "pdf", "doc", "docx", "ppt", "pptx", "xls", "xlsx", "zip", "rar", "7z", "txt", "csv", "jpeg", "jpg", "png", "gif", "webp", "html":
		return "file"
	default:
		return "video"
	}
}

func looksLikeVideoURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") || strings.Contains(lower, ".m4v") || strings.Contains(lower, ".mov") || strings.Contains(lower, ".flv") || strings.Contains(lower, "video/")
}

func fileFormatFromURL(raw string) string {
	u, err := url.Parse(raw)
	pathValue := raw
	if err == nil {
		pathValue = u.Path
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(pathValue)), ".")
	switch ext {
	case "pdf", "doc", "docx", "ppt", "pptx", "xls", "xlsx", "zip", "rar", "7z", "txt", "csv", "jpeg", "jpg", "png", "gif", "webp":
		return ext
	default:
		return ""
	}
}

func fileFormatFromKey(sourceKey string) string {
	key := regexp.MustCompile(`[^a-z0-9]`).ReplaceAllString(strings.ToLower(sourceKey), "")
	for _, ext := range []string{"jpeg", "jpg", "png", "gif", "webp", "pdf", "docx", "doc", "pptx", "ppt", "xlsx", "xls", "zip", "rar", "7z", "csv", "txt"} {
		if strings.Contains(key, ext) {
			return ext
		}
	}
	return ""
}

func textValue(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func extractFirst(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	for _, group := range m[1:] {
		if strings.TrimSpace(group) != "" {
			return strings.TrimSpace(group)
		}
	}
	return ""
}

var ckjrAESKey = []byte("ckjrTheKey!@##@!")
var ckjrAESIV = []byte("9NONwyJtHesysWpN")

func ckjrDecryptURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "http") || strings.HasPrefix(s, "#EXTM3U") {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "+")
	for len(s)%4 != 0 {
		s += "="
	}
	ct, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(ct) == 0 || len(ct)%16 != 0 {
		return ""
	}
	plain, err := util.AESDecryptCBC(ct, ckjrAESKey, ckjrAESIV)
	if err != nil {
		return ""
	}
	u := strings.TrimSpace(string(plain))
	if isMediaURL(u) {
		return u
	}
	return ""
}

// end of file
