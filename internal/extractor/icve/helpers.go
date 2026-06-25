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
)

var (
	aiCIDRe          = regexp.MustCompile(`(?i)https?://ai\.icve\.com\.cn/.*?excellent.*?/([-\w]+)|https?://ai\.icve\.com\.cn/.*?course.*?/([-\w]+)`)
	spaceCollapseRe  = regexp.MustCompile(`\s+`)
	tagStripRe       = regexp.MustCompile(`(?is)<.*?>`)
	filenameBadChars = regexp.MustCompile(`[<>:"/\\|?*]+`)
)

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
