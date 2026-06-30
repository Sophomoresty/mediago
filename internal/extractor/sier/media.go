package sier

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/util"
)

type sierOverlay struct {
	KeyID              string
	CipheredOverlayIV  string
	CipheredOverlayKey string
	OverlayIV          string
	OverlayKey         string
}

var sierVODOverlays = []sierOverlay{
	{
		KeyID:              "2",
		CipheredOverlayIV:  "3b622ee30f2da63c0f03ff6e3155fe8a76eed12d827a94b5fb9479e1b75cf71bbc73004b8c7c96a8a24d037abe87770d1cb935954b7a737dae628c16304d40fbc58b89b0f61a4634ffeefba71070e128584cbebd6f89ca3097f12ada57e86ffcbd2a8a4c5b66a445454e801859cbf92341abdfba6523501087d890dc81c2cfe2",
		CipheredOverlayKey: "a8705ff15d53d080dc5023c479369d566f33659a0c20a9b87334f4b5489c09104d20702a67a4fd7b8116ab0972e1a7a6684b07d22bff03e85da5d38c32895b71f313df3c25e51e094d6439a36a92e26f81d4e98c88297f767747bb86ae378eb765c2916c9ee4f20355e95f49e42568c54e4904768f704742dedf674627628d08",
		OverlayIV:          "3f69012e53574166ac86dbcf8a5be7a3",
		OverlayKey:         "70e84119f43fc24f83c1617dd29448d3",
	},
	{
		KeyID:              "2",
		CipheredOverlayIV:  "9bf0f6445a006d6f01915d2d34fa6388ea8cd8759f0e351de7e816290c2d7f83de18a2af1b2fc754753ff941eae2102bfb41b409fba231729d2727127d7bee17472b6b561243c936ba48489a534cec4ae7cbaa319d8b177b485c877f6191921e3848207eaa5528cf6bfc38727518b36244800f82be47c88cb3c19e65d1a44143",
		CipheredOverlayKey: "32c6f896f0f3cba6d9b9c87d99f3e330b524ad07d87d50ef3f73c90c88c1e377c39062a59beb75744e81ceaed7b23f1e66252e7457f7d5a688526043e6a33598d8a3ac509d23d2b4fbaf3dfc0172cd37326723a9ee8d612ca4d2d366bdd239fcfe36301bf4e18407bd12695849cb3b8d4d00b4e5b89dbad997ced183c3707278",
		OverlayIV:          "5492ae8d148335d11cbf3186a014811b",
		OverlayKey:         "c9f0f1e4643517d94aca86b21423bdf5",
	},
}

func applySierSignature(method string, u *url.URL, params map[string]string, body string, h map[string]string) {
	method = strings.ToUpper(first(method, "GET"))
	if h["Accept"] == "" {
		h["Accept"] = "application/json, text/plain, */*"
	}
	auth := h["authorization"]
	if auth == "" {
		auth = h["Authorization"]
	}
	signed := map[string]string{
		"x-ca-key":              SIER_APP_KEY,
		"x-ca-nonce":            randomHex16(),
		"x-ca-signature-method": "HmacSHA256",
		"x-ca-timestamp":        strconv.FormatInt(time.Now().UnixMilli(), 10),
		"authorization":         auth,
		"x-device-type":         "1",
		"x-os-type":             "1",
		"x-app-type":            "1",
		"x-platform":            "1",
	}
	order := []string{"x-ca-key", "x-ca-nonce", "x-ca-signature-method", "x-ca-timestamp", "authorization", "x-device-type", "x-os-type", "x-app-type", "x-platform"}
	for _, key := range order {
		h[key] = signed[key]
	}

	canonicalPayload := body
	if canonicalPayload == "" && len(params) > 0 {
		canonicalPayload = sortedQuery(params)
	}
	contentMD5 := ""
	if canonicalPayload != "" {
		sum := md5.Sum([]byte(canonicalPayload))
		contentMD5 = base64.StdEncoding.EncodeToString(sum[:])
		h["content-md5"] = contentMD5
	}
	contentType := h["Content-Type"]
	canonicalURL := u.Path
	if u.RawQuery != "" {
		canonicalURL += "?" + sortedRawQuery(u.RawQuery)
	}
	var signedLines []string
	for _, key := range order {
		signedLines = append(signedLines, key+":"+signed[key])
	}
	signedHeaders := strings.Join(order, ",")
	h["x-ca-signature-headers"] = signedHeaders
	h["x-requested-with"] = "XMLHttpRequest"

	canonical := strings.Join([]string{
		method,
		h["Accept"],
		contentMD5,
		contentType,
		"",
		strings.Join(signedLines, "\n"),
		canonicalURL,
	}, "\n")
	mac := hmac.New(sha256.New, []byte(SIER_APP_SECRET))
	_, _ = mac.Write([]byte(canonical))
	h["x-ca-signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func collectFiles(v any, fallbackCatalog string) []fileInfo {
	var out []fileInfo
	seen := map[string]bool{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			kind := strings.ToLower(first(textAt(t, "discriminator", "materialTypeKey", "typeKey", "resourceType", "type"), textAt(unwrapMap(t["material"]), "typeKey")))
			if f := fileFromNode(t, first(textAt(t, "catalogName", "lessonName", "name", "title"), "资料"), fallbackCatalog); f.URL != "" && !seen[f.URL] && !strings.Contains(kind, "video") && !strings.Contains(kind, "live") {
				seen[f.URL] = true
				if f.Name == "" {
					f.Name = fmt.Sprintf("(%d)--资料", len(out)+1)
				}
				out = append(out, f)
			}
			for _, k := range []string{"datumList", "datums", "fileList", "materialList", "resourceList", "attachmentList", "children", "nodeList", "childList", "syllabus", "courseList", "unitList", "catalogList", "material", "resource"} {
				walk(t[k])
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

func fileFromNode(m map[string]any, defaultName, fallbackID string) fileInfo {
	u := first(textAt(m, "downloadUrl", "fileUrl", "url", "path"), textAt(unwrapMap(m["material"]), "downloadUrl", "fileUrl", "url", "path"), textAt(unwrapMap(m["resource"]), "downloadUrl", "fileUrl", "url", "path"))
	if !strings.HasPrefix(u, "http") {
		return fileInfo{}
	}
	if looksVideoURL(u) {
		return fileInfo{}
	}
	name := first(textAt(m, "name", "fileName", "title", "materialName"), textAt(unwrapMap(m["material"]), "name", "fileName", "title"), defaultName)
	format := strings.TrimPrefix(strings.ToLower(first(textAt(m, "suffix", "fileSuffix", "format"), textAt(unwrapMap(m["material"]), "suffix", "fileSuffix", "format"), pickFormat(u))), ".")
	return fileInfo{Name: name, URL: u, Format: format, CatalogID: first(textAt(m, "catalogId", "id"), fallbackID), MaterialID: textAt(m, "materialId")}
}

func videoPlayURL(m map[string]any) string {
	u := directPlayURL(m)
	if u == "" {
		return ""
	}
	if looksVideoURL(u) {
		return u
	}
	if play, ok := m["playUrl"].(string); ok && strings.HasPrefix(play, "http") {
		return play
	}
	return ""
}

func looksVideoURL(raw string) bool {
	lower := strings.ToLower(strings.SplitN(raw, "?", 2)[0])
	return strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") || strings.HasPrefix(strings.ToLower(raw), "data:application/vnd.apple.mpegurl")
}

func buildSierPlayInfoURL(api, psign string, overlay sierOverlay) string {
	u, err := url.Parse(api)
	if err != nil {
		return api
	}
	q := u.Query()
	q.Set("psign", psign)
	q.Set("keyId", first(overlay.KeyID, "2"))
	q.Set("cipheredOverlayIv", overlay.CipheredOverlayIV)
	q.Set("cipheredOverlayKey", overlay.CipheredOverlayKey)
	u.RawQuery = q.Encode()
	return u.String()
}

func sierPlayInfoOK(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return true
	}
	if code, ok := m["code"]; ok {
		return fmt.Sprint(code) == "0" || fmt.Sprint(code) == "0.0"
	}
	return true
}

func extractSierPlayInfo(c *util.Client, h map[string]string, resp any, overlay sierOverlay) playInfo {
	requestID := first(textAnywhere(resp, "requestId"), textAnywhere(resp, "RequestId"))
	drmToken := textAnywhere(resp, "drmToken")
	var plays []playInfo
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			if u := first(textAt(t, "url", "playUrl", "hlsUrl")); strings.HasPrefix(u, "http") {
				plays = append(plays, playInfo{URL: insertDRMToken(u, first(textAt(t, "drmToken"), drmToken)), Size: int64(numAt(t, "size"))})
			}
			for _, v := range t {
				walk(v)
			}
		case []any:
			for _, v := range t {
				walk(v)
			}
		}
	}
	walk(resp)
	if len(plays) == 0 {
		return playInfo{}
	}
	sort.SliceStable(plays, func(i, j int) bool {
		return sierPlayableRank(plays[i]) > sierPlayableRank(plays[j])
	})
	play := plays[0]
	if strings.Contains(strings.ToLower(play.URL), ".m3u8") {
		if dataURL, text, ok := prepareSierM3U8(c, h, play.URL, requestID, overlay); ok {
			play.URL = dataURL
			play.M3U8Text = text
		}
	}
	return play
}

func sierPlayableRank(play playInfo) int64 {
	if strings.TrimSpace(play.URL) == "" {
		return -1
	}
	return play.Size
}

func prepareSierM3U8(c *util.Client, h map[string]string, m3u8URL, requestID string, overlay sierOverlay) (string, string, bool) {
	headers := map[string]string{"Referer": referer, "User-Agent": user_agent}
	for k, v := range h {
		if strings.EqualFold(k, "cookie") || strings.EqualFold(k, "authorization") {
			headers[k] = v
		}
	}
	text, err := c.GetString(m3u8URL, headers)
	if err != nil || !strings.Contains(text, "#EXTM3U") {
		return "", "", false
	}
	mediaURL := m3u8URL
	if strings.Contains(text, "#EXT-X-STREAM-INF") {
		if variant := selectSierVariant(text, m3u8URL); variant != "" && variant != m3u8URL {
			if body, err := c.GetString(variant, headers); err == nil && strings.Contains(body, "#EXTM3U") {
				text, mediaURL = body, variant
			}
		}
	}
	rewritten := patchSierM3U8(c, headers, text, mediaURL, requestID, overlay)
	return sierM3U8DataURL(rewritten), rewritten, true
}

func patchSierM3U8(c *util.Client, headers map[string]string, mediaText, mediaURL, requestID string, overlay sierOverlay) string {
	out := make([]string, 0, strings.Count(mediaText, "\n")+1)
	sc := bufio.NewScanner(strings.NewReader(mediaText))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			attrs := parseM3U8Attrs(strings.TrimPrefix(line, "#EXT-X-KEY:"))
			if keyURI := attrs["URI"]; keyURI != "" && requestID != "" {
				keyURL := resolveSierLine(keyURI, mediaURL)
				if raw, err := c.GetBytes(keyURL, headers); err == nil {
					if key := deriveSierHLSKey(raw, requestID, overlay); len(key) == 16 {
						line = replaceAttrURI(line, sierKeyDataURL(key))
					}
				}
			}
			out = append(out, line)
			continue
		}
		if !strings.HasPrefix(line, "#") {
			line = resolveSierLine(line, mediaURL)
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n") + "\n"
}

func deriveSierHLSKey(rawLicense []byte, requestID string, overlay sierOverlay) []byte {
	secret := "hnv7F2VwUA5O1ZfvEKjl_" + overlay.OverlayKey + "_" + overlay.OverlayIV
	mac := hmac.New(sha256.New, []byte(requestID))
	_, _ = mac.Write([]byte(secret))
	derived := mac.Sum(nil)
	key, iv := derived[:16], derived[16:32]
	block, err := aes.NewCipher(key)
	if err != nil || len(rawLicense)%aes.BlockSize != 0 {
		return nil
	}
	plain := make([]byte, len(rawLicense))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, rawLicense)
	if unpadded := pkcs7Unpad(plain); len(unpadded) == 16 {
		return unpadded
	}
	if len(plain) >= 16 {
		return plain[:16]
	}
	return nil
}

func parseM3U8Attrs(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		out[strings.ToUpper(strings.TrimSpace(kv[0]))] = strings.Trim(strings.TrimSpace(kv[1]), `"`)
	}
	return out
}

func replaceAttrURI(line, uri string) string {
	if i := strings.Index(line, `URI="`); i >= 0 {
		rest := line[i+5:]
		if j := strings.Index(rest, `"`); j >= 0 {
			return line[:i+5] + uri + rest[j:]
		}
	}
	return line + `,URI="` + uri + `"`
}

func selectSierVariant(master, masterURL string) string {
	type variant struct {
		url string
		bw  int64
	}
	var variants []variant
	var currentBW int64
	sc := bufio.NewScanner(strings.NewReader(master))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			currentBW = 0
			for k, v := range parseM3U8Attrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:")) {
				if k == "BANDWIDTH" {
					currentBW, _ = strconv.ParseInt(v, 10, 64)
				}
			}
			continue
		}
		if !strings.HasPrefix(line, "#") && currentBW >= 0 {
			variants = append(variants, variant{url: resolveSierLine(line, masterURL), bw: currentBW})
			currentBW = -1
		}
	}
	if len(variants) == 0 {
		return masterURL
	}
	sort.SliceStable(variants, func(i, j int) bool { return variants[i].bw > variants[j].bw })
	return variants[0].url
}

func resolveSierLine(raw, baseURL string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func textAnywhere(v any, keys ...string) string {
	switch t := v.(type) {
	case map[string]any:
		if s := textAt(t, keys...); s != "" {
			return s
		}
		for _, child := range t {
			if s := textAnywhere(child, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range t {
			if s := textAnywhere(child, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}

func sortedQuery(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(params[key]))
	}
	return strings.Join(parts, "&")
}

func sortedRawQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func randomHex16() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 16)
}

func sierKeyDataURL(key []byte) string {
	return "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(key)
}

func sierM3U8DataURL(manifest string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(manifest))
}
