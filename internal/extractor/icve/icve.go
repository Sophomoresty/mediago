// Package icve implements source-aligned Icve AI extraction.
//
// Covers:
//   - Icve_Ai (ai.icve.com.cn) – title/design/cell/status + video transcoding
//   - Icve_Base smartedu redirect (vocational.smartedu.cn/Details → queryList → fwdz)
package icve

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	COURSENAME          = "{1}--课程"
	FILENAME            = "{2}--资源"
	MATERIAL            = "【全部素材】"
	IS_HD               = 1
	IS_SD               = 2
	ONLY_PDF            = 3
	LEN_S               = 96
	LEN_                = 48
	TIME_SLEEP          = 3.6
	referer             = "https://www.icve.com.cn"
	smarteduReferer     = "https://vocational.smartedu.cn"
	urlTitle            = "https://ai.icve.com.cn/prod-api/course/courseInfo/getLatestInfoByCourseId?courseId=%s"
	urlInfo             = "https://ai.icve.com.cn/prod-api/course/courseDesign/getDesignList?courseInfoId=%s&courseId=%s"
	urlCell             = "https://ai.icve.com.cn/prod-api/course/courseDesign/getCellList?courseInfoId=%s&courseId=%s&parentId=%s"
	urlSourceStatus     = "https://upload.icve.com.cn/%s/status"
	smarteduQueryURL    = "https://vocational.smartedu.cn/gjzyjy/inco/ht/queryList"
	smarteduDetailSQLID = "171695011763866a394676496125233763746e2fbd87ebc94"
)

// patterns matches ai.icve.com.cn course URLs and vocational.smartedu.cn redirect URLs.
// Source: Mooc_Config courses_re['Icve_Ai'] + courses_redirect_re['Icve_Base'].
var patterns = []string{
	`\s*((https?://ai\.icve\.com\.cn/.*?excellent.*?/(?P<cid1>[-\w]+))|(https?://ai\.icve\.com\.cn/.*?course.*?/(?P<cid2>[-\w]+)))`,
	`\s*https?://vocational\.smartedu\.cn/Details\?[^\s#]*\bid=[\w-]+[^\s#]*`,
}

// smarteduRedirectRe detects vocational.smartedu.cn/Details URLs.
// The original Python regex uses lookaheads to match id= and lx= in any order,
// which Go's RE2 engine does not support. Instead we match the base URL and
// extract query params via net/url in resolveSmartEduURL.
// Source: Mooc_Config courses_redirect_re['Icve_Base'].
var smarteduRedirectRe = regexp.MustCompile(
	`(?i)\s*https?://vocational\.smartedu\.cn/Details\?`,
)

func init() {
	extractor.Register(&Icve{}, extractor.SiteInfo{Name: "Icve", URL: "icve.com.cn", NeedAuth: false})
}

type Icve struct{}

func (i *Icve) Patterns() []string { return patterns }

type aiCtx struct {
	c       *util.Client
	headers map[string]string
	mode    int
	cid     string
	infoID  string
	title   string
}

type aiItem struct {
	Name string
	Info string
	Kind string
	Ext  string
}

type aiTitleResp struct {
	Data struct {
		ID         any `json:"id"`
		CourseName any `json:"courseName"`
		SchoolName any `json:"schoolName"`
	} `json:"data"`
}

func (i *Icve) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil {
		opts = &extractor.ExtractOpts{}
	}
	jar := opts.Cookies
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}

	// Source: Icve_Base.get_redirect_url – resolve vocational.smartedu.cn
	// redirect URLs to real ICVE course URLs before extraction.
	resolved, err := resolveSmartEduURL(rawURL, jar)
	if err != nil {
		return nil, fmt.Errorf("icve: smartedu redirect failed: %w", err)
	}
	if resolved != "" {
		rawURL = resolved
	}

	x := newCtx(jar, modeFromQuality(opts.Quality))
	x.cid = parseCID(rawURL)
	if x.cid == "" {
		return nil, fmt.Errorf("icve: cannot parse course id from URL")
	}
	if err := x.loadTitle(); err != nil {
		return nil, err
	}
	items, err := x.loadItems()
	if err != nil {
		return nil, err
	}
	return x.mediaFromItems(items)
}

// resolveSmartEduURL implements Icve_Base.get_redirect_url.
// If rawURL is a vocational.smartedu.cn/Details URL, it encrypts {sqlid, id, lx}
// with AES-CBC and POSTs to the queryList endpoint to obtain the real course URL (fwdz).
// Returns "" if the URL is not a smartedu redirect.
func resolveSmartEduURL(rawURL string, jar http.CookieJar) (string, error) {
	if !smarteduRedirectRe.MatchString(rawURL) {
		return "", nil
	}
	// Parse id and lx from query parameters (source uses named regex groups,
	// but Go RE2 doesn't support lookaheads – parse via net/url instead).
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", nil
	}
	id := strings.TrimSpace(u.Query().Get("id"))
	lx := strings.TrimSpace(u.Query().Get("lx"))
	if id == "" || lx == "" {
		return "", nil
	}

	// Build param payload – source: Icve_Base._smartedu_encrypt_param
	paramData := map[string]string{
		"sqlid": smarteduDetailSQLID,
		"id":    id,
		"lx":    lx,
	}
	encrypted, err := smarteduEncryptParam(paramData)
	if err != nil {
		return "", fmt.Errorf("encrypt param: %w", err)
	}

	// POST to queryList – source: request_json(smartedu_query_url, {"param": encrypted}, headers)
	postBody := map[string]string{"param": encrypted}
	bodyJSON, err := json.Marshal(postBody)
	if err != nil {
		return "", err
	}

	c := util.NewClient()
	c.SetCookieJar(jar)
	hdrs := map[string]string{
		"Origin":       smarteduReferer,
		"Referer":      rawURL,
		"Content-Type": "application/json",
	}
	resp, err := c.Post(smarteduQueryURL, bytes.NewReader(bodyJSON), hdrs)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Parse response: {"content": [{"fwdz": "..."}]}
	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", nil // non-fatal: just skip redirect
	}
	content := mapsFromAny(result["content"])
	if len(content) == 0 {
		return "", nil
	}
	fwdz := strings.TrimSpace(str(content[0]["fwdz"]))
	if fwdz == "" {
		return "", nil
	}

	// Normalize URL – source: Icve_Base.get_redirect_url
	if strings.HasPrefix(fwdz, "//") {
		fwdz = "https:" + fwdz
	} else if !strings.HasPrefix(fwdz, "http") {
		fwdz = "https://" + strings.TrimLeft(fwdz, "/")
	}
	return fwdz, nil
}

// smarteduEncryptParam implements Icve_Base._smartedu_encrypt_param.
// AES-CBC encryption with key=inco12345678ocni, iv=ocni12345678inco,
// null-byte padding to block boundary, base64 encoded output.
func smarteduEncryptParam(data map[string]string) (string, error) {
	// Source uses json.dumps(data, ensure_ascii=False, separators=(',', ':'))
	plaintext, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	// Pad with null bytes to AES block boundary (source: b'\x00' * pad)
	pad := (aes.BlockSize - len(plaintext)%aes.BlockSize) % aes.BlockSize
	if pad > 0 {
		plaintext = append(plaintext, make([]byte, pad)...)
	}

	key := []byte("inco12345678ocni")
	iv := []byte("ocni12345678inco")

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	mode := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(plaintext))
	mode.CryptBlocks(ciphertext, plaintext)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func newCtx(jar http.CookieJar, mode int) *aiCtx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	headers := map[string]string{
		"Sec-Fetch-Site":     "same-origin",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Sec-Ch-Ua-Platform": `"Windows"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua":          `"Not/A)Brand";v="99", "Google Chrome";v="115", "Chromium";v="115"`,
		"Referer":            referer,
		"cookie":             cookieHeader(jar, icveCookieOrigins("https://ai.icve.com.cn/", "https://upload.icve.com.cn/")),
		"User-Agent":         util.RandomUA(),
	}
	return &aiCtx{c: c, headers: headers, mode: mode}
}

func (x *aiCtx) loadTitle() error {
	body, err := x.c.GetString(fmt.Sprintf(urlTitle, url.QueryEscape(x.cid)), x.headers)
	if err != nil {
		return err
	}
	var resp aiTitleResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		resp = aiTitleResp{}
	}
	x.infoID = str(resp.Data.ID)
	x.title = cleanTitle(fmt.Sprintf("%s_%s", str(resp.Data.CourseName), str(resp.Data.SchoolName)))
	return nil
}

func (x *aiCtx) loadItems() ([]aiItem, error) {
	body, err := x.c.GetString(fmt.Sprintf(urlInfo, url.QueryEscape(x.infoID), url.QueryEscape(x.cid)), x.headers)
	if err != nil {
		return nil, err
	}
	root := parseJSONMap(body)
	data := listAt(root, "data")
	sortBySort(data)
	var items []aiItem
	items = append(items, collectAIItems(data, nil)...)
	for i, node := range data {
		if id := str(node["id"]); id != "" {
			cellItems, err := x.loadCellItems(id)
			if err != nil {
				continue
			}
			items = append(items, collectAIItems(cellItems, []int{i + 1})...)
		}
	}
	return dedupeAIItems(items), nil
}

func (x *aiCtx) loadCellItems(parentID string) ([]map[string]any, error) {
	body, err := x.c.GetString(fmt.Sprintf(urlCell, url.QueryEscape(x.infoID), url.QueryEscape(x.cid), url.QueryEscape(parentID)), x.headers)
	if err != nil {
		return nil, err
	}
	root := parseJSONMap(body)
	data := listAt(root, "data")
	sortBySort(data)
	return data, nil
}

func (x *aiCtx) mediaFromItems(items []aiItem) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	var lastErr error
	for _, item := range items {
		switch item.Kind {
		case "video":
			if x.mode == ONLY_PDF {
				continue
			}
			url, ext := x.getVideoURL(item.Info)
			if url == "" {
				continue
			}
			if ext == "" {
				ext = pickExt(url)
			}
			if ext == "" {
				ext = "mp4"
			}
			entries = append(entries, &extractor.MediaInfo{
				Site:  "icve",
				Title: item.Name,
				Streams: map[string]extractor.Stream{
					ext: {Quality: ext, URLs: []string{url}, Format: ext, NeedMerge: ext == "m3u8", Headers: cloneHeaders(x.headers)},
				},
				Extra: map[string]any{"kind": "video"},
			})
		case "file":
			fileEntries := x.fileMediaEntries(item)
			if len(fileEntries) == 0 {
				continue
			}
			entries = append(entries, fileEntries...)
		default:
			lastErr = fmt.Errorf("icve: unknown item kind %q", item.Kind)
		}
	}
	if len(entries) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("icve: no playable entries")
	}
	if len(entries) == 1 {
		if x.title != "" {
			entries[0].Extra["course_title"] = x.title
		}
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "icve", Title: firstNonEmpty(x.title, x.cid, "icve"), Entries: entries, Extra: map[string]any{"course_id": x.cid, "info_id": x.infoID}}, nil
}

func (x *aiCtx) fileMediaEntries(item aiItem) []*extractor.MediaInfo {
	rawURL, ext := x.getFileURL(item.Info)
	if isDownloadableICVEURL(rawURL) {
		if ext == "" {
			ext = pickExt(rawURL)
		}
		if ext == "" {
			ext = "html"
		}
		return []*extractor.MediaInfo{{
			Site:  "icve",
			Title: item.Name,
			Streams: map[string]extractor.Stream{
				ext: {Quality: ext, URLs: []string{rawURL}, Format: ext, Headers: cloneHeaders(x.headers)},
			},
			Extra: map[string]any{"kind": "file"},
		}}
	}

	payload := parseICVEResourcePayload(item.Info)
	if len(payload) == 0 {
		return nil
	}
	fileType := firstNonEmpty(
		item.Ext,
		str(payload["fileType"]),
		str(payload["file_type"]),
		str(payload["type"]),
		str(payload["suffix"]),
		str(payload["fileSuffix"]),
		pickExt(rawURL),
	)
	return buildICVEResourceEntries(x.c, x.headers, x.mode, payload, fileType, item.Name, "ai")
}

func (x *aiCtx) getVideoURL(videoInfo string) (string, string) {
	data := parseJSONMap(videoInfo)
	if len(data) == 0 {
		return "", ""
	}
	oriURL := str(data["ossOriUrl"])
	ext := pickExt(oriURL)
	if ext == "" {
		ext = "mp4"
	}
	if genURL := str(data["ossGenUrl"]); genURL != "" && strings.HasPrefix(genURL, "http") {
		if content := str(data["content"]); content != "" {
			statusBody, err := x.c.GetString(fmt.Sprintf(urlSourceStatus, strings.TrimLeft(content, "/")), x.headers)
			if err == nil {
				status := parseJSONMap(statusBody)
				if u := x.selectTranscodedURL(genURL, ext, status); u != "" {
					return u, pickExt(u)
				}
			}
		}
		if u := x.selectTranscodedURL(genURL, ext, map[string]any{}); u != "" {
			return u, pickExt(u)
		}
	}
	if oriURL != "" {
		return oriURL, ext
	}
	return str(data["url"]), pickExt(str(data["url"]))
}

func (x *aiCtx) getFileURL(fileInfo string) (string, string) {
	data := parseJSONMap(fileInfo)
	if len(data) == 0 {
		return "", ""
	}
	oriURL := str(data["ossOriUrl"])
	if oriURL != "" {
		return oriURL, pickExt(oriURL)
	}
	return str(data["url"]), pickExt(str(data["url"]))
}

func (x *aiCtx) selectTranscodedURL(genURL, originExt string, status map[string]any) string {
	args := mapAt(status, "args")
	qualityOrder := x.videoQualityCandidates()
	if q := x.selectVideoQuality(args); q != "" {
		qualityOrder = append([]string{q}, filterOtherQualities(qualityOrder, q)...)
	}
	extOrder := []string{"mp4", "m3u8"}
	if originExt == "m3u8" {
		extOrder = []string{"m3u8", "mp4"}
	}
	if typ := strings.ToLower(firstNonEmpty(str(status["type"]), str(args["type"]))); strings.Contains(typ, "m3u8") {
		extOrder = []string{"m3u8", "mp4"}
	}
	for _, q := range qualityOrder {
		for _, ext := range extOrder {
			u := fmt.Sprintf("%s/%s.%s", strings.TrimRight(genURL, "/"), q, ext)
			if x.checkURL(u) {
				return u
			}
		}
	}
	return ""
}

func (x *aiCtx) checkURL(raw string) bool {
	resp, err := x.c.Get(raw, x.headers)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return true
}

func (x *aiCtx) videoQualityCandidates() []string {
	switch x.mode {
	case IS_HD:
		return []string{"720p", "480p", "360p", "1080p"}
	case IS_SD:
		return []string{"480p", "360p", "720p", "1080p"}
	default:
		return []string{"360p", "480p", "720p", "1080p"}
	}
}

func (x *aiCtx) selectVideoQuality(args map[string]any) string {
	for _, q := range x.videoQualityCandidates() {
		v := args[q]
		if v == true || strings.EqualFold(str(v), "true") {
			return q
		}
	}
	return ""
}

func isDownloadableICVEURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:")
}
