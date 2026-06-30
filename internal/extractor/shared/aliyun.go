// Aliyun VOD/MTS helpers shared by course sites that obtain short-lived STS
// credentials from their parent API and then call Aliyun's signed OpenAPI
// directly. The flow mirrors the decompiled Wangxiao233/Haiyangknow/Wowtiku
// sources:
//
//  1. Decode playAuth or map parent STS fields to AccessKeyId/Secret/Token.
//  2. Sign vod.{region}.aliyuncs.com GetPlayInfo with HMAC-SHA1.
//  3. Parse JSON or XML PlayInfoList and pick a playable m3u8/mp4 variant.
//  4. For AliyunVoDEncryption keys, fetch MTS GetLicense and inline key hex
//     into the prepared manifest when the caller asks for m3u8 text.
package shared

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

	"github.com/Sophomoresty/mediago/internal/util"
)

var aliyunMediaTokenRe = regexp.MustCompile(`(?i)^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(.+)$`)

// AliyunPlayPayload contains the STS material needed by GetPlayInfo/GetLicense.
type AliyunPlayPayload struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	Region          string
	AuthInfo        string
	AuthTimeout     string
	Raw             any
}

// AliyunPlayOptions controls source-specific request details while keeping the
// signing/parsing code byte-for-byte aligned with the Python sources.
type AliyunPlayOptions struct {
	Referer           string
	Origin            string
	Quality           string
	Definitions       string
	Formats           string
	AuthTimeout       string
	PreferDefinitions []string
	ExtraParams       map[string]string
	Headers           map[string]string
	FetchM3U8         bool
	RewriteM3U8Keys   bool
}

// AliyunPlayInfo is the resolved Aliyun source and optional prepared manifest.
type AliyunPlayInfo struct {
	URL          string
	Format       string
	NeedMerge    bool
	Size         int64
	Definition   string
	Encrypted    bool
	EncryptType  string
	SourceType   string
	APIURL       string
	M3U8Text     string
	PlayResponse map[string]any
}

// AliyunDecodePlayAuth accepts either a dict-like value, a JSON string, or a
// base64-encoded JSON playAuth string returned by parent APIs.
func AliyunDecodePlayAuth(value any) AliyunPlayPayload {
	var m map[string]any
	switch t := value.(type) {
	case map[string]any:
		m = t
	case string:
		s := strings.TrimSpace(t)
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			_ = json.Unmarshal([]byte(s), &m)
		} else if b := aliyunSafeB64Decode(s); len(b) > 0 {
			_ = json.Unmarshal(b, &m)
		}
	}
	return AliyunPayloadFromMap(m, value)
}

// AliyunPayloadFromMap normalizes common parent API field names to STS payload.
func AliyunPayloadFromMap(m map[string]any, raw any) AliyunPlayPayload {
	if m == nil {
		return AliyunPlayPayload{Raw: raw}
	}
	return AliyunPlayPayload{
		AccessKeyID:     firstAnyString(m, "AccessKeyId", "accessKeyId", "access_id", "ky"),
		AccessKeySecret: firstAnyString(m, "AccessKeySecret", "accessKeySecret", "access_secret", "sc"),
		SecurityToken:   firstAnyString(m, "SecurityToken", "securityToken", "sts_token", "tk"),
		Region:          firstAnyString(m, "Region", "region", "regionId", "domain_region"),
		AuthInfo:        firstAnyString(m, "AuthInfo", "authInfo", "auth_info"),
		AuthTimeout:     firstNonEmptyShared(firstAnyString(m, "AuthTimeout", "authTimeout", "auth_timeout"), "7200"),
		Raw:             raw,
	}
}

// AliyunResolvePlayInfo signs GetPlayInfo, parses the response, and optionally
// prepares/rekeys the m3u8 text. It returns an error instead of a fabricated URL
// when the STS material or encrypted key chain is incomplete.
func AliyunResolvePlayInfo(c *util.Client, payload AliyunPlayPayload, videoID string, opts AliyunPlayOptions) (*AliyunPlayInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("aliyun: nil client")
	}
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return nil, fmt.Errorf("aliyun: missing video id")
	}
	if payload.AccessKeyID == "" || payload.AccessKeySecret == "" || payload.Region == "" {
		return nil, fmt.Errorf("aliyun: incomplete STS payload")
	}

	params := map[string]string{
		"Definition":       firstNonEmptyShared(opts.Definitions, "FD,LD,SD,HD,OD,2K,4K"),
		"ResultType":       "Multiple",
		"VideoId":          videoID,
		"SecurityToken":    payload.SecurityToken,
		"SignatureNonce":   aliyunNonce(),
		"SignatureVersion": "1.0",
		"SignatureMethod":  "HMAC-SHA1",
		"Version":          "2017-03-21",
		"Formats":          firstNonEmptyShared(opts.Formats, "m3u8,mp4"),
		"AuthTimeout":      firstNonEmptyShared(opts.AuthTimeout, payload.AuthTimeout, "7200"),
		"AuthInfo":         payload.AuthInfo,
		"Action":           "GetPlayInfo",
		"AccessKeyId":      payload.AccessKeyID,
	}
	for k, v := range opts.ExtraParams {
		if strings.TrimSpace(v) != "" {
			params[k] = v
		}
	}
	params["Signature"] = AliyunSignature(params, payload.AccessKeySecret, "GET")
	apiURL := fmt.Sprintf("https://vod.%s.aliyuncs.com/?%s", payload.Region, AliyunSortedQuery(params))

	body, err := c.GetString(apiURL, aliyunHeaders(opts, false))
	if err != nil {
		return nil, fmt.Errorf("aliyun GetPlayInfo: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		root = parseAliyunPlayXML(body)
	}
	info := extractAliyunPlayResponse(root, opts)
	if info == nil || info.URL == "" {
		return nil, fmt.Errorf("aliyun GetPlayInfo: no PlayURL in response")
	}
	info.APIURL = apiURL
	info.PlayResponse = root

	if info.NeedMerge && opts.FetchM3U8 {
		text, err := c.GetString(info.URL, aliyunHeaders(opts, false))
		if err != nil {
			return nil, fmt.Errorf("aliyun m3u8 fetch: %w", err)
		}
		info.M3U8Text = text
		info.SourceType = "m3u8_text"
		if info.Encrypted && opts.RewriteM3U8Keys {
			rewritten, err := AliyunRewriteM3U8Keys(c, text, payload, info.EncryptType, info.URL, opts)
			if err != nil {
				return nil, err
			}
			info.M3U8Text = rewritten
		}
	}
	return info, nil
}

func extractAliyunPlayResponse(root map[string]any, opts AliyunPlayOptions) *AliyunPlayInfo {
	playList := asAnyMap(root["PlayInfoList"])
	items := asAnyList(playList["PlayInfo"])
	if len(items) == 0 {
		return nil
	}
	plays := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m := asAnyMap(item)
		if firstAnyString(m, "PlayURL", "PlayUrl", "playUrl", "url") != "" {
			plays = append(plays, m)
		}
	}
	if len(plays) == 0 {
		return nil
	}

	rank := aliyunDefinitionRank(opts.Quality, opts.PreferDefinitions)
	sort.SliceStable(plays, func(i, j int) bool {
		a, b := plays[i], plays[j]
		sa, sb := aliyunSourceRank(a), aliyunSourceRank(b)
		if sa != sb {
			return sa < sb
		}
		da, db := strings.ToUpper(firstAnyString(a, "Definition")), strings.ToUpper(firstAnyString(b, "Definition"))
		ra, rb := rank[da], rank[db]
		if ra == 0 && da != firstPreferredDefinition(opts.PreferDefinitions) {
			ra = len(rank) + 1
		}
		if rb == 0 && db != firstPreferredDefinition(opts.PreferDefinitions) {
			rb = len(rank) + 1
		}
		if ra != rb {
			return ra < rb
		}
		return intAny(a["Bitrate"]) > intAny(b["Bitrate"])
	})

	best := plays[0]
	playURL := firstAnyString(best, "PlayURL", "PlayUrl", "playUrl", "url")
	format := strings.ToLower(firstAnyString(best, "Format"))
	if format == "" && strings.Contains(strings.ToLower(playURL), ".m3u8") {
		format = "m3u8"
	}
	if format == "" {
		format = "mp4"
	}
	sizeText := firstNonEmptyShared(firstAnyString(best, "Size"), firstAnyString(asAnyMap(root["VideoBase"]), "Size"))
	encrypted := truthyAny(best["Encrypt"])
	sourceType := "video_url"
	needMerge := format == "m3u8" || strings.Contains(strings.ToLower(playURL), ".m3u8")
	if needMerge {
		sourceType = "m3u8_url"
	}
	return &AliyunPlayInfo{
		URL:         playURL,
		Format:      format,
		NeedMerge:   needMerge,
		Size:        parseAliyunSize(sizeText),
		Definition:  firstAnyString(best, "Definition"),
		Encrypted:   encrypted,
		EncryptType: firstAnyString(best, "EncryptType"),
		SourceType:  sourceType,
	}
}

func aliyunSourceRank(item map[string]any) int {
	format := strings.ToLower(firstAnyString(item, "Format"))
	urlValue := strings.ToLower(firstAnyString(item, "PlayURL", "PlayUrl", "playUrl", "url"))
	if format == "" && strings.Contains(urlValue, ".m3u8") {
		format = "m3u8"
	}
	encrypted := truthyAny(item["Encrypt"])
	encType := firstAnyString(item, "EncryptType")
	switch {
	case format == "mp4" && !encrypted:
		return 0
	case format == "m3u8" && !encrypted:
		return 1
	case format == "m3u8" && encType == "HLSEncryption":
		return 2
	case format == "m3u8" && encType == "AliyunVoDEncryption":
		return 3
	case format == "m3u8":
		return 4
	default:
		return 5
	}
}

func aliyunDefinitionRank(quality string, prefer []string) map[string]int {
	order := prefer
	if len(order) == 0 {
		switch strings.ToLower(quality) {
		case "sd", "ld", "fd":
			order = []string{"SD", "LD", "FD", "HD", "OD", "2K", "4K"}
		case "fhd", "4k", "2k", "od":
			order = []string{"4K", "2K", "OD", "HD", "SD", "LD", "FD"}
		default:
			order = []string{"HD", "SD", "LD", "FD", "OD", "2K", "4K"}
		}
	}
	out := map[string]int{}
	for i, v := range order {
		out[strings.ToUpper(v)] = i + 1
	}
	return out
}

func firstPreferredDefinition(prefer []string) string {
	if len(prefer) == 0 {
		return ""
	}
	return strings.ToUpper(prefer[0])
}

// AliyunRewriteM3U8Keys rewrites EXT-X-KEY URI values to inline hex keys after
// resolving AliyunVoDEncryption media/challenge tokens through MTS GetLicense.
func AliyunRewriteM3U8Keys(c *util.Client, text string, payload AliyunPlayPayload, encType, sourceURL string, opts AliyunPlayOptions) (string, error) {
	if c == nil {
		return "", fmt.Errorf("aliyun: nil client")
	}
	re := regexp.MustCompile(`URI="([^"]+)"`)
	var firstErr error
	keyLines := 0
	rewrittenKeys := 0
	out := re.ReplaceAllStringFunc(text, func(line string) string {
		m := re.FindStringSubmatch(line)
		if len(m) != 2 {
			return line
		}
		keyLines++
		uri := m[1]
		content := []byte(uri)
		if keyURL := aliyunKeyURL(uri, sourceURL); strings.HasPrefix(keyURL, "http") {
			b, err := c.GetBytes(keyURL, aliyunHeaders(opts, false))
			if err != nil {
				firstErr = fmt.Errorf("aliyun key fetch: %w", err)
				return line
			}
			content = b
		}
		mediaID, challenge := AliyunExtractKeyMaterial(content)
		if mediaID == "" || challenge == "" {
			return line
		}
		license, err := AliyunRequestLicense(c, payload, mediaID, challenge, encType, opts)
		if err != nil {
			firstErr = err
			return line
		}
		rewrittenKeys++
		return strings.Replace(line, uri, "0x"+strings.ToUpper(hex.EncodeToString(license)), 1)
	})
	if firstErr != nil {
		return out, firstErr
	}
	if keyLines > 0 && rewrittenKeys == 0 && strings.TrimSpace(encType) != "" {
		return out, fmt.Errorf("aliyun GetLicense: no encrypted key material rewritten")
	}
	return out, nil
}

func aliyunKeyURL(uri, sourceURL string) string {
	if strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") {
		return uri
	}
	if sourceURL == "" || strings.Contains(uri, "://") {
		return ""
	}
	base, err := url.Parse(sourceURL)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

// AliyunExtractKeyMaterial decodes the media/challenge token stored in Aliyun
// encrypted m3u8 key URI content.
func AliyunExtractKeyMaterial(content []byte) (string, string) {
	decoded := aliyunSafeB64Decode(strings.TrimSpace(string(content)))
	if len(decoded) == 0 {
		return "", ""
	}
	m := aliyunMediaTokenRe.FindStringSubmatch(string(decoded))
	if len(m) != 3 {
		return "", ""
	}
	return m[1], m[2]
}

// AliyunRequestLicense signs and posts MTS GetLicense and returns the decoded
// AES key bytes.
func AliyunRequestLicense(c *util.Client, payload AliyunPlayPayload, mediaID, challenge, encType string, opts AliyunPlayOptions) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("aliyun: nil client")
	}
	if payload.AccessKeyID == "" || payload.AccessKeySecret == "" || payload.Region == "" || mediaID == "" || challenge == "" {
		return nil, fmt.Errorf("aliyun GetLicense: incomplete payload")
	}
	licenseURL := fmt.Sprintf("https://mts.%s.aliyuncs.com/?", payload.Region)
	params := map[string]string{
		"data":             challenge,
		"SignatureNonce":   aliyunNonce(),
		"SignatureVersion": "1.0",
		"SignatureMethod":  "HMAC-SHA1",
		"Version":          "2014-06-18",
		"Type":             encType,
		"Format":           "JSON",
		"SecurityToken":    payload.SecurityToken,
		"LicenseUrl":       licenseURL,
		"MediaId":          mediaID,
		"Action":           "GetLicense",
		"AccessKeyId":      payload.AccessKeyID,
	}
	sig := AliyunSignature(params, payload.AccessKeySecret, "POST")
	body, err := c.PostForm(licenseURL+"Signature="+AliyunEncodeURI(sig), params, aliyunHeaders(opts, true))
	if err != nil {
		return nil, fmt.Errorf("aliyun GetLicense: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("aliyun GetLicense parse: %w", err)
	}
	license := firstAnyString(root, "License", "license")
	if license == "" {
		license = firstAnyString(asAnyMap(root["Data"]), "License", "license", "data")
	}
	if license == "" {
		license = firstAnyString(asAnyMap(root["data"]), "License", "license", "data")
	}
	if license == "" {
		return nil, fmt.Errorf("aliyun GetLicense: empty license")
	}
	out := aliyunSafeB64Decode(license)
	if len(out) == 0 {
		out = []byte(license)
	}
	return out, nil
}

func aliyunHeaders(opts AliyunPlayOptions, post bool) map[string]string {
	h := map[string]string{"Accept": "application/json, text/plain, */*"}
	if post {
		h["Content-Type"] = "application/x-www-form-urlencoded; charset=UTF-8"
	}
	if opts.Referer != "" {
		h["Referer"] = opts.Referer
	}
	if opts.Origin != "" {
		h["Origin"] = opts.Origin
	}
	for k, v := range opts.Headers {
		h[k] = v
	}
	return h
}

// AliyunEncodeURI applies Aliyun OpenAPI's RFC3986-ish query escaping.
func AliyunEncodeURI(s string) string {
	r := url.QueryEscape(s)
	r = strings.ReplaceAll(r, "+", "%20")
	r = strings.ReplaceAll(r, "%7E", "~")
	return r
}

// AliyunSortedQuery returns non-empty params sorted and escaped for signing.
func AliyunSortedQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, AliyunEncodeURI(k)+"="+AliyunEncodeURI(params[k]))
	}
	return strings.Join(parts, "&")
}

// AliyunSignature signs params with HMAC-SHA1 using the Aliyun OpenAPI method.
func AliyunSignature(params map[string]string, secret, method string) string {
	qs := AliyunSortedQuery(params)
	toSign := strings.ToUpper(method) + "&" + AliyunEncodeURI("/") + "&" + AliyunEncodeURI(qs)
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(toSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func aliyunNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func aliyunSafeB64Decode(s string) []byte {
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

type aliyunXMLNode struct {
	XMLName xml.Name
	Text    string          `xml:",chardata"`
	Nodes   []aliyunXMLNode `xml:",any"`
}

func parseAliyunPlayXML(text string) map[string]any {
	var n aliyunXMLNode
	if err := xml.Unmarshal([]byte(text), &n); err != nil {
		return map[string]any{}
	}
	return aliyunXMLNodeMap(n)
}

func aliyunXMLNodeMap(n aliyunXMLNode) map[string]any {
	out := map[string]any{}
	for _, c := range n.Nodes {
		name := c.XMLName.Local
		var v any
		if len(c.Nodes) > 0 {
			v = aliyunXMLNodeMap(c)
		} else {
			v = strings.TrimSpace(c.Text)
		}
		if old, ok := out[name]; ok {
			out[name] = append(asAnyList(old), v)
		} else {
			out[name] = v
		}
	}
	return out
}

func asAnyMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asAnyList(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case nil:
		return nil
	default:
		return []any{t}
	}
}

func firstAnyString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func firstNonEmptyShared(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "<nil>" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func truthyAny(v any) bool {
	s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
	return s == "1" || s == "true" || s == "yes"
}

func intAny(v any) int {
	var n int
	_, _ = fmt.Sscanf(fmt.Sprint(v), "%d", &n)
	return n
}

func parseAliyunSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0
	}
	return int64(f)
}
