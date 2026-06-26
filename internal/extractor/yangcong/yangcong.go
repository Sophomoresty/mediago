// Package yangcong implements an extractor for yangcongxueyuan.com / yangcong345.com.
package yangcong

import (
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL        = "https://school.yangcongxueyuan.com/"
	originURL         = "https://school.yangcongxueyuan.com"
	apiHost           = "https://school-api.yangcong345.com"
	subjectsURL       = apiHost + "/course/subjects"
	chaptersURL       = apiHost + "/course/chapters-with-section/scene"
	specialCoursesURL = apiHost + "/course-tree/special-courses"
	specialCourseURL  = apiHost + "/course/special-course/%s"
	topicDetailsURL   = apiHost + "/course-business/courseTree/getAnyTopicDetailsByIds"
	videoAddressesURL = apiHost + "/videos/addresses"
	orderAuthURL      = apiHost + "/user-auths/order/auth"
	meURL             = apiHost + "/me"
	hlsSaltURL        = apiHost + "/videoBase/getHlsEncryptSalt"
	hlsKeyURL         = apiHost + "/videoBase/getHlsEncryptKey?id=%s&x-key=%s"

	// Yangcong_Config constants
	hlsSaltFallback = "yangcong"           // YANGCONG_HLS_SALT_FALLBACK
	hlsKeyVersion   = "1.0.12-beta.18"     // YANGCONG_HLS_KEY_VERSION
	aesECBKey       = "1234567890123456"    // AES ECB key for encrypt_body decryption
	ycUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var (
	patterns     = []string{`(?:[\w-]+\.)?(?:yangcong345|yangcongxueyuan)\.com/`}
	specialIDRe  = regexp.MustCompile(`(?:special-course/|special-)([\w-]+)`)
	titleCleanRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
	hlsURIRe     = regexp.MustCompile(`URI="([^"]+)"`)
)

func init() {
	extractor.Register(&Yangcong{}, extractor.SiteInfo{Name: "Yangcong", URL: "yangcongxueyuan.com", NeedAuth: true})
}

type Yangcong struct{}

func (y *Yangcong) Patterns() []string { return patterns }

type courseRequest struct {
	subjectID, stageID, publisherID, semesterID, semesterName string
	specialID, title                                          string
}

type ycVideo struct {
	VideoID string
	TopicID string
	Title   string
	Path    string
}

func (y *Yangcong) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("yangcong requires login cookies")
	}
	req := parseCourseRequest(rawURL)
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := buildHeaders(opts.Cookies)
	if err := checkCookie(c, headers); err != nil {
		return nil, err
	}
	_, _ = getJSON(c, subjectsURL, headers)  // source warms course subject list
	_, _ = getJSON(c, orderAuthURL, headers) // source loads order/auth before price

	root, title, err := fetchCoursePayload(c, headers, req)
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = firstNonEmpty(req.title, req.specialID, req.semesterName, "yangcong")
	}
	videos := collectVideos(root)
	if len(videos) == 0 {
		return nil, fmt.Errorf("yangcong: no video topics found")
	}
	entries := make([]*extractor.MediaInfo, 0, len(videos))
	seen := map[string]bool{}
	for _, v := range videos {
		if v.VideoID == "" || seen[v.VideoID+":"+v.TopicID] {
			continue
		}
		seen[v.VideoID+":"+v.TopicID] = true
		entry, err := resolveVideo(c, headers, v)
		if err == nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("yangcong: no playable video address resolved")
	}
	return &extractor.MediaInfo{Site: "yangcong", Title: cleanTitle(title), Entries: entries}, nil
}

func parseCourseRequest(raw string) courseRequest {
	u, _ := url.Parse(raw)
	q := url.Values{}
	if u != nil {
		for k, vs := range u.Query() {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		if strings.Contains(u.Fragment, "?") {
			if fq, err := url.ParseQuery(strings.SplitN(u.Fragment, "?", 2)[1]); err == nil {
				for k, vs := range fq {
					for _, v := range vs {
						q.Add(k, v)
					}
				}
			}
		}
	}
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(q.Get(k)); v != "" {
				return v
			}
		}
		return ""
	}
	r := courseRequest{subjectID: get("subjectId", "subject_id"), stageID: get("stageId", "stage_id"), publisherID: get("publisherId", "publisher_id"), semesterID: get("semesterId", "semester_id"), semesterName: get("semesterName", "semester_name"), specialID: get("specialCourseId", "special_course_id", "cid", "id"), title: get("title", "name")}
	if m := specialIDRe.FindStringSubmatch(raw); len(m) > 1 {
		r.specialID = m[1]
	}
	if strings.Contains(raw, "courseType=special") || strings.Contains(raw, "course_type=special") {
		return r
	}
	if r.subjectID != "" && r.stageID != "" {
		r.specialID = ""
	}
	return r
}

func buildHeaders(jar http.CookieJar) map[string]string {
	h := map[string]string{"Accept": "application/json, text/plain, */*", "Origin": originURL, "Referer": refererURL}
	for _, raw := range []string{refererURL, apiHost + "/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			if strings.EqualFold(ck.Name, "authorization") || strings.EqualFold(ck.Name, "token") {
				h["authorization"] = normalizeAuth(ck.Value)
			}
		}
	}
	return h
}

func normalizeAuth(v string) string {
	v = strings.TrimSpace(v)
	if v != "" && !strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return "Bearer " + v
	}
	return v
}

func checkCookie(c *util.Client, headers map[string]string) error {
	body, err := c.GetString(meURL, headers)
	if err != nil {
		return fmt.Errorf("yangcong me: %w", err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return fmt.Errorf("yangcong me parse: %w", err)
	}
	if firstString(data, "id") == "" || firstString(data, "role") == "" {
		return fmt.Errorf("yangcong login check failed")
	}
	return nil
}

func fetchCoursePayload(c *util.Client, headers map[string]string, req courseRequest) (map[string]any, string, error) {
	if req.specialID != "" {
		data, err := getJSON(c, fmt.Sprintf(specialCourseURL, url.PathEscape(req.specialID)), headers)
		if err != nil {
			return nil, "", err
		}
		return data, firstString(data, "name", "title"), nil
	}
	if req.subjectID == "" || req.stageID == "" || req.publisherID == "" || req.semesterID == "" {
		return nil, "", fmt.Errorf("yangcong: subjectId/stageId/publisherId/semesterId are required for sync course URLs")
	}
	q := url.Values{"filterPublished": {"false"}, "subjectId": {req.subjectID}, "stageId": {req.stageID}, "publisherId": {req.publisherID}, "semesterId": {req.semesterID}}
	if req.semesterName != "" {
		q.Set("semesterName", req.semesterName)
	}
	data, err := getJSON(c, chaptersURL+"?"+q.Encode(), headers)
	if err != nil {
		return nil, "", err
	}
	book := asMap(data["defaultBook"])
	return data, firstNonEmpty(firstString(book, "name", "title"), req.title), nil
}

func getJSON(c *util.Client, apiURL string, headers map[string]string) (map[string]any, error) {
	body, err := c.GetString(apiURL, headers)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		var arr []any
		if e := json.Unmarshal([]byte(body), &arr); e != nil {
			return nil, err
		}
		data = map[string]any{"list": arr}
	}
	return data, nil
}

func postJSON(c *util.Client, apiURL string, payload any, headers map[string]string) (map[string]any, error) {
	b, _ := json.Marshal(payload)
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	resp, err := c.Post(apiURL, strings.NewReader(string(b)), h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func collectVideos(root map[string]any) []ycVideo {
	var out []ycVideo
	var walk func(any, []string)
	walk = func(v any, path []string) {
		m := asMap(v)
		if len(m) > 0 {
			name := cleanTitle(firstString(m, "name", "title"))
			if name != "" {
				path = append(path, name)
			}
			if vid := videoID(m); vid != "" {
				out = append(out, ycVideo{VideoID: vid, TopicID: firstString(m, "id", "topic_id", "topicId"), Title: firstNonEmpty(name, vid), Path: strings.Join(path, " / ")})
			}
			for _, k := range []string{"children", "childrens", "levels", "sections", "subsections", "themes", "topics", "items", "list"} {
				walk(m[k], path)
			}
			return
		}
		if arr, ok := v.([]any); ok {
			for _, it := range arr {
				walk(it, append([]string{}, path...))
			}
		}
	}
	walk(root, nil)
	return out
}

func videoID(m map[string]any) string {
	if v := firstString(m, "videoId", "video_id"); v != "" {
		return v
	}
	return firstString(asMap(m["video"]), "id", "videoId", "video_id")
}

func resolveVideo(c *util.Client, headers map[string]string, v ycVideo) (*extractor.MediaInfo, error) {
	payload := map[string]any{"videoList": []map[string]any{{"refinedExerciseId": v.TopicID, "topicId": v.TopicID, "videoId": v.VideoID, "custom": map[string]string{"videoId": v.VideoID}}}}
	resp, err := postJSON(c, videoAddressesURL, payload, headers)
	if err != nil {
		return nil, fmt.Errorf("yangcong video address: %w", err)
	}
	addressList := extractAddressList(resp)

	// Source logic: prefer HLS, then try HLS m3u8 rewrite, fall back to mp4.
	addr := selectAddress(addressList, "hls")
	format := "m3u8"
	if addr != "" && strings.Contains(addr, ".m3u8") {
		rewritten := rewriteHLSM3U8(c, headers, addr, v.VideoID)
		if rewritten != "" {
			return &extractor.MediaInfo{Site: "yangcong", Title: cleanTitle(firstNonEmpty(v.Path, v.Title, v.VideoID)), Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{addr}, Format: "m3u8", Headers: map[string]string{"Referer": refererURL}}}, Extra: map[string]any{"video_id": v.VideoID, "topic_id": v.TopicID, "m3u8_text": rewritten}}, nil
		}
	}

	// Fall back: mp4 or any available format
	addr = selectAddress(addressList, "mp4")
	if addr == "" {
		addr = pickAddress(resp)
	}
	if addr == "" {
		return nil, fmt.Errorf("yangcong: no address for video %s", v.VideoID)
	}
	format = pickFormat(addr)
	return &extractor.MediaInfo{Site: "yangcong", Title: cleanTitle(firstNonEmpty(v.Path, v.Title, v.VideoID)), Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{addr}, Format: format, Headers: map[string]string{"Referer": refererURL}}}, Extra: map[string]any{"video_id": v.VideoID, "topic_id": v.TopicID}}, nil
}

// extractAddressList pulls out the address array from the video addresses response.
// Source: resp.get("videoList", [])[0].get("address", [])
func extractAddressList(resp map[string]any) []map[string]any {
	videoList, _ := resp["videoList"].([]any)
	if len(videoList) == 0 {
		// Try the direct response walk
		if list, ok := resp["list"].([]any); ok {
			videoList = list
		}
	}
	if len(videoList) == 0 {
		return nil
	}
	first := asMap(videoList[0])
	if first == nil {
		return nil
	}
	rawAddrs, _ := first["address"].([]any)
	if len(rawAddrs) == 0 {
		return nil
	}
	var out []map[string]any
	for _, a := range rawAddrs {
		if m := asMap(a); m != nil {
			out = append(out, m)
		}
	}
	return out
}

// selectAddress implements the source's _select_address with prefer_format.
// It iterates format priority (prefer_format first, then the other, then ycm),
// then clarity priority (fullHigh > high > middle > low for FHD mode),
// then platform priority (pc > mobile > ""),
// skipping .ycm URLs.
func selectAddress(addressList []map[string]any, preferFormat string) string {
	if len(addressList) == 0 {
		return ""
	}
	// Build format iteration order: [preferFormat, otherFormat, ycm]
	var formatOrder []string
	if preferFormat == "mp4" {
		formatOrder = []string{"mp4", "hls", "ycm"}
	} else {
		formatOrder = []string{"hls", "mp4", "ycm"}
	}
	// Clarity priority for FHD mode (default)
	clarityOrder := []string{"fullHigh", "high", "middle", "low"}
	// Platform priority
	platformOrder := []string{"pc", "mobile", ""}

	for _, fmt := range formatOrder {
		for _, clarity := range clarityOrder {
			for _, platform := range platformOrder {
				for _, addr := range addressList {
					if firstString(addr, "format") != fmt {
						continue
					}
					if firstString(addr, "clarity") != clarity {
						continue
					}
					if platform != "" && firstString(addr, "platform") != platform {
						continue
					}
					u := firstString(addr, "url")
					if u != "" && !strings.HasSuffix(u, ".ycm") {
						return u
					}
				}
			}
		}
	}
	// Fallback: any address with a non-.ycm URL
	for _, addr := range addressList {
		u := firstString(addr, "url")
		if u != "" && !strings.HasSuffix(u, ".ycm") {
			return u
		}
	}
	return ""
}

func pickAddress(v any) string {
	bestURL, bestScore := "", -1
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for _, it := range t {
				walk(it)
			}
		case map[string]any:
			if u := firstString(t, "url"); strings.HasPrefix(u, "http") && !strings.HasSuffix(u, ".ycm") {
				score := qualityScore(firstString(t, "format"), firstString(t, "clarity"), firstString(t, "platform"))
				if score > bestScore {
					bestURL, bestScore = u, score
				}
			}
			for _, k := range []string{"address", "data", "list", "videoList", "videos"} {
				walk(t[k])
			}
		}
	}
	walk(v)
	return bestURL
}

func qualityScore(vals ...string) int {
	score := 0
	joined := strings.ToLower(strings.Join(vals, " "))
	for i, key := range []string{"low", "middle", "high", "fullhigh"} {
		if strings.Contains(joined, key) {
			score = i + 1
		}
	}
	if strings.Contains(joined, "mp4") {
		score += 10
	} else if strings.Contains(joined, "hls") {
		score += 5
	}
	if strings.Contains(joined, "pc") {
		score++
	}
	return score
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(fmt.Sprint(m[k])); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }
func pickFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}

// --- HLS key decryption and m3u8 rewriting (source: Yangcong_Course) ---

// safeB64Decode decodes a URL-safe or standard base64 string with padding fix.
func safeB64Decode(value string) []byte {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.ReplaceAll(value, "-", "+")
	value = strings.ReplaceAll(value, "_", "/")
	if mod := len(value) % 4; mod != 0 {
		value += strings.Repeat("=", 4-mod)
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil
	}
	return data
}

// decryptEncryptBody decrypts an AES-ECB encrypted base64 body using key "1234567890123456".
func decryptEncryptBody(encryptBody string) string {
	data := safeB64Decode(encryptBody)
	if len(data) == 0 {
		return ""
	}
	block, err := aes.NewCipher([]byte(aesECBKey))
	if err != nil {
		return ""
	}
	bs := block.BlockSize()
	if len(data)%bs != 0 {
		return ""
	}
	// ECB mode decryption
	out := make([]byte, len(data))
	for i := 0; i < len(data); i += bs {
		block.Decrypt(out[i:i+bs], data[i:i+bs])
	}
	// PKCS7 unpadding
	if len(out) == 0 {
		return ""
	}
	pad := int(out[len(out)-1])
	if pad > 0 && pad <= bs && pad <= len(out) {
		out = out[:len(out)-pad]
	}
	return string(out)
}

// decodeEncryptedJSON handles the encrypt_body wrapper from yangcong API responses.
func decodeEncryptedJSON(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	eb, ok := data["encrypt_body"].(string)
	if !ok || eb == "" {
		return data
	}
	plaintext := decryptEncryptBody(eb)
	if plaintext == "" {
		return data
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(plaintext), &result); err != nil {
		return data
	}
	return result
}

// getHLSSalt fetches the HLS encryption salt from the API.
// Falls back to YANGCONG_HLS_SALT_FALLBACK ("yangcong").
func getHLSSalt(c *util.Client, headers map[string]string) string {
	h := map[string]string{
		"Content-Type": "application/json;charset=UTF-8",
		"params-style": "encrypt",
		"User-Agent":   ycUserAgent,
	}
	// Merge in authorization from headers
	for k, v := range headers {
		if strings.EqualFold(k, "authorization") {
			h[k] = v
		}
	}
	body, err := c.GetString(hlsSaltURL, h)
	if err != nil {
		return hlsSaltFallback
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return hlsSaltFallback
	}
	decoded := decodeEncryptedJSON(raw)
	dataObj := asMap(decoded["data"])
	if dataObj == nil {
		// Try direct .salt access
		dataObj = decoded
	}
	saltB64 := firstString(dataObj, "salt")
	if saltB64 == "" {
		return hlsSaltFallback
	}
	saltBytes := safeB64Decode(saltB64)
	if len(saltBytes) == 0 {
		return hlsSaltFallback
	}
	return string(saltBytes)
}

// buildHLSXKey constructs the x-key parameter for HLS key requests.
// Source: _build_hls_x_key(self, video_id)
func buildHLSXKey(c *util.Client, headers map[string]string, videoID string) string {
	salt := getHLSSalt(c, headers)
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	raw := fmt.Sprintf("v=2&sMethod=md5&vId=%s&timestamp=%s", videoID, ts)
	h := md5.Sum([]byte(raw + salt))
	sig := hex.EncodeToString(h[:])
	payload := raw + "&signature=" + sig
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))
	return url.QueryEscape(encoded)
}

// getHLSKeyBytes fetches the 16-byte AES key for HLS decryption.
// Source: _get_hls_key_bytes(self, key_url, video_id)
func getHLSKeyBytes(c *util.Client, headers map[string]string, keyURL string, videoID string) []byte {
	if keyURL == "" || videoID == "" {
		return nil
	}
	// Parse the key URL to extract the "id" parameter
	var keyID string
	parsed, err := url.Parse(keyURL)
	if err == nil {
		qs := parsed.Query()
		ids := qs["id"]
		if len(ids) > 0 {
			keyID = ids[0]
		}
	}
	if keyID == "" {
		return nil
	}
	// Build the API URL
	xKey := buildHLSXKey(c, headers, videoID)
	apiURL := apiHost + fmt.Sprintf("/videoBase/getHlsEncryptKey?id=%s&x-key=%s",
		url.QueryEscape(keyID), xKey)

	h := map[string]string{
		"Content-Type": "application/json;charset=UTF-8",
		"params-style": "encrypt",
		"source":       "PC",
		"userId":       "",
		"env":          "online",
		"version":      hlsKeyVersion,
		"User-Agent":   ycUserAgent,
	}
	// Merge authorization
	for k, v := range headers {
		if strings.EqualFold(k, "authorization") {
			h[k] = v
		}
	}
	resp, err := c.Get(apiURL, h)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	if len(content) == 16 {
		return content
	}
	return nil
}

// absolutizeHLSSegments converts relative segment paths in m3u8 text to absolute URLs.
// Source: _absolutize_hls_segments(self, m3u8_text, m3u8_url)
func absolutizeHLSSegments(m3u8Text string, m3u8URL string) string {
	parts := strings.SplitAfterN(m3u8URL, "/", -1)
	baseURL := ""
	if idx := strings.LastIndex(m3u8URL, "/"); idx >= 0 {
		baseURL = m3u8URL[:idx+1]
	}
	_ = parts
	var lines []string
	for _, line := range strings.Split(m3u8Text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			lines = append(lines, line)
		} else {
			// Relative path: join with base URL
			absURL := baseURL + trimmed
			if u, err := url.Parse(baseURL); err == nil {
				if ref, err := url.Parse(trimmed); err == nil {
					absURL = u.ResolveReference(ref).String()
				}
			}
			lines = append(lines, absURL)
		}
	}
	return strings.Join(lines, "\n")
}

// rewriteHLSM3U8 fetches an m3u8 manifest, replaces the EXT-X-KEY URI with
// an inline hex key (fetched via the HLS key API), and absolutizes segment URLs.
// Source: _rewrite_hls_m3u8(self, m3u8_url, video_id)
func rewriteHLSM3U8(c *util.Client, headers map[string]string, m3u8URL string, videoID string) string {
	if m3u8URL == "" {
		return ""
	}
	h := map[string]string{
		"Referer":    refererURL,
		"User-Agent": util.RandomUA(),
	}
	// Merge authorization
	for k, v := range headers {
		if strings.EqualFold(k, "authorization") {
			h[k] = v
		}
	}
	resp, err := c.Get(m3u8URL, h)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	m3u8Text := string(body)

	// Find URI="..." in EXT-X-KEY line
	match := hlsURIRe.FindStringSubmatch(m3u8Text)
	if match == nil {
		// No encryption key URI found, just absolutize segments
		return absolutizeHLSSegments(m3u8Text, m3u8URL)
	}
	origURI := match[1]

	// Fetch the HLS key bytes
	keyBytes := getHLSKeyBytes(c, headers, origURI, videoID)
	if len(keyBytes) == 0 {
		return ""
	}
	// Replace the URI with the hex-encoded key bytes
	keyHex := hex.EncodeToString(keyBytes)
	m3u8Text = strings.Replace(m3u8Text, origURI, keyHex, 1)

	return absolutizeHLSSegments(m3u8Text, m3u8URL)
}
