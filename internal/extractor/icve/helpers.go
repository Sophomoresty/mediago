package icve

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/util"
)

var (
	aiCIDRe          = regexp.MustCompile(`(?i)https?://ai\.icve\.com\.cn/.*?excellent.*?/([-\w]+)|https?://ai\.icve\.com\.cn/.*?course.*?/([-\w]+)`)
	spaceCollapseRe  = regexp.MustCompile(`\s+`)
	tagStripRe       = regexp.MustCompile(`(?is)<.*?>`)
	filenameBadChars = regexp.MustCompile(`[<>:"/\\|?*]+`)
)

const icveURLPDFPNGs = "https://zjy2.icve.com.cn/prod-api/spoc/oss/getUrlPngs?fileUrl=%s"

func modeFromQuality(q string) int {
	switch normalizeQuality(q) {
	case "2", "sd", "标清", "480p", "360p":
		return IS_SD
	case "3", "pdf", "onlypdf", "only_pdf", "material", "courseware", "课件", "资料", "素材":
		return ONLY_PDF
	default:
		return IS_HD
	}
}

func normalizeQuality(q string) string {
	q = strings.TrimSpace(strings.ToLower(q))
	q = strings.NewReplacer("-", "", "_", "", " ", "").Replace(q)
	return q
}

func parseCID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if m := aiCIDRe.FindStringSubmatch(raw); len(m) == 3 {
		if strings.TrimSpace(m[1]) != "" {
			return strings.TrimSpace(m[1])
		}
		if strings.TrimSpace(m[2]) != "" {
			return strings.TrimSpace(m[2])
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, key := range []string{"courseId", "course_id", "cid", "id"} {
		if v := strings.TrimSpace(u.Query().Get(key)); v != "" {
			return v
		}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return ""
}

func cookieHeader(jar http.CookieJar, origins []string) string {
	if jar == nil {
		return ""
	}
	seen := map[string]bool{}
	var parts []string
	for _, raw := range origins {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		for _, c := range jar.Cookies(u) {
			if c.Name == "" {
				continue
			}
			key := c.Name + "=" + c.Value
			if seen[key] {
				continue
			}
			seen[key] = true
			parts = append(parts, key)
		}
	}
	return strings.Join(parts, "; ")
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case uint:
		return strconv.FormatUint(uint64(t), 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func cleanTitle(s string) string {
	s = html.UnescapeString(strings.TrimSpace(s))
	s = tagStripRe.ReplaceAllString(s, " ")
	s = filenameBadChars.ReplaceAllString(s, " ")
	s = spaceCollapseRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func listAt(m map[string]any, key string) []map[string]any {
	if m == nil {
		return nil
	}
	return mapsFromAny(m[key])
}

func mapsFromAny(v any) []map[string]any {
	switch t := v.(type) {
	case []map[string]any:
		return t
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func mapAt(m map[string]any, key string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	if sub, ok := m[key].(map[string]any); ok {
		return sub
	}
	return map[string]any{}
}

func sortBySort(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		return intVal(items[i]["sort"]) < intVal(items[j]["sort"])
	})
}

func intVal(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int8:
		return int(t)
	case int16:
		return int(t)
	case int32:
		return int(t)
	case int64:
		return int(t)
	case uint:
		return int(t)
	case uint32:
		return int(t)
	case uint64:
		return int(t)
	case float32:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(t))
		return i
	default:
		i, _ := strconv.Atoi(str(t))
		return i
	}
}

func parseJSONMap(text string) map[string]any {
	text = strings.TrimSpace(text)
	if text == "" {
		return map[string]any{}
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return map[string]any{}
	}
	return out
}

func pickExt(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if ext := strings.TrimPrefix(strings.ToLower(path.Ext(u.Path)), "."); ext != "" {
			return ext
		}
	}
	raw = strings.Split(raw, "?")[0]
	return strings.TrimPrefix(strings.ToLower(path.Ext(raw)), ".")
}

func filterOtherQualities(order []string, selected string) []string {
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return append([]string{}, order...)
	}
	out := make([]string, 0, len(order))
	for _, q := range order {
		if q != selected {
			out = append(out, q)
		}
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

func cloneHeaders(h map[string]string) map[string]string {
	if h == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out
}

func icveCookieOrigins(primary ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(primary)+10)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		out = append(out, raw)
	}
	for _, raw := range primary {
		add(raw)
	}
	for _, raw := range []string{
		referer + "/",
		"https://www.icve.com.cn/",
		"https://sso.icve.com.cn/",
		"https://user.icve.com.cn/",
		"https://mooc-old.icve.com.cn/",
		"https://course.icve.com.cn/",
		"https://zjy2.icve.com.cn/",
		"https://zyk.icve.com.cn/",
		"https://upload.icve.com.cn/",
		"https://qun.icve.com.cn/",
	} {
		add(raw)
	}
	return out
}

func ensureICVEBearerAuth(c *util.Client, headers map[string]string, passLoginURL, checkLoginURL string) string {
	if c == nil || headers == nil {
		return ""
	}
	cookies := parseRawCookieHeader(headers["cookie"])
	access := strings.TrimSpace(cookies["Token"])
	if access != "" {
		auth := "Bearer " + access
		if strings.TrimSpace(headers["Authorization"]) == "" && strings.TrimSpace(headers["authorization"]) == "" {
			headers["Authorization"] = auth
		}
		return access
	}
	token := strings.TrimSpace(cookies["token"])
	if token == "" || passLoginURL == "" {
		return ""
	}
	body, err := c.GetString(fmt.Sprintf(passLoginURL, url.QueryEscape(token)), headers)
	if err != nil {
		return ""
	}
	access = str(mapAt(parseJSONMap(body), "data")["access_token"])
	if access == "" {
		return ""
	}
	auth := "Bearer " + access
	withTokenCookie := mergeCookieHeaders(headers["cookie"], "Token="+access)
	checkHeaders := cloneHeaders(headers)
	checkHeaders["cookie"] = withTokenCookie
	checkHeaders["Authorization"] = auth
	checkHeaders["authorization"] = auth
	if checkLoginURL != "" {
		checkBody, err := c.GetString(checkLoginURL, checkHeaders)
		if err != nil || !courseOKCodeRe.MatchString(checkBody) {
			return ""
		}
	}
	headers["cookie"] = withTokenCookie
	headers["Authorization"] = auth
	return access
}

func collectAIItems(list []map[string]any, prefix []int) []aiItem {
	if len(list) == 0 {
		return nil
	}
	items := make([]map[string]any, len(list))
	copy(items, list)
	sortBySort(items)

	var out []aiItem
	videoCounter := 1
	fileCounter := 1
	for idx, node := range items {
		if node == nil {
			continue
		}
		pos := idx + 1
		nextPrefix := append(append([]int{}, prefix...), pos)
		if children := childList(node); len(children) > 0 {
			out = append(out, collectAIItems(children, nextPrefix)...)
		}
		kind := strings.ToLower(firstNonEmpty(str(node["fileType"]), str(node["file_type"])))
		rawInfo := fileInfoText(node["fileUrl"], node["file_url"], node["fileInfo"], node["file_info"])
		name := cleanTitle(str(node["name"]))
		switch kind {
		case "mp4", "video", "flv", "mpg", "avi", "mov":
			if rawInfo == "" {
				continue
			}
			idxs := append(append([]int{}, prefix...), videoCounter)
			videoCounter++
			out = append(out, aiItem{
				Name: fmt.Sprintf("[%s]--%s", joinInts(idxs, "."), trimRStripMP4(name)),
				Info: rawInfo,
				Kind: "video",
				Ext:  pickExt(rawInfo),
			})
		default:
			if rawInfo == "" || kind == "" {
				continue
			}
			idxs := append(append([]int{}, prefix...), fileCounter)
			fileCounter++
			out = append(out, aiItem{
				Name: fmt.Sprintf("(%s)--%s", joinInts(idxs, "."), name),
				Info: rawInfo,
				Kind: "file",
				Ext:  pickExt(rawInfo),
			})
		}
	}
	return out
}

func childList(node map[string]any) []map[string]any {
	for _, childKey := range []string{"children", "child"} {
		if children := listAt(node, childKey); len(children) > 0 {
			return children
		}
	}
	return nil
}

func fileInfoText(values ...any) string {
	for _, v := range values {
		if s := jsonText(v); s != "" {
			return s
		}
	}
	return ""
}

func jsonText(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []byte:
		return strings.TrimSpace(string(t))
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return str(t)
		}
		return strings.TrimSpace(string(b))
	}
}

func trimRStripMP4(s string) string {
	return trimTrailingChars(s, ".mp4")
}

func trimTrailingChars(s, cutset string) string {
	return strings.TrimRight(s, cutset)
}

func joinInts(xs []int, sep string) string {
	if len(xs) == 0 {
		return ""
	}
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return strings.Join(parts, sep)
}

func readBody(resp *http.Response) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("nil response")
	}
	return io.ReadAll(resp.Body)
}

// regexExtract extracts the first capturing group match from text.
func regexExtract(pattern, text string) string {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(text)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func dedupeAIItems(items []aiItem) []aiItem {
	seen := map[string]bool{}
	out := make([]aiItem, 0, len(items))
	for _, item := range items {
		key := item.Kind + "|" + item.Name + "|" + item.Info
		if item.Info == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func normalizeICVEFileType(ft string) string {
	ft = strings.TrimSpace(strings.ToLower(ft))
	ft = strings.TrimPrefix(ft, ".")
	return strings.TrimRight(ft, "x")
}

func parseICVEResourcePayload(text string) map[string]any {
	text = strings.TrimSpace(text)
	if text == "" {
		return map[string]any{}
	}
	var value any
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return map[string]any{}
	}
	for {
		switch v := value.(type) {
		case map[string]any:
			return v
		case []any:
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					return m
				}
			}
			return map[string]any{}
		case string:
			s := strings.TrimSpace(v)
			if s == "" || (!strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[")) {
				return map[string]any{}
			}
			value = nil
			dec := json.NewDecoder(strings.NewReader(s))
			dec.UseNumber()
			if err := dec.Decode(&value); err != nil {
				return map[string]any{}
			}
		default:
			return map[string]any{}
		}
	}
}

func firstNonEmptyMap(m map[string]any, keys ...string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	for _, key := range keys {
		switch v := m[key].(type) {
		case map[string]any:
			if len(v) > 0 {
				return v
			}
		case string:
			if parsed := parseICVEResourcePayload(v); len(parsed) > 0 {
				return parsed
			}
		}
	}
	return map[string]any{}
}

func resourceMapsFromAny(v any) []map[string]any {
	switch t := v.(type) {
	case []map[string]any:
		return t
	case []any:
		return mapsFromAny(t)
	case map[string]any:
		if len(t) > 0 {
			return []map[string]any{t}
		}
	case string:
		text := strings.TrimSpace(t)
		if text == "" {
			return nil
		}
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		var value any
		if err := dec.Decode(&value); err != nil {
			return nil
		}
		return resourceMapsFromAny(value)
	}
	return nil
}

func extractVOSResource(payload map[string]any) []map[string]any {
	if payload == nil {
		return nil
	}
	for _, key := range []string{
		"courseDesignCellResourceVos",
		"courseDesignCellResourceVo",
		"cellResourceVos",
		"vosResources",
		"vosResource",
		"resources",
		"resourceList",
		"fileList",
		"files",
	} {
		if rows := resourceMapsFromAny(payload[key]); len(rows) > 0 {
			return rows
		}
	}
	return nil
}

func mergeICVEContainerPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	merged := make(map[string]any, len(payload)+8)
	for k, v := range payload {
		merged[k] = v
	}
	for _, key := range []string{"data", "resource", "file", "content", "cloudFileInfo", "fileUrl", "file_url", "fileInfo", "file_info"} {
		if nested := firstNonEmptyMap(merged, key); len(nested) > 0 {
			for k, v := range nested {
				if _, exists := merged[k]; !exists {
					merged[k] = v
				}
			}
		}
	}
	return merged
}

type icveResolvedResource struct {
	Name     string
	URL      string
	Ext      string
	Kind     string
	FileType string
}

func resolveICVEResourceMediaList(c *util.Client, headers map[string]string, mode int, payload map[string]any, fileType, baseName string) []icveResolvedResource {
	return resolveICVEResourceMediaListDepth(c, headers, mode, payload, fileType, baseName, 0)
}

func resolveICVEResourceMediaListDepth(c *util.Client, headers map[string]string, mode int, payload map[string]any, fileType, baseName string, depth int) []icveResolvedResource {
	if payload == nil || depth > 5 {
		return nil
	}
	merged := mergeICVEContainerPayload(payload)
	fileType = normalizeICVEFileType(firstNonEmpty(
		fileType,
		str(merged["fileType"]),
		str(merged["file_type"]),
		str(merged["type"]),
		str(merged["suffix"]),
		str(merged["fileSuffix"]),
	))

	if vos := extractVOSResource(merged); len(vos) > 0 {
		var out []icveResolvedResource
		for _, item := range vos {
			child := mergeICVEResourcePayload(item, item["fileUrl"])
			childFileType := firstNonEmpty(
				str(child["fileType"]),
				str(child["file_type"]),
				str(child["type"]),
				str(child["suffix"]),
				str(child["fileSuffix"]),
				fileType,
			)
			for _, key := range []string{"fileType", "file_type", "type", "suffix", "fileSuffix"} {
				if _, exists := child[key]; !exists && merged[key] != nil {
					child[key] = merged[key]
				}
			}
			childName := cleanTitle(firstNonEmpty(str(item["name"]), str(item["title"]), str(item["fileName"])))
			name := baseName
			if childName != "" {
				if name != "" {
					name = name + "——" + childName
				} else {
					name = childName
				}
			}
			out = append(out, resolveICVEResourceMediaListDepth(c, headers, mode, child, childFileType, name, depth+1)...)
		}
		if len(out) > 0 {
			return dedupeICVEResolvedResources(out)
		}
	}

	u, ext, kind := resolveICVEResourceMediaDepth(c, headers, mode, merged, fileType, depth+1)
	if u == "" {
		if pages := resolveICVEPDFPageResources(c, headers, mode, merged, fileType, baseName); len(pages) > 0 {
			return pages
		}
		return nil
	}
	return []icveResolvedResource{{Name: baseName, URL: u, Ext: ext, Kind: kind, FileType: fileType}}
}

func dedupeICVEResolvedResources(items []icveResolvedResource) []icveResolvedResource {
	seen := map[string]bool{}
	out := make([]icveResolvedResource, 0, len(items))
	for _, item := range items {
		key := item.URL + "|" + item.Name
		if item.URL == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func resolveICVETranscodedURL(c *util.Client, headers map[string]string, mode int, payload map[string]any, originExt string) string {
	if c == nil || payload == nil {
		return ""
	}
	fileGenURL := firstNonEmpty(str(payload["fileGenUrl"]), str(payload["ossGenUrl"]))
	if fileGenURL == "" || !strings.HasPrefix(fileGenURL, "http") {
		return ""
	}
	urlShort := firstNonEmpty(str(payload["urlShort"]), str(payload["content"]))
	if urlShort == "" {
		candidate := str(payload["url"])
		if candidate != "" && !strings.HasPrefix(strings.ToLower(candidate), "http") {
			urlShort = candidate
		}
	}
	ac := &aiCtx{c: c, headers: headers, mode: mode}
	if urlShort != "" {
		statusBody, err := c.GetString(fmt.Sprintf(urlSourceStatus, strings.TrimLeft(urlShort, "/")), headers)
		if err == nil {
			if u := ac.selectTranscodedURL(fileGenURL, firstNonEmpty(originExt, pickExt(str(payload["ossOriUrl"])), "mp4"), parseJSONMap(statusBody)); u != "" {
				return u
			}
		}
	}
	args := firstNonEmptyMap(payload, "args")
	if len(args) == 0 {
		args = payload
	}
	return ac.selectTranscodedURL(fileGenURL, firstNonEmpty(originExt, pickExt(str(payload["ossOriUrl"])), "mp4"), map[string]any{"args": args, "type": str(payload["type"])})
}

func resolveICVEResourceMedia(c *util.Client, headers map[string]string, mode int, payload map[string]any, fileType string) (string, string, string) {
	return resolveICVEResourceMediaDepth(c, headers, mode, payload, fileType, 0)
}

func resolveICVEResourceMediaDepth(c *util.Client, headers map[string]string, mode int, payload map[string]any, fileType string, depth int) (string, string, string) {
	if payload == nil || depth > 5 {
		return "", "", "file"
	}
	merged := mergeICVEContainerPayload(payload)

	fileType = normalizeICVEFileType(firstNonEmpty(
		fileType,
		str(merged["fileType"]),
		str(merged["file_type"]),
		str(merged["type"]),
		str(merged["suffix"]),
		str(merged["fileSuffix"]),
	))

	for _, item := range extractVOSResource(merged) {
		child := mergeICVEResourcePayload(item)
		childFileType := firstNonEmpty(
			str(child["fileType"]),
			str(child["file_type"]),
			str(child["type"]),
			str(child["suffix"]),
			str(child["fileSuffix"]),
			fileType,
		)
		for _, key := range []string{"fileType", "file_type", "type", "suffix", "fileSuffix"} {
			if _, exists := child[key]; !exists && merged[key] != nil {
				child[key] = merged[key]
			}
		}
		if u, ext, kind := resolveICVEResourceMediaDepth(c, headers, mode, child, childFileType, depth+1); u != "" {
			return u, ext, kind
		}
	}

	direct := firstNonEmpty(
		str(merged["ossOriUrl"]),
		str(merged["fileUrl"]),
		str(merged["downloadUrl"]),
		str(merged["downloadurl"]),
		str(merged["url"]),
	)
	if strings.HasPrefix(strings.TrimSpace(direct), "{") || strings.HasPrefix(strings.TrimSpace(direct), "[") {
		nested := parseICVEResourcePayload(direct)
		for k, v := range merged {
			if _, exists := nested[k]; !exists {
				nested[k] = v
			}
		}
		return resolveICVEResourceMediaDepth(c, headers, mode, nested, fileType, depth+1)
	}

	kind := "file"
	if isVideoType(fileType) || isVideoType(pickExt(direct)) {
		kind = "video"
	}
	if kind == "video" {
		if u := resolveICVETranscodedURL(c, headers, mode, merged, "mp4"); u != "" {
			return u, pickExt(u), kind
		}
	}
	direct = normalizeICVEHTTPURL(direct)
	if direct == "" {
		return "", "", kind
	}
	ext := pickExt(direct)
	if ext == "" && fileType != "" && !isVideoType(fileType) {
		ext = fileType
	}
	return direct, ext, kind
}

func normalizeICVEHTTPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if !strings.HasPrefix(strings.ToLower(raw), "http") {
		return ""
	}
	return raw
}

func resolveICVEPDFPageResources(c *util.Client, headers map[string]string, mode int, payload map[string]any, fileType, baseName string) []icveResolvedResource {
	if !isICVEPDFLike(fileType, payload) {
		return nil
	}
	pages := resolveICVEPDFPageURLs(c, headers, mode, payload)
	if len(pages) == 0 {
		return nil
	}
	out := make([]icveResolvedResource, 0, len(pages))
	for idx, pageURL := range pages {
		ext := pickExt(pageURL)
		if ext == "" {
			ext = "png"
		}
		name := strings.TrimSpace(baseName)
		if name == "" {
			name = "pdf"
		}
		out = append(out, icveResolvedResource{
			Name:     fmt.Sprintf("%s_%03d", name, idx+1),
			URL:      pageURL,
			Ext:      ext,
			Kind:     "file",
			FileType: "pdf_page",
		})
	}
	return out
}

func resolveICVEPDFPageURLs(c *util.Client, headers map[string]string, _ int, payload map[string]any) []string {
	if payload == nil {
		return nil
	}
	merged := mergeICVEContainerPayload(payload)
	var pages []string
	for _, key := range []string{
		"pageUrls", "pageURLS", "page_urls", "pageUrlList", "pageURLList",
		"pageList", "page_list", "pngs", "urlPngs", "urlPngList",
		"pages", "urls",
	} {
		pages = append(pages, collectICVEImageURLs(merged[key])...)
	}

	genURL := firstNonEmpty(str(merged["fileGenUrl"]), str(merged["ossGenUrl"]))
	urlShort := firstNonEmpty(str(merged["urlShort"]), str(merged["content"]))
	if urlShort == "" {
		candidate := str(merged["url"])
		if candidate != "" && !strings.HasPrefix(strings.ToLower(candidate), "http") {
			urlShort = candidate
		}
	}
	if c != nil && genURL != "" && strings.HasPrefix(genURL, "http") && urlShort != "" {
		statusBody, err := c.GetString(fmt.Sprintf(urlSourceStatus, strings.TrimLeft(urlShort, "/")), headers)
		if err == nil {
			status := parseJSONMap(statusBody)
			if count := icvePageCount(status); count > 0 {
				pages = append(pages, icveGeneratedPageURLs(genURL, count)...)
			}
			if args := firstNonEmptyMap(status, "args"); len(args) > 0 {
				pages = append(pages, collectICVEImageURLs(args)...)
			}
		}
	}
	if genURL != "" && strings.HasPrefix(genURL, "http") {
		if count := icvePageCount(merged); count > 0 {
			pages = append(pages, icveGeneratedPageURLs(genURL, count)...)
		}
		if args := firstNonEmptyMap(merged, "args"); len(args) > 0 {
			if count := icvePageCount(args); count > 0 {
				pages = append(pages, icveGeneratedPageURLs(genURL, count)...)
			}
			pages = append(pages, collectICVEImageURLs(args)...)
		}
	}

	if c != nil && len(pages) == 0 {
		if fileURL := firstNonEmpty(str(merged["ossOriUrl"]), str(merged["downloadUrl"]), str(merged["downloadurl"]), str(merged["fileUrl"]), str(merged["url"])); isICVEPDFURL(fileURL) {
			pages = append(pages, fetchICVEPDFPageURLs(c, headers, fileURL)...)
		}
	}
	return dedupeICVEStrings(pages)
}

func fetchICVEPDFPageURLs(c *util.Client, headers map[string]string, fileURL string) []string {
	fileURL = strings.TrimSpace(fileURL)
	if c == nil || fileURL == "" {
		return nil
	}
	body, err := c.GetString(fmt.Sprintf(icveURLPDFPNGs, url.QueryEscape(fileURL)), headers)
	if err != nil {
		return nil
	}
	root := parseJSONMap(body)
	var pages []string
	pages = append(pages, collectICVEImageURLs(root)...)
	for _, key := range []string{"data", "rows", "list", "items", "page"} {
		pages = append(pages, collectICVEImageURLs(root[key])...)
	}
	return dedupeICVEStrings(pages)
}

func collectICVEImageURLs(value any) []string {
	var out []string
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			dec := json.NewDecoder(strings.NewReader(s))
			dec.UseNumber()
			var parsed any
			if err := dec.Decode(&parsed); err == nil {
				return collectICVEImageURLs(parsed)
			}
		}
		if u := normalizeICVEHTTPURL(s); u != "" && isICVEImageURL(u) {
			out = append(out, u)
		}
	case []any:
		for _, item := range v {
			out = append(out, collectICVEImageURLs(item)...)
		}
	case []string:
		for _, item := range v {
			out = append(out, collectICVEImageURLs(item)...)
		}
	case map[string]any:
		for _, key := range []string{
			"url", "src", "href", "previewUrl", "previewURL", "imgUrl", "imgURL",
			"imageUrl", "imageURL", "pngUrl", "pngURL", "fileUrl", "downloadUrl",
		} {
			out = append(out, collectICVEImageURLs(v[key])...)
		}
		for _, key := range []string{
			"pageUrls", "pageURLS", "page_urls", "pageUrlList", "pageURLList",
			"pageList", "page_list", "pngs", "urlPngs", "urlPngList", "dataList",
			"pages", "urls", "list", "rows", "data", "items",
		} {
			out = append(out, collectICVEImageURLs(v[key])...)
		}
	}
	return dedupeICVEStrings(out)
}

func icvePageCount(payload map[string]any) int {
	if payload == nil {
		return 0
	}
	if args := firstNonEmptyMap(payload, "args"); len(args) > 0 {
		if n := icvePageCount(args); n > 0 {
			return n
		}
	}
	for _, key := range []string{"page_count", "pageCount", "pages", "totalPage", "totalPages"} {
		if n := intVal(payload[key]); n > 0 {
			return n
		}
	}
	return 0
}

func icveGeneratedPageURLs(genURL string, count int) []string {
	genURL = strings.TrimRight(strings.TrimSpace(genURL), "/")
	if genURL == "" || count <= 0 {
		return nil
	}
	pages := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		pages = append(pages, fmt.Sprintf("%s/%d.png", genURL, i))
	}
	return pages
}

func isICVEPDFLike(fileType string, payload map[string]any) bool {
	ft := normalizeICVEFileType(fileType)
	if ft == "pdf" {
		return true
	}
	if payload == nil {
		return false
	}
	merged := mergeICVEContainerPayload(payload)
	if ft := normalizeICVEFileType(firstNonEmpty(str(merged["fileType"]), str(merged["file_type"]), str(merged["type"]), str(merged["suffix"]), str(merged["fileSuffix"]))); ft == "pdf" {
		return true
	}
	for _, key := range []string{"ossOriUrl", "fileUrl", "downloadUrl", "downloadurl", "url", "name", "fileName", "title"} {
		if isICVEPDFURL(str(merged[key])) {
			return true
		}
	}
	return false
}

func isICVEPDFURL(raw string) bool {
	return pickExt(raw) == "pdf"
}

func isICVEImageURL(raw string) bool {
	switch pickExt(raw) {
	case "png", "jpg", "jpeg", "webp", "gif":
		return true
	default:
		return false
	}
}

func dedupeICVEStrings(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
