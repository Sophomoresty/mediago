package xsteach

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type qcloudPlayInfo struct {
	MasterURL  string
	DRMToken   string
	OverlayIV  string
	OverlayKey string
	SubStreams []map[string]any
	Size       int64
	Raw        map[string]any
}

func qcloudMediaSource(c *util.Client, auth map[string]string, quality string) xsSource {
	overlayKey, overlayIV := newOverlayText(), newOverlayText()
	q := url.Values{"keyId": {"1"}, "psign": {auth["psign"]}}
	if iv := rsaEncryptOverlay(overlayIV); iv != "" {
		q.Set("cipheredOverlayIv", iv)
	}
	if key := rsaEncryptOverlay(overlayKey); key != "" {
		q.Set("cipheredOverlayKey", key)
	}
	body, err := c.GetString(qcloudURL(auth["app_id"], auth["file_id"])+"?"+q.Encode(), map[string]string{"Accept": "*/*", "Referer": refererURL, "User-Agent": defaultUserAgent})
	if err != nil {
		return xsSource{}
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil || !codeIs(root["code"], 0) {
		return xsSource{}
	}
	info := parseQcloudPlayInfo(root, overlayKey, overlayIV)
	extra := map[string]any{"source_type": "qcloud", "app_id": auth["app_id"], "file_id": auth["file_id"]}
	if info.DRMToken != "" {
		extra["drm_token"] = info.DRMToken
	}
	if info.Size > 0 {
		extra["size"] = info.Size
	}
	if info.MasterURL != "" {
		if finalURL, text := loadFinalQcloudM3U8(c, info, xsteachTargetQualityRank(quality)); finalURL != "" {
			if text != "" {
				extra["m3u8_text"] = text
				extra["source_type"] = "m3u8_text"
			}
			return xsSource{URL: finalURL, Extra: extra}
		}
		return xsSource{URL: info.MasterURL, Extra: extra}
	}
	if u := firstMediaURL(root); u != "" {
		return xsSource{URL: u, Extra: extra}
	}
	return xsSource{}
}

func parseQcloudPlayInfo(root map[string]any, overlayKey, overlayIV string) qcloudPlayInfo {
	info := qcloudPlayInfo{OverlayKey: overlayKey, OverlayIV: overlayIV, Raw: root}
	for _, m := range mapsUnder(root) {
		if info.MasterURL == "" {
			if ml, ok := m["masterPlayList"].(map[string]any); ok {
				info.MasterURL = normalizeURL(val(ml, "url"))
			}
		}
		if info.MasterURL == "" {
			info.MasterURL = normalizeURL(val(m, "url"))
		}
		if info.DRMToken == "" {
			info.DRMToken = firstNonEmpty(val(m, "drmToken"), val(m, "drm_token"))
		}
		if info.Size == 0 {
			info.Size = int64(number(firstNonEmpty(val(m, "size"), val(m, "fileSize"), val(m, "videoSize"))))
		}
		if len(info.SubStreams) == 0 {
			info.SubStreams = listUnder(m, "subStreams")
		}
	}
	return info
}

func loadFinalQcloudM3U8(c *util.Client, info qcloudPlayInfo, targetRank int) (string, string) {
	if info.MasterURL == "" {
		return "", ""
	}
	h := map[string]string{"Accept": "*/*", "Referer": refererURL, "User-Agent": defaultUserAgent}
	master, err := c.GetString(info.MasterURL, h)
	if err != nil || !strings.Contains(master, "#EXTM3U") {
		return info.MasterURL, ""
	}
	variantURL, size := selectQcloudVariant(master, info.MasterURL, info, targetRank)
	if variantURL == "" {
		variantURL = info.MasterURL
	}
	text := master
	sourceURL := info.MasterURL
	if variantURL != info.MasterURL {
		if body, err := c.GetString(variantURL, h); err == nil && strings.Contains(body, "#EXTM3U") {
			text = body
			sourceURL = variantURL
		}
	}
	rewritten := rewriteQcloudM3U8(text, sourceURL, info.DRMToken)
	if rewritten == "" {
		return variantURL, ""
	}
	if size > 0 {
		info.Size = size
	}
	return dataM3U8URL(rewritten), rewritten
}

type qcloudVariant struct {
	URL       string
	Bandwidth int64
	Rank      int64
	Size      int64
}

func selectQcloudVariant(masterText, masterURL string, info qcloudPlayInfo, targetRank int) (string, int64) {
	lines := strings.Split(masterText, "\n")
	variants := []qcloudVariant{}
	streamIndex := 0
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			continue
		}
		v := qcloudVariant{Bandwidth: attrInt(line, "BANDWIDTH")}
		if m := regexp.MustCompile(`RESOLUTION=\d+x(\d+)`).FindStringSubmatch(line); len(m) > 1 {
			v.Rank = int64(number(m[1]))
		}
		if streamIndex < len(info.SubStreams) {
			s := info.SubStreams[streamIndex]
			v.Rank = firstInt(v.Rank, int64(number(firstNonEmpty(val(s, "height"), val(s, "resolution"), val(s, "resolutionName")))))
			v.Size = int64(number(val(s, "size")))
		}
		streamIndex++
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" || strings.HasPrefix(next, "#") {
				continue
			}
			v.URL = resolveAgainst(next, masterURL)
			break
		}
		if v.URL != "" {
			variants = append(variants, v)
		}
	}
	if len(variants) == 0 {
		return masterURL, info.Size
	}
	sort.SliceStable(variants, func(i, j int) bool {
		if variants[i].Rank == variants[j].Rank {
			return variants[i].Bandwidth > variants[j].Bandwidth
		}
		return variants[i].Rank > variants[j].Rank
	})
	if targetRank > 0 {
		for _, variant := range variants {
			if variant.Rank > 0 && variant.Rank <= int64(targetRank) {
				return variant.URL, variant.Size
			}
		}
		if targetRank <= 480 {
			lowest := variants[len(variants)-1]
			return lowest.URL, lowest.Size
		}
	}
	return variants[0].URL, variants[0].Size
}

var uriRe = regexp.MustCompile(`URI="([^"]+)"`)

func rewriteQcloudM3U8(text, sourceURL, drmToken string) string {
	if !strings.Contains(text, "#EXTM3U") {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, "URI=") {
				lines[i] = uriRe.ReplaceAllStringFunc(line, func(match string) string {
					m := uriRe.FindStringSubmatch(match)
					if len(m) < 2 {
						return match
					}
					u := resolveAgainst(m[1], sourceURL)
					if strings.Contains(trimmed, "#EXT-X-KEY") {
						u = appendToken(u, drmToken)
					}
					return `URI="` + u + `"`
				})
			}
			continue
		}
		lines[i] = resolveAgainst(trimmed, sourceURL)
	}
	return strings.Join(lines, "\n")
}

func appendToken(raw, token string) string {
	raw, token = strings.TrimSpace(raw), strings.TrimSpace(token)
	if raw == "" || token == "" || strings.Contains(raw, "token=") {
		return raw
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	return raw + sep + "token=" + url.QueryEscape(token)
}

func resolveAgainst(raw, base string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") {
		return raw
	}
	b, err := url.Parse(base)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return b.ResolveReference(ref).String()
}

func attrInt(line, key string) int64 {
	m := regexp.MustCompile(key + `=(\d+)`).FindStringSubmatch(line)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	return n
}

func firstInt(vals ...int64) int64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func dataM3U8URL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}

func mediaSource(title string, src xsSource, vi xsVideo) *extractor.MediaInfo {
	entry := media(title, src.URL, vi)
	if entry.Extra == nil {
		entry.Extra = map[string]any{}
	}
	for k, v := range src.Extra {
		entry.Extra[k] = v
	}
	if stream, ok := entry.Streams["default"]; ok {
		stream.NeedMerge = stream.Format == "m3u8"
		entry.Streams["default"] = stream
	}
	return entry
}

func uniqueSources(in []xsSource) []xsSource {
	out := []xsSource{}
	seen := map[string]bool{}
	for _, src := range in {
		if src.URL == "" || seen[src.URL] {
			continue
		}
		seen[src.URL] = true
		out = append(out, src)
	}
	return out
}
