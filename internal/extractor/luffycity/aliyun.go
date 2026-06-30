package luffycity

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

func luffyDecodeAliyunPlayAuth(v any) map[string]any {
	var raw map[string]any
	switch x := v.(type) {
	case map[string]any:
		raw = x
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return map[string]any{}
		}
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			_ = json.Unmarshal([]byte(s), &raw)
		} else if b, err := base64.StdEncoding.DecodeString(padB64(strings.ReplaceAll(strings.ReplaceAll(s, "-", "+"), "_", "/"))); err == nil {
			_ = json.Unmarshal(b, &raw)
		}
	}
	if raw == nil {
		raw = map[string]any{}
	}
	out := map[string]any{
		"raw":           raw,
		"auth_timeout":  firstText(raw["auth_timeout"], raw["AuthTimeout"], "7200"),
		"auth_info":     firstText(raw["auth_info"], raw["AuthInfo"]),
		"domain_region": firstText(raw["region"], raw["Region"], raw["regionId"], raw["RegionId"]),
		"sts_token":     firstText(raw["securityToken"], raw["SecurityToken"]),
		"access_secret": firstText(raw["accessSecret"], raw["AccessKeySecret"]),
		"access_id":     firstText(raw["accessId"], raw["AccessKeyId"]),
	}
	return out
}

func luffyResolveAliyun(c *util.Client, sess *luffySession, authInfo map[string]any) luffySource {
	videoID := firstText(authInfo["videoId"], authInfo["vid"], authInfo["video_id"])
	if videoID == "" {
		return luffySource{}
	}
	playAuth := authInfo["play_auth"]
	if playAuth == nil {
		playAuth = authInfo["playAuth"]
	}
	if playAuth == nil || firstText(playAuth) == "" {
		if media := luffyGetData(c, fmt.Sprintf("/media/play/%s/", videoID), nil, sess.Headers); true {
			if m := mapAny(media); len(m) > 0 {
				if pa := m["playauth"]; pa != nil {
					playAuth = pa
				}
				if pa := m["play_auth"]; pa != nil {
					playAuth = pa
				}
				if pa := m["playAuth"]; pa != nil {
					playAuth = pa
				}
				if vr := firstText(m["regionId"], m["region"]); vr != "" && authInfo["regionId"] == nil && authInfo["region"] == nil {
					authInfo["regionId"] = vr
				}
			}
		}
	}
	payload := luffyDecodeAliyunPlayAuth(playAuth)
	if r := firstText(authInfo["regionId"], authInfo["region"], payload["domain_region"]); r != "" {
		payload["domain_region"] = r
	}
	if sharedSrc := luffyResolveAliyunShared(c, sess, payload, videoID); sharedSrc.URL != "" {
		return sharedSrc
	}
	resp := luffyRequestAliyunPlayInfo(c, sess, payload, videoID)
	if src := luffyExtractAliyunPlayResponse(resp); src.URL != "" {
		return src
	}
	return luffySource{}
}

func luffyResolveAliyunShared(c *util.Client, sess *luffySession, payload map[string]any, videoID string) luffySource {
	p := shared.AliyunPlayPayload{
		AccessKeyID:     firstText(payload["access_id"]),
		AccessKeySecret: firstText(payload["access_secret"]),
		SecurityToken:   firstText(payload["sts_token"]),
		Region:          firstText(payload["domain_region"]),
		AuthInfo:        firstText(payload["auth_info"]),
		AuthTimeout:     firstText(payload["auth_timeout"], "7200"),
		Raw:             payload["raw"],
	}
	if p.AccessKeyID == "" || p.AccessKeySecret == "" || p.SecurityToken == "" || p.Region == "" || p.AuthInfo == "" {
		return luffySource{}
	}
	opts := shared.AliyunPlayOptions{
		Referer:           urlReferer,
		Origin:            urlOrigin,
		Quality:           "",
		FetchM3U8:         true,
		RewriteM3U8Keys:   true,
		PreferDefinitions: []string{"4K", "2K", "OD", "FHD", "HD", "SD", "LD", "FD"},
		Headers: map[string]string{
			"Accept":     "application/json, text/plain, */*",
			"Referer":    urlReferer,
			"Origin":     urlOrigin,
			"User-Agent": sess.Headers["User-Agent"],
		},
	}
	info, err := shared.AliyunResolvePlayInfo(c, p, videoID, opts)
	if err != nil || info == nil || info.URL == "" {
		return luffySource{}
	}
	sourceURL := info.URL
	sourceType := firstText(info.SourceType, info.Format, mediaExt(info.URL))
	extra := map[string]any{"aliyun_api": info.APIURL, "source_type": sourceType, "definition": info.Definition}
	if info.M3U8Text != "" {
		extra["m3u8_text"] = info.M3U8Text
		sourceURL = luffyM3U8DataURL(info.M3U8Text)
		sourceType = "m3u8_text"
		extra["source_type"] = sourceType
	}
	return luffySource{URL: sourceURL, Type: sourceType, Size: info.Size, Extra: extra}
}

func luffyRequestAliyunPlayInfo(c *util.Client, sess *luffySession, payload map[string]any, videoID string) map[string]any {
	accessID := firstText(payload["access_id"])
	accessSecret := firstText(payload["access_secret"])
	stsToken := firstText(payload["sts_token"])
	domainRegion := firstText(payload["domain_region"])
	authInfo := firstText(payload["auth_info"])
	if accessID == "" || accessSecret == "" || stsToken == "" || domainRegion == "" || authInfo == "" || videoID == "" {
		return map[string]any{}
	}
	params := map[string]string{
		"Action":           "GetPlayInfo",
		"AccessKeyId":      accessID,
		"AuthInfo":         authInfo,
		"AuthTimeout":      firstText(payload["auth_timeout"], "7200"),
		"Definition":       "FD,LD,SD,HD,OD,2K,4K",
		"Formats":          "m3u8,mp4",
		"ResultType":       "Multiple",
		"SecurityToken":    stsToken,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   luffyNonce(),
		"SignatureVersion": "1.0",
		"Version":          "2017-03-21",
		"VideoId":          videoID,
	}
	signature := luffyAliyunSignature(params, accessSecret, "GET")
	full := "https://vod." + domainRegion + ".aliyuncs.com/?" + luffyAliyunSortedQuery(mergeStringMap(params, map[string]string{"Signature": signature}))
	body, err := c.GetString(full, map[string]string{"Accept": "application/json, text/plain, */*", "Referer": urlReferer, "Origin": urlOrigin, "User-Agent": sess.Headers["User-Agent"]})
	if err != nil {
		return map[string]any{}
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return map[string]any{}
	}
	return resp
}

func luffyExtractAliyunPlayResponse(info map[string]any) luffySource {
	pis := mapAny(nested(info, "PlayInfoList"))
	items := records(pis["PlayInfo"])
	if len(items) == 0 {
		return luffySource{}
	}
	rank := map[string]int{"4K": 1, "2K": 2, "OD": 3, "FHD": 4, "HD": 5, "SD": 6, "LD": 7, "FD": 8}
	sort.SliceStable(items, func(i, j int) bool {
		return aliyunItemRank(items[i], rank) < aliyunItemRank(items[j], rank)
	})
	for _, item := range items {
		u := firstText(item["PlayURL"], item["PlayUrl"])
		if u == "" {
			continue
		}
		return luffySource{URL: u, Type: mediaExt(u), Size: int64(numOf(item["Size"]))}
	}
	return luffySource{}
}

func aliyunItemRank(item map[string]any, rank map[string]int) int {
	def := strings.ToUpper(firstText(item["Definition"]))
	if def == "" {
		def = "HD"
	}
	if r, ok := rank[def]; ok {
		return r
	}
	return 99
}

func luffyAliyunSignature(params map[string]string, accessSecret, method string) string {
	qs := luffyAliyunSortedQuery(params)
	stringToSign := strings.ToUpper(method) + "&" + luffyAliyunEncodeURI("/") + "&" + luffyAliyunEncodeURI(qs)
	mac := hmac.New(sha1.New, []byte(accessSecret+"&"))
	_, _ = mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func luffyAliyunSortedQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, luffyAliyunEncodeURI(k)+"="+luffyAliyunEncodeURI(params[k]))
	}
	return strings.Join(parts, "&")
}

func luffyAliyunEncodeURI(v string) string {
	escaped := url.QueryEscape(v)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}
func luffyNonce() string { return fmt.Sprintf("%d", time.Now().UnixNano()) }
func padB64(s string) string {
	switch len(s) % 4 {
	case 2:
		return s + "=="
	case 3:
		return s + "="
	case 1:
		return s + "==="
	default:
		return s
	}
}
func mergeStringMap(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
