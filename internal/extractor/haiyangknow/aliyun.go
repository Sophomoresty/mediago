package haiyangknow

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var aliyunMediaTokenRe = regexp.MustCompile(`(?i)^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(.+)$`)

func (x *hyCtx) aliyunSource(play map[string]any) (hySource, bool) {
	videoID := firstString(play, "videoId", "vid")
	pa := firstExisting(play, "playAuth", "playauth")
	payload := decodeAliyunPlayAuth(pa)
	if region := firstString(play, "regionId"); region != "" {
		payload["domain_region"] = region
	}
	if videoID == "" || payload["access_id"] == "" || payload["access_secret"] == "" || payload["domain_region"] == "" {
		return hySource{}, false
	}
	src, info, ok := x.requestAliyunPlayInfo(payload, videoID)
	if !ok || src.URL == "" {
		return hySource{}, false
	}
	if src.NeedMerge {
		if text, err := x.c.GetString(src.URL, map[string]string{"Referer": referer, "Origin": origin}); err == nil {
			src.Extra["m3u8_text"] = text
			if truthy(info["encrypt"]) {
				if rewritten, err := x.rewriteAliyunM3U8Keys(text, payload, str(info["encrypt_type"])); err == nil {
					src.Extra["m3u8_text"] = rewritten
				}
			}
		}
	}
	src.Extra["video_id"] = videoID
	src.Extra["source_type"] = map[bool]string{true: "m3u8_text", false: "video_url"}[src.NeedMerge]
	return src, true
}

func decodeAliyunPlayAuth(value any) map[string]string {
	var m map[string]any
	switch t := value.(type) {
	case map[string]any:
		m = t
	case string:
		s := strings.TrimSpace(t)
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			_ = json.Unmarshal([]byte(s), &m)
		} else if b := safeB64Decode(s); len(b) > 0 {
			_ = json.Unmarshal(b, &m)
		}
	}
	if m == nil {
		return map[string]string{}
	}
	return map[string]string{
		"raw":           str(value),
		"access_id":     firstString(m, "AccessKeyId", "accessKeyId"),
		"access_secret": firstString(m, "AccessKeySecret", "accessKeySecret"),
		"sts_token":     firstString(m, "SecurityToken", "securityToken"),
		"domain_region": firstString(m, "Region", "region"),
		"auth_info":     firstString(m, "AuthInfo", "authInfo"),
		"auth_timeout":  firstNonEmpty(firstString(m, "AuthTimeout", "authTimeout"), "7200"),
	}
}

func (x *hyCtx) requestAliyunPlayInfo(payload map[string]string, videoID string) (hySource, map[string]any, bool) {
	params := map[string]string{
		"Definition":       "FD,LD,SD,HD,OD,2K,4K",
		"ResultType":       "Multiple",
		"VideoId":          videoID,
		"SecurityToken":    payload["sts_token"],
		"SignatureNonce":   aliyunNonce(),
		"SignatureVersion": "1.0",
		"SignatureMethod":  "HMAC-SHA1",
		"Version":          "2017-03-21",
		"Formats":          "m3u8,mp4",
		"AuthTimeout":      firstNonEmpty(payload["auth_timeout"], "7200"),
		"AuthInfo":         payload["auth_info"],
		"Action":           "GetPlayInfo",
		"AccessKeyId":      payload["access_id"],
	}
	params["Signature"] = aliyunSignature(params, payload["access_secret"], "GET")
	api := fmt.Sprintf("https://vod.%s.aliyuncs.com/?%s", payload["domain_region"], aliyunSortedQuery(params))
	body, err := x.c.GetString(api, map[string]string{"Accept": "application/json, text/plain, */*", "Referer": referer, "Origin": origin})
	if err != nil {
		return hySource{}, nil, false
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		root = parseAliyunPlayXML(body)
	}
	info := extractAliyunPlayResponse(root, x.quality)
	if len(info) == 0 {
		return hySource{}, nil, false
	}
	if u := str(info["direct_video_url"]); u != "" {
		return hySource{URL: u, Format: "mp4", Size: int64(intVal(info["size"])), Extra: map[string]any{"aliyun_api": api}}, info, true
	}
	if u := str(info["master_m3u8_url"]); u != "" {
		return hySource{URL: u, Format: "m3u8", NeedMerge: true, Size: int64(intVal(info["size"])), Extra: map[string]any{"aliyun_api": api}}, info, true
	}
	return hySource{}, nil, false
}

func extractAliyunPlayResponse(root map[string]any, quality string) map[string]any {
	pil := asMap(root["PlayInfoList"])
	items := anyList(pil["PlayInfo"])
	if len(items) == 0 {
		return map[string]any{}
	}
	plays := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m := asMap(item)
		if firstString(m, "PlayURL", "PlayUrl") != "" {
			plays = append(plays, m)
		}
	}
	if len(plays) == 0 {
		return map[string]any{}
	}
	rank := definitionRank(quality)
	sort.SliceStable(plays, func(i, j int) bool {
		a, b := plays[i], plays[j]
		da, db := strings.ToUpper(firstString(a, "Definition")), strings.ToUpper(firstString(b, "Definition"))
		ra, rb := rank[da], rank[db]
		if ra != rb {
			return ra < rb
		}
		fa, fb := strings.ToLower(firstString(a, "Format")), strings.ToLower(firstString(b, "Format"))
		if fa != fb {
			return fa == "m3u8"
		}
		return intVal(a["Bitrate"]) > intVal(b["Bitrate"])
	})
	best := plays[0]
	playURL := firstString(best, "PlayURL", "PlayUrl")
	format := strings.ToLower(firstString(best, "Format"))
	size := firstNonEmpty(firstString(best, "Size"), firstString(asMap(root["VideoBase"]), "Size"))
	encrypt := truthy(best["Encrypt"])
	out := map[string]any{"size": parseSizeMB(size), "encrypt": encrypt, "encrypt_type": firstString(best, "EncryptType")}
	if format == "m3u8" || strings.Contains(strings.ToLower(playURL), ".m3u8") {
		out["master_m3u8_url"] = playURL
	} else {
		out["direct_video_url"] = playURL
	}
	return out
}

func definitionRank(quality string) map[string]int {
	var pref []string
	switch strings.ToLower(quality) {
	case "sd", "ld", "fd":
		pref = []string{"SD", "LD", "FD", "HD", "OD", "2K", "4K"}
	case "fhd", "4k", "2k", "od":
		pref = []string{"4K", "2K", "OD", "HD", "SD", "LD", "FD"}
	default:
		pref = []string{"HD", "SD", "LD", "FD", "OD", "2K", "4K"}
	}
	out := map[string]int{}
	for i, v := range pref {
		out[v] = i
	}
	return out
}

func (x *hyCtx) rewriteAliyunM3U8Keys(text string, payload map[string]string, encType string) (string, error) {
	re := regexp.MustCompile(`URI="([^"]+)"`)
	var firstErr error
	out := re.ReplaceAllStringFunc(text, func(line string) string {
		m := re.FindStringSubmatch(line)
		if len(m) != 2 {
			return line
		}
		uri := m[1]
		content := []byte(uri)
		if strings.HasPrefix(uri, "http") {
			b, err := x.c.GetBytes(uri, map[string]string{"Referer": referer, "Origin": origin})
			if err != nil {
				firstErr = err
				return line
			}
			content = b
		}
		mediaID, challenge := extractAliyunKeyMaterial(content)
		if mediaID == "" || challenge == "" {
			return line
		}
		license, err := x.requestAliyunLicense(payload, mediaID, challenge, encType)
		if err != nil || len(license) == 0 {
			firstErr = err
			return line
		}
		return strings.Replace(line, uri, "0x"+strings.ToUpper(hex.EncodeToString(license)), 1)
	})
	if firstErr != nil {
		return out, firstErr
	}
	return out, nil
}

func extractAliyunKeyMaterial(content []byte) (string, string) {
	decoded := safeB64Decode(strings.TrimSpace(string(content)))
	if len(decoded) == 0 {
		return "", ""
	}
	m := aliyunMediaTokenRe.FindStringSubmatch(string(decoded))
	if len(m) != 3 {
		return "", ""
	}
	return m[1], m[2]
}

func (x *hyCtx) requestAliyunLicense(payload map[string]string, mediaID, challenge, encType string) ([]byte, error) {
	key := mediaID + ":" + challenge
	if cached, ok := x.licenseCache[key]; ok {
		return cached, nil
	}
	licenseURL := fmt.Sprintf("https://mts.%s.aliyuncs.com/?", payload["domain_region"])
	params := map[string]string{"data": challenge, "SignatureNonce": aliyunNonce(), "SignatureVersion": "1.0", "SignatureMethod": "HMAC-SHA1", "Version": "2014-06-18", "Type": encType, "Format": "JSON", "SecurityToken": payload["sts_token"], "LicenseUrl": licenseURL, "MediaId": mediaID, "Action": "GetLicense", "AccessKeyId": payload["access_id"]}
	sig := aliyunSignature(params, payload["access_secret"], "POST")
	body, err := x.c.PostForm(licenseURL+"Signature="+aliyunEncodeURI(sig), params, map[string]string{"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8", "Accept": "application/json, text/plain, */*", "Referer": referer, "Origin": origin})
	if err != nil {
		return nil, err
	}
	var root map[string]any
	_ = json.Unmarshal([]byte(body), &root)
	license := firstString(root, "License", "license")
	if license == "" {
		license = firstString(asMap(root["Data"]), "License", "license", "data")
	}
	if license == "" {
		return nil, fmt.Errorf("empty aliyun license")
	}
	out := safeB64Decode(license)
	if len(out) == 0 {
		out = []byte(license)
	}
	x.licenseCache[key] = out
	return out, nil
}

func aliyunEncodeURI(s string) string {
	r := url.QueryEscape(s)
	r = strings.ReplaceAll(r, "+", "%20")
	r = strings.ReplaceAll(r, "%7E", "~")
	return r
}
func aliyunSortedQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, aliyunEncodeURI(k)+"="+aliyunEncodeURI(params[k]))
	}
	return strings.Join(parts, "&")
}
func aliyunSignature(params map[string]string, secret, method string) string {
	qs := aliyunSortedQuery(params)
	sts := strings.ToUpper(method) + "&" + aliyunEncodeURI("/") + "&" + aliyunEncodeURI(qs)
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(sts))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
func aliyunNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func safeB64Decode(s string) []byte {
	s = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "-", "+"), "_", "/")
	if s == "" {
		return nil
	}
	s += strings.Repeat("=", (4-len(s)%4)%4)
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

type xmlNode struct {
	XMLName xml.Name
	Text    string    `xml:",chardata"`
	Nodes   []xmlNode `xml:",any"`
}

func parseAliyunPlayXML(text string) map[string]any {
	var n xmlNode
	if err := xml.Unmarshal([]byte(text), &n); err != nil {
		return map[string]any{}
	}
	return xmlNodeMap(n)
}
func xmlNodeMap(n xmlNode) map[string]any {
	out := map[string]any{}
	for _, c := range n.Nodes {
		name := c.XMLName.Local
		var v any
		if len(c.Nodes) > 0 {
			v = xmlNodeMap(c)
		} else {
			v = strings.TrimSpace(c.Text)
		}
		if old, ok := out[name]; ok {
			out[name] = append(anyList(old), v)
		} else {
			out[name] = v
		}
	}
	return out
}
