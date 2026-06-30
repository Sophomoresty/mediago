package sier

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

func cookieInfoFromJar(j http.CookieJar) cookieInfo {
	var ck cookieInfo
	var parts []string
	for _, host := range []string{"https://player.sieredu.com/", "https://www.sieredu.com/", "https://study.sieredu.com/"} {
		u, _ := url.Parse(host)
		for _, c := range j.Cookies(u) {
			parts = append(parts, c.Name+"="+c.Value)
			if strings.EqualFold(c.Name, "sid") {
				ck.SID = c.Value
			}
			if strings.EqualFold(c.Name, "deviceId") || strings.EqualFold(c.Name, "deviceid") {
				ck.DeviceID = c.Value
			}
		}
	}
	ck.Cookie = strings.Join(parts, "; ")
	return ck
}
func sierHeaders(ck cookieInfo, ref string) map[string]string {
	h := map[string]string{"Accept": "application/json, text/plain, */*", "User-Agent": user_agent, "Origin": "https://player.sieredu.com", "Referer": ref, "referer": ref, "Cookie": ck.Cookie, "cookie": ck.Cookie, "uuid": ck.DeviceID}
	if ck.SID != "" {
		h["authorization"] = "Bearer " + ck.SID
	}
	return h
}
func decryptPsign(m map[string]any) string {
	tok := first(textAt(m, "token", "psign", "pSign"))
	iv := textAt(m, "iv")
	if tok == "" || iv == "" {
		return tok
	}
	key, e1 := base64.StdEncoding.DecodeString(SIER_TOKEN_AES_KEY_B64)
	ivb, e2 := base64.StdEncoding.DecodeString(iv)
	data, e3 := base64.StdEncoding.DecodeString(tok)
	if e1 != nil || e2 != nil || e3 != nil || len(data)%aes.BlockSize != 0 {
		return tok
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return tok
	}
	cipher.NewCBCDecrypter(block, ivb).CryptBlocks(data, data)
	return string(pkcs7Unpad(data))
}
func pkcs7Unpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n < 1 || n > aes.BlockSize || n > len(b) {
		return b
	}
	return b[:len(b)-n]
}
func directPlayURL(m map[string]any) string {
	if s := textAt(m, "playUrl", "videoUrl", "fileUrl", "downloadUrl", "url", "path"); strings.HasPrefix(s, "http") {
		return s
	}
	if mm, ok := m["playUrl"].(map[string]any); ok {
		return directPlayURL(mm)
	}
	return ""
}
func extractLists(m map[string]any, keys ...string) []map[string]any {
	for _, k := range keys {
		if l, ok := m[k].([]any); ok {
			return maps(l)
		}
	}
	if d, ok := m["data"].(map[string]any); ok {
		return extractLists(d, keys...)
	}
	if e, ok := m["entity"].(map[string]any); ok {
		return extractLists(e, keys...)
	}
	return nil
}
func unwrapMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		if e, ok := m["entity"].(map[string]any); ok {
			return e
		}
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
func mergeMaps(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
func findURL(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if u := directPlayURL(t); u != "" {
			return u
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
func textAt(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && fmt.Sprint(v) != "<nil>" {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}
func numAt(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}
func insertDRMToken(u, token string) string {
	if u == "" || token == "" || strings.Contains(u, "voddrm.token.") {
		return u
	}
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	i := strings.LastIndex(p.Path, "/")
	if i < 0 {
		return u
	}
	p.Path = p.Path[:i+1] + "voddrm.token." + token + "." + p.Path[i+1:]
	return p.String()
}
func cloneHeaders(h map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[k] = v
	}
	return out
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
	if strings.HasPrefix(strings.ToLower(u), "data:application/vnd.apple.mpegurl") {
		return "m3u8"
	}
	p := strings.ToLower(strings.SplitN(strings.SplitN(u, "?", 2)[0], "#", 2)[0])
	if strings.Contains(p, ".m3u8") {
		return "m3u8"
	}
	if i := strings.LastIndex(p, "."); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return "mp4"
}
