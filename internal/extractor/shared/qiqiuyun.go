package shared

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/util"
)

// QiqiuyunM3U8Options controls qiqiuyun HLS preparation.
type QiqiuyunM3U8Options struct {
	Headers map[string]string
	Referer string
	Cookie  string
	Version int
	Mode    int
}

// QiqiuyunM3U8Result contains the rewritten playlist and metadata needed by downloaders.
type QiqiuyunM3U8Result struct {
	URL       string
	Text      string
	SourceURL string
	Meta      map[string]any
}

var (
	qiqiuyunKeyURIRe = regexp.MustCompile(`URI="([^"]+)"`)
	qiqiuyunIVRe     = regexp.MustCompile(`IV\s*=\s*0x([0-9a-fA-F]+)`)
)

// PrepareQiqiuyunM3U8 fetches a qiqiuyun playlist, selects a variant, absolutizes
// segment/key URLs, decodes qiqiuyun-encrypted keys, and returns a data URL
// suitable for HLS downloaders.
func PrepareQiqiuyunM3U8(c *util.Client, rawURL string, opts QiqiuyunM3U8Options) QiqiuyunM3U8Result {
	headers := qiqiuyunHeaders(opts)
	b, finalURL, ok := qiqiuyunFetchBinary(c, rawURL, rawURL, headers)
	if !ok {
		return QiqiuyunM3U8Result{}
	}
	text := string(b)
	baseURL := finalURL
	if variant := qiqiuyunSelectM3U8Variant(text, finalURL, opts.Mode); variant != "" {
		if vb, variantURL, ok := qiqiuyunFetchBinary(c, variant, finalURL, headers); ok && strings.HasPrefix(strings.TrimSpace(string(vb)), "#EXTM3U") {
			text, baseURL = string(vb), variantURL
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(text), "#EXTM3U") {
		return QiqiuyunM3U8Result{}
	}
	prepared, meta := qiqiuyunRewriteM3U8(c, text, baseURL, headers, opts.Version)
	if prepared == "" {
		return QiqiuyunM3U8Result{}
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["version"] = opts.Version
	meta["source_url"] = baseURL
	meta["bytes"] = len(prepared)
	return QiqiuyunM3U8Result{URL: M3U8DataURL(prepared), Text: prepared, SourceURL: baseURL, Meta: meta}
}

// DecodeQiqiuyunKey mirrors qiqiuyun getKey_qiqiuyun for the key formats used by
// Unipus and Wallstreets playlists.
func DecodeQiqiuyunKey(content []byte, version int) []byte {
	trimmedBytes := bytes.TrimSpace(content)
	if len(trimmedBytes) == 16 || len(trimmedBytes) == 24 || len(trimmedBytes) == 32 {
		return append([]byte(nil), trimmedBytes...)
	}
	decoded := decryptQiqiuyun(trimmedBytes, version)
	if len(decoded) == 16 || len(decoded) == 24 || len(decoded) == 32 {
		return decoded
	}
	return nil
}

// M3U8DataURL wraps a playlist as a downloader-consumable data URL.
func M3U8DataURL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}

func qiqiuyunHeaders(opts QiqiuyunM3U8Options) map[string]string {
	headers := make(map[string]string, len(opts.Headers)+4)
	for k, v := range opts.Headers {
		headers[k] = v
	}
	if opts.Referer != "" {
		headers["Referer"] = opts.Referer
	}
	if opts.Cookie != "" {
		headers["Cookie"] = opts.Cookie
		headers["cookie"] = opts.Cookie
	}
	return headers
}

func qiqiuyunFetchBinary(c *util.Client, rawURL, baseURL string, headers map[string]string) ([]byte, string, bool) {
	if c == nil {
		c = util.NewClient()
	}
	finalURL := qiqiuyunResolveM3U8URL(rawURL, baseURL)
	if data, ok := qiqiuyunDecodeDataURLBytes(finalURL); ok {
		return data, finalURL, true
	}
	resp, err := c.Get(finalURL, headers)
	if err != nil {
		return nil, "", false
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if resp.StatusCode >= 400 {
		return nil, finalURL, false
	}
	b, err := io.ReadAll(resp.Body)
	return b, finalURL, err == nil
}

func qiqiuyunSelectM3U8Variant(masterText, masterURL string, mode int) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(masterText, "\r\n", "\n"), "\r", "\n"), "\n")
	type candidate struct {
		url       string
		bandwidth int
	}
	var variants []candidate
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			continue
		}
		bw, _ := strconv.Atoi(qiqiuyunMatch1(line, regexp.MustCompile(`BANDWIDTH=(\d+)`)))
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" || strings.HasPrefix(next, "#") {
				continue
			}
			variants = append(variants, candidate{url: qiqiuyunResolveM3U8URL(next, masterURL), bandwidth: bw})
			break
		}
	}
	if len(variants) == 0 {
		return ""
	}
	sort.SliceStable(variants, func(i, j int) bool { return variants[i].bandwidth > variants[j].bandwidth })
	idx := mode - 1
	if idx < 0 {
		idx = 0
	} else if idx >= len(variants) {
		idx = len(variants) - 1
	}
	return variants[idx].url
}

func qiqiuyunRewriteM3U8(c *util.Client, text, baseURL string, headers map[string]string, version int) (string, map[string]any) {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	out := make([]string, 0, len(lines))
	segmentMap := map[int]map[string]any{}
	keyCache := map[string][]byte{}
	meta := map[string]any{}
	var curKey, curIV []byte
	segIndex := 0
	awaitingSegment := false

	for _, line := range lines {
		trim := strings.TrimSpace(line)
		upper := strings.ToUpper(trim)
		if strings.HasPrefix(upper, "#EXT-X-KEY") {
			out = append(out, line)
			if strings.Contains(upper, "METHOD=NONE") {
				curKey, curIV = nil, nil
				continue
			}
			if keyURI := qiqiuyunMatch1(trim, qiqiuyunKeyURIRe); keyURI != "" {
				keyURL := qiqiuyunResolveM3U8URL(keyURI, baseURL)
				key := keyCache[keyURL]
				if len(key) == 0 {
					if kb, _, ok := qiqiuyunFetchBinary(c, keyURL, baseURL, headers); ok {
						meta["key_uri"] = keyURL
						meta["key_bytes"] = len(kb)
						meta["key_decode"] = "qiqiuyun_key_decode"
						key = DecodeQiqiuyunKey(kb, version)
						keyCache[keyURL] = key
					}
				}
				curKey = key
				if len(key) > 0 {
					out[len(out)-1] = strings.Replace(line, keyURI, qiqiuyunKeyDataURL(key), 1)
				} else {
					out[len(out)-1] = strings.Replace(line, keyURI, keyURL, 1)
				}
			}
			curIV = qiqiuyunM3U8IVBytes(qiqiuyunMatch1(trim, qiqiuyunIVRe))
			continue
		}
		if strings.HasPrefix(upper, "#EXT-X-MAP") {
			out = append(out, qiqiuyunRewriteM3U8URI(line, baseURL))
			continue
		}
		out = append(out, line)
		if strings.HasPrefix(trim, "#EXTINF") {
			awaitingSegment = true
			continue
		}
		if awaitingSegment && trim != "" && !strings.HasPrefix(trim, "#") {
			out[len(out)-1] = qiqiuyunResolveM3U8URL(trim, baseURL)
			if len(curKey) > 0 {
				m := map[string]any{"key": append([]byte(nil), curKey...)}
				if len(curIV) > 0 {
					m["iv"] = append([]byte(nil), curIV...)
				}
				segmentMap[segIndex] = m
			}
			segIndex++
			awaitingSegment = false
		}
	}
	prepared := strings.Join(out, "\n")
	if len(segmentMap) > 0 {
		meta["cryptor"] = map[string]any{"_lel_cryptor_type": "aes_cbc_segment_map", "segments": segmentMap}
	}
	if len(meta) == 0 {
		return prepared, nil
	}
	return prepared, meta
}

func qiqiuyunRewriteM3U8URI(line, baseURL string) string {
	if keyURI := qiqiuyunMatch1(line, qiqiuyunKeyURIRe); keyURI != "" {
		return strings.Replace(line, keyURI, qiqiuyunResolveM3U8URL(keyURI, baseURL), 1)
	}
	return line
}

func qiqiuyunResolveM3U8URL(raw, base string) string {
	s := strings.TrimSpace(raw)
	low := strings.ToLower(s)
	if s == "" || strings.HasPrefix(low, "data:") || strings.HasPrefix(low, "0x") {
		return s
	}
	return qiqiuyunAbsURL(s, base)
}

func qiqiuyunDecodeDataURLBytes(raw string) ([]byte, bool) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:") {
		return nil, false
	}
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return nil, false
	}
	meta, payload := raw[:comma], raw[comma+1:]
	if strings.Contains(strings.ToLower(meta), ";base64") {
		b, err := base64.StdEncoding.DecodeString(payload)
		return b, err == nil
	}
	decoded, err := url.PathUnescape(payload)
	if err != nil {
		return nil, false
	}
	return []byte(decoded), true
}

func decryptQiqiuyun(in []byte, version int) []byte {
	if len(in) == 17 {
		return qiqiuyunPick(in[1:], "8-9-2-3-4-5-6-7-0-1-10-11-12-13-14-15")
	}
	if len(in) == 20 {
		switch version {
		case 1:
			return decryptQiqiuyun20(in, 7, [4][2]int{{8, 9}, {10, 11}, {15, 16}, {17, 18}}, []int{0, 1, 2, 3, 4, 5, 6, 7, -1, -2, 12, 13, 14, -3, -4, 19}, "0-1-2-3-4-5-6-7-18-16-15-13-12-11-10-8", "0-1-2-3-4-5-6-7-8-10-11-12-14-15-16-18")
		case 2:
			return decryptQiqiuyun20(in, 2, [4][2]int{{3, 4}, {8, 9}, {14, 15}, {18, 19}}, []int{0, 1, 2, -1, 5, 6, 7, -2, 10, 11, 12, 13, -3, 16, 17, -4}, "0-1-2-3-4-12-13-14-7-6-18-17-15-8-9-10", "0-1-2-12-13-14-15-16-17-18-4-5-6-7-9-10")
		case 3:
			return decryptQiqiuyun20(in, 2, [4][2]int{{5, 6}, {9, 10}, {13, 14}, {17, 18}}, []int{0, 1, 2, 3, 4, -1, 7, 8, -2, 11, 12, -3, 15, 16, -4, 19}, "0-1-2-8-9-10-11-12-18-17-16-15-14-4-5-6", "0-1-2-3-4-15-16-17-18-10-11-12-13-6-7-8")
		}
	}
	if len(in) == 21 {
		if order := qiqiuyunOrder21(version, in[20]); order != "" {
			return qiqiuyunPick(in, order)
		}
	}
	return append([]byte(nil), in...)
}

func decryptQiqiuyun20(in []byte, mod int, pairs [4][2]int, layout []int, order1, order0 string) []byte {
	if len(in) != 20 {
		return nil
	}
	n := qiqiuyunParseBase36Char(in[0]) % mod
	u := qiqiuyunParseBase36Pair(in[n], in[n+1]) % 3
	switch u {
	case 2:
		vals := make([]byte, 4)
		for i, pair := range pairs {
			digit, ok := qiqiuyunParseDigit(in[pair[1]])
			if !ok {
				vals[i] = 0
				continue
			}
			offset := 1
			if i == 3 {
				offset = 2
			}
			vals[i] = byte(int(in[pair[0]]) - 97 + 26*(digit+offset) - 97)
		}
		out := make([]byte, 16)
		for i, spec := range layout {
			switch spec {
			case -1:
				out[i] = vals[0]
			case -2:
				out[i] = vals[1]
			case -3:
				out[i] = vals[2]
			case -4:
				out[i] = vals[3]
			default:
				out[i] = in[spec]
			}
		}
		return out
	case 1:
		return qiqiuyunPick(in, order1)
	case 0:
		return qiqiuyunPick(in, order0)
	default:
		return nil
	}
}

func qiqiuyunOrder21(version int, tag byte) string {
	orders := map[int]map[byte]string{
		4:  {'0': "4-13-19-14-0-3-16-18-9-2-11-6-8-1-5-12", '1': "7-15-13-4-0-9-12-17-14-16-18-3-6-11-8-2", '2': "17-0-13-3-11-6-15-2-12-14-4-1-18-10-7-19"},
		5:  {'0': "18-5-6-4-16-2-12-0-15-7-9-3-13-19-17-14", '1': "1-15-0-18-7-2-5-4-11-13-14-10-12-17-6-8", '2': "19-2-12-1-18-13-5-8-7-6-9-11-0-17-10-16"},
		6:  {'0': "7-8-4-0-10-14-15-16-5-18-12-1-6-13-17-9", '1': "15-0-12-2-6-10-16-11-19-18-4-17-5-8-14-1", '2': "16-18-13-6-17-0-15-3-19-10-11-12-5-2-14-4"},
		7:  {'0': "2-12-16-7-3-4-18-19-8-1-15-13-9-5-17-10", '1': "1-6-9-15-8-14-16-12-3-13-4-18-5-17-7-2", '2': "15-7-10-8-17-4-14-6-18-11-0-9-12-5-3-16"},
		8:  {'0': "2-17-1-11-19-13-6-3-18-4-9-16-12-15-7-0", '1': "9-4-17-1-8-2-12-0-6-7-14-19-5-11-15-13", '2': "16-8-5-3-1-6-11-12-2-7-13-0-19-18-9-4"},
		9:  {'0': "5-19-15-1-17-13-12-10-16-14-7-0-18-2-6-9", '1': "13-0-5-4-1-19-2-12-18-14-9-17-10-15-8-16", '2': "12-8-0-9-19-5-1-16-10-18-13-6-17-7-15-14"},
		10: {'0': "9-10-3-19-1-12-6-4-18-14-16-15-5-2-17-8", '1': "12-13-14-3-17-19-15-7-5-10-9-6-16-11-1-0", '2': "19-15-0-4-12-1-3-2-18-7-10-6-11-14-13-5"},
	}
	if byTag, ok := orders[version]; ok {
		return byTag[tag]
	}
	return ""
}

func qiqiuyunPick(in []byte, order string) []byte {
	if order == "" {
		return nil
	}
	parts := strings.Split(order, "-")
	out := make([]byte, 0, len(parts))
	for _, p := range parts {
		i, err := strconv.Atoi(p)
		if err != nil || i < 0 || i >= len(in) {
			return nil
		}
		out = append(out, in[i])
	}
	return out
}

func qiqiuyunParseBase36Char(b byte) int {
	s := strings.ToLower(string([]byte{b}))
	n, err := strconv.ParseInt(s, 36, 64)
	if err != nil {
		return 0
	}
	return int(n)
}

func qiqiuyunParseBase36Pair(a, b byte) int {
	n, err := strconv.ParseInt(string([]byte{a, b}), 36, 64)
	if err != nil {
		return 0
	}
	return int(n)
}

func qiqiuyunParseDigit(b byte) (int, bool) {
	n, err := strconv.Atoi(string([]byte{b}))
	if err != nil {
		return 0, false
	}
	return n, true
}

func qiqiuyunM3U8IVBytes(hexText string) []byte {
	if hexText == "" {
		return nil
	}
	iv, err := hex.DecodeString(hexText)
	if err != nil {
		return nil
	}
	if len(iv) == 16 {
		return iv
	}
	if len(iv) > 16 {
		return iv[len(iv)-16:]
	}
	out := make([]byte, 16)
	copy(out[16-len(iv):], iv)
	return out
}

func qiqiuyunKeyDataURL(key []byte) string {
	return "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(key)
}

func qiqiuyunAbsURL(raw, base string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	bu, err := url.Parse(base)
	if err != nil {
		return s
	}
	return bu.ResolveReference(u).String()
}

func qiqiuyunMatch1(s string, re *regexp.Regexp) string {
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}
