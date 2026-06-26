package gaotu

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type gaotuFileNode struct {
	ID     string
	Name   string
	URL    string
	Format string
	Type   string
	Root   string
}

func fetchGaotuPrice(c *util.Client, headers map[string]string, endpoints gaotuEndpoints, clazz string) (float64, bool) {
	if clazz == "" {
		return 0, false
	}
	body, err := c.GetString(fmt.Sprintf(endpoints.priceURL(), q(clazz)), headers)
	if err != nil {
		return 0, false
	}
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return 0, false
	}
	price, ok := gaotuPriceFromPayload(payload)
	return price, ok
}

func gaotuPriceFromPayload(v any) (float64, bool) {
	switch x := v.(type) {
	case map[string]any:
		if data, ok := x["data"].(map[string]any); ok {
			if core, ok := data["coreButton"].(map[string]any); ok {
				return gaotuCentsToPrice(core["price"])
			}
		}
		if core, ok := x["coreButton"].(map[string]any); ok {
			return gaotuCentsToPrice(core["price"])
		}
		for _, child := range x {
			if price, ok := gaotuPriceFromPayload(child); ok {
				return price, true
			}
		}
	case []any:
		for _, child := range x {
			if price, ok := gaotuPriceFromPayload(child); ok {
				return price, true
			}
		}
	}
	return 0, false
}

func gaotuCentsToPrice(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if n == 0 {
			return 0, false
		}
		return float64(int64(n)) / 100, true
	case float32:
		if n == 0 {
			return 0, false
		}
		return float64(int64(n)) / 100, true
	case int:
		if n == 0 {
			return 0, false
		}
		return float64(n) / 100, true
	case int64:
		if n == 0 {
			return 0, false
		}
		return float64(n) / 100, true
	case string:
		n = strings.TrimSpace(n)
		if n == "" || n == "0" {
			return 0, false
		}
		var cents float64
		if _, err := fmt.Sscan(n, &cents); err != nil || cents == 0 {
			return 0, false
		}
		return float64(int64(cents)) / 100, true
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" || s == "0" {
			return 0, false
		}
		var cents float64
		if _, err := fmt.Sscan(s, &cents); err != nil || cents == 0 {
			return 0, false
		}
		return float64(int64(cents)) / 100, true
	}
}

func resolveGaotuFiles(c *util.Client, headers map[string]string, endpoints gaotuEndpoints, root string) []*extractor.MediaInfo {
	if root == "" {
		return nil
	}
	seenDirs := map[string]bool{}
	seenFiles := map[string]bool{}
	var entries []*extractor.MediaInfo
	var walk func(parent string)
	walk = func(parent string) {
		key := parent + "|" + root
		if seenDirs[key] {
			return
		}
		seenDirs[key] = true
		payload, err := postJSON(c, endpoints.sourceURL(), map[string]any{
			"pageNo":       1,
			"pageSize":     999,
			"parentNumber": parent,
			"rootNumber":   root,
		}, headers)
		if err != nil {
			return
		}
		for _, node := range collectGaotuPanNodes(payload) {
			if node.ID == "" && node.URL == "" {
				continue
			}
			node.Root = firstNonEmpty(node.Root, root)
			if isGaotuDir(node) {
				walk(node.ID)
				continue
			}
			entry := gaotuFileMedia(c, headers, endpoints, node, len(entries)+1)
			if entry == nil || len(entry.Streams) == 0 {
				continue
			}
			urls := entry.Streams["file"].URLs
			if len(urls) == 0 || seenFiles[urls[0]] {
				continue
			}
			seenFiles[urls[0]] = true
			entries = append(entries, entry)
		}
	}
	walk("")
	return entries
}

func collectGaotuPanNodes(v any) []gaotuFileNode {
	var out []gaotuFileNode
	seen := map[string]bool{}
	var walk func(any)
	walk = func(x any) {
		switch vv := x.(type) {
		case map[string]any:
			if node, ok := parseGaotuPanNode(vv); ok {
				key := strings.Join([]string{node.ID, node.URL, node.Name, node.Type, node.Root}, "|")
				if !seen[key] {
					seen[key] = true
					out = append(out, node)
				}
			}
			for _, k := range []string{"dirList", "children", "items", "list", "data", "result"} {
				if child, ok := vv[k]; ok {
					walk(child)
				}
			}
			for _, child := range vv {
				walk(child)
			}
		case []any:
			for _, child := range vv {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func parseGaotuPanNode(m map[string]any) (gaotuFileNode, bool) {
	node := gaotuFileNode{
		ID:     firstNonEmpty(valueString(m, "file_id", "entityNumber", "id")),
		Name:   firstNonEmpty(valueString(m, "file_name", "name", "title")),
		URL:    normalizeURL(firstNonEmpty(valueString(m, "file_url", "url", "fileUrl", "downloadUrl", "downloadURL"))),
		Format: strings.TrimPrefix(strings.ToLower(firstNonEmpty(valueString(m, "file_fmt", "fileFmt", "format", "suffix"))), "."),
		Type:   firstNonEmpty(valueString(m, "file_type", "entityType", "type")),
		Root:   firstNonEmpty(valueString(m, "file_number", "rootNumber", "subclazzNumber")),
	}
	if node.Name == "" && node.ID == "" && node.URL == "" {
		return gaotuFileNode{}, false
	}
	if node.Format == "" {
		node.Format = gaotuFileFormat(node.Name, node.URL)
	}
	return node, true
}

func isGaotuDir(node gaotuFileNode) bool {
	typ := strings.ToLower(strings.TrimSpace(node.Type))
	return typ == "1" || typ == "dir" || typ == "folder"
}

func gaotuFileMedia(c *util.Client, headers map[string]string, endpoints gaotuEndpoints, node gaotuFileNode, index int) *extractor.MediaInfo {
	rawURL := node.URL
	format := firstNonEmpty(node.Format, gaotuFileFormat(node.Name, rawURL), "bin")
	if node.Type == "100" || format == "mp4" {
		if resolved := resolveGaotuFileURL(c, headers, endpoints, node); resolved != "" {
			rawURL = resolved
			format = firstNonEmpty(gaotuFileFormat(node.Name, rawURL), format, "mp4")
		}
	}
	if rawURL == "" && node.ID != "" {
		rawURL = resolveGaotuFileURL(c, headers, endpoints, node)
		format = firstNonEmpty(gaotuFileFormat(node.Name, rawURL), format)
	}
	if !isHTTPURL(rawURL) {
		return nil
	}
	if format == "" {
		format = gaotuFileFormat(node.Name, rawURL)
	}
	if format == "" {
		format = "bin"
	}
	title := firstNonEmpty(node.Name, fmt.Sprintf("gaotu_file_%02d", index))
	extra := map[string]any{"kind": "file"}
	if node.ID != "" {
		extra["file_id"] = node.ID
	}
	if node.Type != "" {
		extra["file_type"] = node.Type
	}
	if node.Root != "" {
		extra["root_number"] = node.Root
	}
	return &extractor.MediaInfo{
		Site:  "gaotu",
		Title: util.SanitizeFilename(title),
		Streams: map[string]extractor.Stream{
			"file": {
				Quality: "source",
				URLs:    []string{rawURL},
				Format:  format,
				Headers: headers,
			},
		},
		Extra: extra,
	}
}

func resolveGaotuFileURL(c *util.Client, headers map[string]string, endpoints gaotuEndpoints, node gaotuFileNode) string {
	if node.ID == "" {
		return ""
	}
	payload, err := postJSON(c, endpoints.fileURL(), map[string]any{
		"entityNumber":   node.ID,
		"entityType":     100,
		"subclazzNumber": firstNonEmpty(node.Root, ""),
	}, headers)
	if err != nil {
		return ""
	}
	if raw := firstFieldString(payload, "fileUrl", "file_url", "url"); raw != "" {
		if media := decodeGaotuMediaURL(c, headers, raw); media != "" {
			return media
		}
		if isHTTPURL(raw) {
			return raw
		}
	}
	if media := findMediaURL(payload); media != "" {
		return media
	}
	if raw := firstHTTPURL(payload); raw != "" {
		return raw
	}
	return ""
}

func decodeGaotuMediaURL(c *util.Client, headers map[string]string, raw string) string {
	raw = normalizeURL(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "bjcloudvod://") {
		return decodeBjcloudvod(raw)
	}
	if strings.Contains(raw, "?") {
		if media := decodePcURL(c, headers, raw); media != "" {
			return media
		}
	}
	return raw
}

func firstFieldString(v any, keys ...string) string {
	switch x := v.(type) {
	case map[string]any:
		if s := valueString(x, keys...); s != "" {
			return s
		}
		for _, child := range x {
			if s := firstFieldString(child, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := firstFieldString(child, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}

func firstHTTPURL(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range []string{"downloadUrl", "downloadURL", "fileUrl", "fileURL", "file_url", "url", "path", "resourceUrl", "resourceURL", "attachUrl", "attachmentUrl", "materialUrl", "handoutUrl", "pdfUrl", "pptUrl", "docUrl"} {
			if s := normalizeURL(valueString(x, k)); isHTTPURL(s) {
				return s
			}
		}
		for _, child := range x {
			if s := firstHTTPURL(child); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range x {
			if s := firstHTTPURL(child); s != "" {
				return s
			}
		}
	case string:
		if s := normalizeURL(x); isHTTPURL(s) {
			return s
		}
	}
	return ""
}

func gaotuFileFormat(name, rawURL string) string {
	for _, s := range []string{name, rawURL} {
		if idx := strings.LastIndexAny(s, "?#"); idx >= 0 {
			s = s[:idx]
		}
		if dot := strings.LastIndex(s, "."); dot >= 0 && dot < len(s)-1 {
			ext := strings.ToLower(s[dot+1:])
			if len(ext) <= 8 {
				return ext
			}
		}
	}
	return ""
}

func isHTTPURL(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}

func compactExtra(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func decodeBjcloudvod(encoded string) string {
	const prefix = "bjcloudvod://"
	if !strings.HasPrefix(encoded, prefix) {
		return ""
	}
	payload := strings.TrimPrefix(encoded, prefix)
	payload = strings.NewReplacer("-", "+", "_", "/").Replace(payload)
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(decoded) == 0 {
		return ""
	}
	shift := int(decoded[0] % 8)
	decoded = decoded[1:]
	out := make([]byte, len(decoded))
	for i, b := range decoded {
		out[i] = b ^ byte((shift+i)%8)
	}
	return string(out)
}
