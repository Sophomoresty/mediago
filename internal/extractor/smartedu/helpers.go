package smartedu

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/util"
)

var (
	smExtXURIRe = regexp.MustCompile(`URI="([^"]*)"`)
	smKeyIDRe   = regexp.MustCompile(`keys/([^/?#"]+)`)
)

func selectVideoItem(r map[string]any) map[string]any {
	list := items(r)
	flags := []string{"href-720p-m3u8", "href-m3u8", "href", "href-480p-m3u8", "href-360p-m3u8"}
	for _, f := range flags {
		for _, it := range list {
			if str(it["ti_file_flag"]) == f && isVideoFmt(it) {
				return it
			}
		}
	}
	for _, it := range list {
		if isVideoFmt(it) {
			return it
		}
	}
	return nil
}

func selectFileItem(r map[string]any) map[string]any {
	list := items(r)
	fileFmt := map[string]bool{"pdf": true, "ppt": true, "pptx": true, "doc": true, "docx": true, "xls": true, "xlsx": true, "zip": true, "rar": true, "7z": true}
	for _, f := range []string{"source", "pdf", "href"} {
		for _, it := range list {
			if str(it["ti_file_flag"]) == f && fileFmt[strings.ToLower(str(it["ti_format"]))] {
				return it
			}
		}
	}
	for _, ext := range []string{"pdf", "ppt", "pptx", "doc", "docx", "xls", "xlsx", "zip", "rar", "7z"} {
		for _, it := range list {
			if strings.ToLower(str(it["ti_format"])) == ext {
				return it
			}
		}
	}
	return nil
}

func itemURL(it map[string]any) string {
	urls := itemURLs(it)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func itemURLs(it map[string]any) []string {
	for _, k := range []string{"ti_storage", "url", "download_url", "href"} {
		if u := str(it[k]); u != "" {
			return normalizeStorageCandidates(u)
		}
	}
	if arr, ok := it["ti_storages"].([]any); ok {
		for _, v := range arr {
			if m, ok := v.(map[string]any); ok {
				for _, k := range []string{"ti_storage", "storage", "url", "download_url", "href", "ti_url"} {
					if u := str(m[k]); u != "" {
						return normalizeStorageCandidates(u)
					}
				}
				if nested, ok := m["storage"].(map[string]any); ok {
					for _, k := range []string{"ti_storage", "storage", "url", "download_url", "href", "ti_url"} {
						if u := str(nested[k]); u != "" {
							return normalizeStorageCandidates(u)
						}
					}
				}
			}
		}
	}
	return nil
}

func normalizeStorage(s string) string {
	urls := normalizeStorageCandidates(s)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func normalizeStorageCandidates(s string) []string {
	s = strings.TrimSpace(strings.ReplaceAll(s, `\/`, `/`))
	if s == "" {
		return nil
	}
	if strings.Contains(s, "cs_path:${ref-path}") {
		out := make([]string, 0, len(privateHosts))
		for _, host := range privateHosts {
			out = append(out, normalize(strings.ReplaceAll(s, "cs_path:${ref-path}", host), host))
		}
		return dedupeStrings(out)
	}
	normalized := normalize(s, privateHost)
	if expanded := expandSmarteduCDNHosts(normalized); len(expanded) > 0 {
		return expanded
	}
	return []string{normalized}
}

func expandSmarteduCDNHosts(raw string) []string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil
	}
	for _, hosts := range [][]string{privateHosts, publicHosts, overseaHosts} {
		if !hostInList(u.Host, hosts) {
			continue
		}
		out := make([]string, 0, len(hosts))
		for _, host := range hosts {
			next := *u
			if parsed, err := url.Parse(host); err == nil && parsed.Host != "" {
				next.Scheme = firstNonEmpty(parsed.Scheme, next.Scheme)
				next.Host = parsed.Host
			}
			out = append(out, next.String())
		}
		return dedupeStrings(out)
	}
	return nil
}

func hostInList(host string, hosts []string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	for _, raw := range hosts {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if strings.EqualFold(h, u.Host) {
			return true
		}
	}
	return false
}

func dedupeStrings(in []string) []string {
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

func relationResources(m map[string]any) []map[string]any {
	var out []map[string]any
	if rel, ok := m["relations"].(map[string]any); ok {
		for _, k := range []string{"national_course_resource", "tch_materials", "basic_works", "prepare_lessons", "elite_lessons"} {
			out = append(out, mapsFromAny(rel[k])...)
		}
	}
	return out
}
func collectResourceMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			if len(items(t)) > 0 || str(t["id"]) != "" {
				out = append(out, t)
			}
			for _, c := range t {
				walk(c)
			}
		case []any:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(v)
	return out
}
func mapsFromAny(v any) []map[string]any {
	var out []map[string]any
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}
func items(r map[string]any) []map[string]any { return mapsFromAny(r["ti_items"]) }
func isVideoFmt(it map[string]any) bool {
	f := strings.ToLower(firstNonEmpty(str(it["ti_format"]), extFormat(itemURL(it))))
	return f == "m3u8" || f == "mp4"
}
func itemSize(it map[string]any) int64 {
	var n int64
	switch v := it["ti_size"].(type) {
	case float64:
		n = int64(v)
	case json.Number:
		n, _ = v.Int64()
	}
	return n
}
func extFormat(u string) string {
	e := strings.TrimPrefix(strings.ToLower(path.Ext(strings.Split(u, "?")[0])), ".")
	return e
}
func staticBases() []string  { return []string{staticBase0, staticBase1} }
func specialBases() []string { return []string{special0, special1, special2, special3} }
func tplURLs(tpl string, bases []string, id string) []string {
	out := make([]string, 0, len(bases))
	for _, b := range bases {
		out = append(out, fmt.Sprintf(tpl, b, url.PathEscape(id)))
	}
	return out
}
func firstQuery(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			return v
		}
	}
	return ""
}
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func str(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
}
func globalTitle(m map[string]any) string {
	for _, k := range []string{"title", "name", "global_title", "globalTitle"} {
		if s := str(m[k]); s != "" {
			return s
		}
		if mm, ok := m[k].(map[string]any); ok {
			if s := str(mm["zh-CN"]); s != "" {
				return s
			}
			for _, v := range mm {
				if s := str(v); s != "" {
					return s
				}
			}
		}
	}
	return ""
}
func normalize(s, base string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	if strings.HasPrefix(s, "/") && base != "" {
		b, _ := url.Parse(base)
		u, _ := url.Parse(s)
		return b.ResolveReference(u).String()
	}
	return s
}
func isPrivate(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Host)
	return strings.Contains(h, "-private.") || strings.Contains(h, "ndr-private.")
}
func privateToPublic(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	u.Host = strings.Replace(strings.Replace(u.Host, "-private.", ".", 1), "ndr-private.", "ndr.", 1)
	return u.String()
}
func privateURLsToPublic(urls []string) []string {
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		out = append(out, privateToPublic(u))
	}
	return dedupeStrings(out)
}

func (x *smCtx) prepareM3U8(raw string) (string, string, error) {
	text, err := x.c.GetString(raw, x.requestHeaders(raw, true))
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(strings.TrimSpace(text), "#EXTM3U") {
		return "", "", fmt.Errorf("smartedu: not an m3u8 manifest")
	}
	rewritten := x.absoluteM3U8Text(text, raw)
	return smarteduM3U8DataURL(rewritten), rewritten, nil
}

func (x *smCtx) prepareM3U8Candidates(urls []string) (string, string, string, error) {
	var last error
	for _, raw := range urls {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		dataURL, manifest, err := x.prepareM3U8(raw)
		if err == nil && dataURL != "" {
			return dataURL, manifest, raw, nil
		}
		last = err
	}
	if last != nil {
		return "", "", "", last
	}
	return "", "", "", fmt.Errorf("smartedu: empty m3u8 URL candidates")
}

func (x *smCtx) absoluteM3U8Text(text, base string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			out = append(out, line)
		case strings.HasPrefix(trimmed, "#EXT-X-KEY"):
			out = append(out, x.rewriteM3U8URI(line, base, true))
		case strings.HasPrefix(trimmed, "#EXT-X-MAP"):
			out = append(out, x.rewriteM3U8URI(line, base, false))
		case strings.HasPrefix(trimmed, "#"):
			out = append(out, line)
		case strings.HasPrefix(trimmed, "data:"):
			out = append(out, line)
		default:
			out = append(out, x.withAccess(resolveSmarteduURL(trimmed, base)))
		}
	}
	return strings.Join(out, "\n")
}

func (x *smCtx) rewriteM3U8URI(line, base string, isKey bool) string {
	return smExtXURIRe.ReplaceAllStringFunc(line, func(match string) string {
		parts := smExtXURIRe.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		resolved := resolveSmarteduURL(parts[1], base)
		if !isKey {
			return `URI="` + x.withAccess(resolved) + `"`
		}
		signed := x.signedVideoKeyURL(resolved)
		if body, err := x.c.GetBytes(signed, x.requestHeaders(signed, true)); err == nil {
			if key := x.decryptSmarteduKey(body); len(key) > 0 {
				return `URI="data:application/octet-stream;base64,` + base64.StdEncoding.EncodeToString(key) + `"`
			}
		}
		return `URI="` + signed + `"`
	})
}

func (x *smCtx) signedVideoKeyURL(keyURL string) string {
	keyURL = normalize(keyURL, "")
	if !strings.HasPrefix(strings.ToLower(keyURL), "http") {
		return keyURL
	}
	signsURL := strings.TrimRight(keyURL, "/") + "/signs"
	body, err := x.c.GetBytes(signsURL, x.requestHeaders(signsURL, true))
	if err != nil {
		return keyURL
	}
	var resp map[string]any
	if json.Unmarshal(body, &resp) != nil {
		return keyURL
	}
	nonce := str(resp["nonce"])
	if nonce == "" {
		return keyURL
	}
	keyID := ""
	if m := smKeyIDRe.FindStringSubmatch(keyURL); len(m) > 1 {
		keyID = m[1]
	} else if u, err := url.Parse(keyURL); err == nil {
		keyID = path.Base(u.Path)
	}
	if keyID == "" {
		return keyURL
	}
	sign := util.MD5(nonce + keyID)
	if len(sign) > 16 {
		sign = sign[:16]
	}
	x.lastVideoKeySign = sign
	u, err := url.Parse(keyURL)
	if err != nil {
		return keyURL
	}
	q := u.Query()
	q.Set("nonce", nonce)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String()
}

func (x *smCtx) decryptSmarteduKey(content []byte) []byte {
	if len(content) == 0 {
		return nil
	}
	var resp map[string]any
	if json.Unmarshal(content, &resp) != nil {
		return content
	}
	encoded := str(resp["key"])
	sign := firstNonEmpty(str(resp["_smartedu_sign"]), x.lastVideoKeySign)
	if encoded == "" || sign == "" {
		return nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	plain, err := util.AESDecryptECB(ciphertext, []byte(sign))
	if err != nil {
		return nil
	}
	return plain
}

func resolveSmarteduURL(raw, base string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "data:") {
		return raw
	}
	u, err := url.Parse(raw)
	if err == nil && u.IsAbs() {
		return raw
	}
	b, err := url.Parse(base)
	if err != nil {
		return normalize(raw, privateHost)
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return b.ResolveReference(ref).String()
}

func smarteduM3U8DataURL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}

func isM3U8URL(u, fmtv string) bool {
	lu := strings.ToLower(u)
	return strings.EqualFold(fmtv, "m3u8") || strings.Contains(lu, ".m3u8") || strings.HasPrefix(lu, "data:application/vnd.apple.mpegurl")
}

func tagSet(m map[string]any) map[string]bool {
	out := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			if s := strings.TrimSpace(x); s != "" {
				out[s] = true
			}
		case []any:
			for _, it := range x {
				walk(it)
			}
		case map[string]any:
			for _, key := range []string{"id", "tag_id", "tagId", "code", "value"} {
				if s := str(x[key]); s != "" {
					out[s] = true
				}
			}
			for _, key := range []string{"tag_list", "tags", "children"} {
				walk(x[key])
			}
		}
	}
	walk(m["tag_list"])
	walk(m["tags"])
	return out
}

func cookieHeader(jar http.CookieJar, bases []string) string {
	seen := map[string]bool{}
	var parts []string
	for _, raw := range bases {
		u, _ := url.Parse(raw)
		for _, c := range jar.Cookies(u) {
			if !seen[c.Name] {
				seen[c.Name] = true
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}
func decodeAccessToken(cookie string) string {
	return decodeSmarteduAuth(cookie).accessToken
}
func decodeSmarteduAuth(cookie string) smarteduAuth {
	for _, part := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 || (kv[0] != "UC_TOKEN" && !strings.HasPrefix(kv[0], "UC_TOKEN-")) {
			continue
		}
		raw := kv[1] + strings.Repeat("=", (4-len(kv[1])%4)%4)
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			auth := smarteduAuth{
				accessToken:  str(m["access_token"]),
				refreshToken: str(m["refresh_token"]),
				macKey:       str(m["mac_key"]),
			}
			if d := str(m["diff"]); d != "" {
				auth.diff, _ = strconv.ParseInt(d, 10, 64)
			}
			return auth
		}
	}
	return smarteduAuth{}
}
