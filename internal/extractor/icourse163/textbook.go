package icourse163

// Textbook (电子教材) flow ported from decompiled
// Mooc/Courses/Mooc163/Icourse163/Icourse163_Textbook.pyc.
//
// icourse163 side:
//   1. /textbook/{cid}.htm                                          -> title (page fallback)
//   2. textbookBean.getTextbookInfo.rpc                             -> textbookName / enroll / price
//   3. textbookBean.listTextBookCatalogAndUserProgress.rpc          -> catalogVos tree + ycFrontTextbookId
//
// HEP (etextbook.hep.com.cn) side:
//   4. GET  /app/AppInfo                           (unsigned)       -> Secret (AppSecret)
//   5. GET  /ThirdPlatForm/AuthorizeFunctionSearch  (signed, no AT) -> pre-auth
//   6. POST /Passport/ThirdPartyUserLoginRecord     (signed, no AT) -> AccessToken
//   7. POST /TableOfContent/GetList                 (signed, AT)    -> toc map with ExternalId
//   8. GET  /content/GetSectionToHep                (signed, AT)    -> HTML content per section
//
// Signing algorithm (fully recovered from bytecode,
// Icourse163_Textbook.pyc.1shot.das lines 2427-2773):
//
//   base_params = {X_Public_AppId, DeviceType="1", wesite="web",
//                  X_Public_From="4", X_Public_TimeStamp (UTC),
//                  X_Public_Nonce (uuid4)} + optionally X_Public_AccessToken
//   merged = base_params | user_params
//   filtered = {k: hep_value(v) for k, v in merged if hep_keep_value(v)}
//   entries  = sorted(["key_lower/Key=Value" for Key, Value in filtered])
//   sign_str = "".join(e.split("/", 1)[1] for e in entries) + secret
//   X_Public_Token = sha1(sign_str.encode("utf-8")).hexdigest()

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

// Constants ported verbatim from Icourse163_Textbook.
const (
	textbookInfoURL    = "https://www.icourse163.org/web/j/textbookBean.getTextbookInfo.rpc?csrfKey="
	textbookCatalogURL = "https://www.icourse163.org/web/j/textbookBean.listTextBookCatalogAndUserProgress.rpc?csrfKey="
	textbookPageURLFmt = "https://www.icourse163.org/textbook/%s.htm"
	hepAPIBase         = "https://etextbook.hep.com.cn/ebookapi"
	hepAppID           = "1281503927916822528"
)

// Source pattern: courses_re['Icourse163_Textbook'].
var textbookPatterns = []string{
	`(?:www\.)?icourse163\.org/textbook/(?:learn/)?\d+\.htm`,
}

var textbookURLRe = regexp.MustCompile(
	`^https?://www\.icourse163\.org/textbook/(?:learn/)?(?P<cid>\d+)\.htm`,
)

func init() {
	extractor.Register(&ICourse163Textbook{}, extractor.SiteInfo{
		Name:     "icourse163_textbook",
		URL:      "icourse163.org/textbook",
		NeedAuth: true,
	})
}

type ICourse163Textbook struct{}

func (t *ICourse163Textbook) Patterns() []string { return textbookPatterns }

func parseTextbookURL(rawURL string) (string, bool) {
	m := textbookURLRe.FindStringSubmatch(rawURL)
	if m == nil {
		return "", false
	}
	cid := m[textbookURLRe.SubexpIndex("cid")]
	return cid, cid != ""
}

func (t *ICourse163Textbook) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("icourse163 textbook requires login cookies (use --cookies or --cookies-from-browser)")
	}
	cid, ok := parseTextbookURL(rawURL)
	if !ok {
		return nil, fmt.Errorf("cannot parse icourse163 textbook URL: %s", rawURL)
	}

	c := newClient(opts.Cookies)

	title := fmt.Sprintf("textbook_%s", cid)
	info, _ := fetchTextbookInfo(c, cid)
	if info.name != "" {
		title = sanitize(info.name)
	} else if page, err := c.GetString(fmt.Sprintf(textbookPageURLFmt, cid), textbookHeaders(cid)); err == nil {
		if pt := titleFromPage(page, ""); pt != "" {
			title = pt
		}
	}

	catalog, err := fetchTextbookCatalog(c, cid)
	if err != nil {
		return nil, fmt.Errorf("listTextBookCatalogAndUserProgress: %w", err)
	}
	leaves := flattenTextbookCatalog(catalog.catalogVos, nil)
	if len(leaves) == 0 {
		return nil, fmt.Errorf("textbook %s has no readable catalog entries (not enrolled or empty)", cid)
	}

	// --- HEP content download ---
	h := &hepClient{
		client:     c,
		cid:        cid,
		appID:      hepAppID,
		apiBase:    hepAPIBase,
		refererURL: fmt.Sprintf(textbookPageURLFmt, cid),
	}

	// Step 1: get secret from /app/AppInfo
	if err := h.getSecret(); err != nil {
		return nil, fmt.Errorf("HEP getSecret: %w", err)
	}

	// Step 2: login to HEP (get icourse user ID, then ThirdPartyUserLoginRecord)
	if err := h.login(c); err != nil {
		return nil, fmt.Errorf("HEP login: %w", err)
	}

	// Step 3: get toc map
	tocMap, err := h.getTocMap(catalog.ycFrontTextbookID, leaves)
	if err != nil {
		return nil, fmt.Errorf("HEP GetList: %w", err)
	}

	// Step 4: fetch each section's content
	var entries []*extractor.MediaInfo
	for i, lf := range leaves {
		toc := tocMap[lf.externalID]
		// Skip if toc says it's a data file
		if toc != nil {
			if df, ok := toc["IsGetDataFile"]; ok {
				if dfBool, ok := df.(bool); ok && dfBool {
					continue
				}
			}
		}

		section, err := h.getSection(lf.externalID)
		if err != nil {
			continue
		}
		content, _ := section["Content"].(string)
		if content == "" {
			continue
		}

		sectionTitle := lf.title
		if toc != nil {
			if t, ok := toc["Title"].(string); ok && t != "" {
				sectionTitle = t
			}
		}

		entries = append(entries, &extractor.MediaInfo{
			Site:  "icourse163_textbook",
			Title: sanitize(sectionTitle),
			Extra: map[string]any{
				"index":       i + 1,
				"external_id": lf.externalID,
				"content":     normalizeContentHTML(content),
			},
		})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("textbook %q (id=%s): no content sections could be fetched from HEP", title, cid)
	}

	return &extractor.MediaInfo{
		Site:    "icourse163_textbook",
		Title:   title,
		Entries: entries,
		Extra: map[string]any{
			"textbook_id":    cid,
			"hep_book_id":    catalog.ycFrontTextbookID,
			"enrolled":       info.enroll,
			"total_sections": len(leaves),
			"fetched":        len(entries),
		},
	}, nil
}

// ---------- HEP API client ----------

type hepClient struct {
	client      *util.Client
	cid         string
	appID       string
	apiBase     string
	refererURL  string
	secret      string
	accessToken string
}

// hepHeaders returns the HTTP headers for HEP API calls.
func (h *hepClient) hepHeaders() map[string]string {
	return map[string]string{
		"Referer":    h.refererURL,
		"Origin":     "https://www.icourse163.org",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}
}

// hepValue converts a value to its string representation for signing.
// Matches Icourse163_Textbook._hep_value (bytecode lines 2326-2362).
func hepValue(v string) string {
	return v
}

// hepKeepValue decides whether to include a k/v pair in signing.
// Matches Icourse163_Textbook._hep_keep_value (bytecode lines 2389-2423):
//
//	return bool(value) or value == 0 or isinstance(value, bool)
//
// For our Go implementation, all string values are kept unless empty.
func hepKeepValue(v string) bool {
	return v != ""
}

// hepSignedParams implements the X_Public_Token signing algorithm fully
// recovered from bytecode (Icourse163_Textbook.pyc.1shot.das lines 2427-2773).
//
// Algorithm:
//  1. Build base param map with standard HEP fields
//  2. Merge user params
//  3. Filter out empty values
//  4. For each (key, value) pair, build "key_lower/Key=Value"
//  5. Sort these strings alphabetically
//  6. Extract "Key=Value" (split on "/" take [1])
//  7. Concatenate all Key=Value strings with NO separator
//  8. Append the HEP secret
//  9. SHA1 hex digest of the UTF-8 encoded string
func (h *hepClient) hepSignedParams(params map[string]string, includeAccessToken bool) map[string]string {
	if params == nil {
		params = make(map[string]string)
	}

	// Base params (bytecode lines 2620-2667)
	base := map[string]string{
		"X_Public_AppId":     h.appID,
		"DeviceType":         "1",
		"wesite":             "web",
		"X_Public_From":      "4",
		"X_Public_TimeStamp": time.Now().UTC().Format("2006-01-02 15:04:05"),
		"X_Public_Nonce":     uuid.New().String(),
	}
	if includeAccessToken && h.accessToken != "" {
		base["X_Public_AccessToken"] = h.accessToken
	}

	// Merge user params into base
	for k, v := range params {
		base[k] = v
	}

	// Filter: keep only non-empty values (hep_keep_value)
	filtered := make(map[string]string)
	for k, v := range base {
		if hepKeepValue(v) {
			filtered[k] = hepValue(v)
		}
	}

	// Build sort entries: "key_lower/Key=Value"
	// Bytecode listcomp (lines 2534-2552): '{}{}{}={}'.format(k.lower(), '/', k, v)
	sortEntries := make([]string, 0, len(filtered))
	for k, v := range filtered {
		sortEntries = append(sortEntries, fmt.Sprintf("%s/%s=%s", strings.ToLower(k), k, v))
	}
	sort.Strings(sortEntries)

	// Extract Key=Value parts and concatenate
	// Bytecode genexpr (lines 2579-2593): item.split('/', 1)[1]
	var signParts []string
	for _, entry := range sortEntries {
		idx := strings.Index(entry, "/")
		if idx >= 0 {
			signParts = append(signParts, entry[idx+1:])
		}
	}

	// sign_str = "".join(parts) + self._hep_secret
	signStr := strings.Join(signParts, "") + h.secret

	// X_Public_Token = sha1(sign_str.encode('utf-8')).hexdigest()
	sum := sha1.Sum([]byte(signStr))
	filtered["X_Public_Token"] = hex.EncodeToString(sum[:])

	return filtered
}

// hepJSON makes a signed request to the HEP API.
// Matches Icourse163_Textbook._hep_json (bytecode lines 2777-2844).
func (h *hepClient) hepJSON(method, path string, params map[string]string, includeAccessToken, signed bool) (map[string]any, error) {
	fullURL := strings.TrimRight(h.apiBase, "/") + "/" + strings.TrimLeft(path, "/")

	if params == nil {
		params = make(map[string]string)
	}
	if signed {
		params = h.hepSignedParams(params, includeAccessToken)
	}

	var body string
	var err error

	if strings.EqualFold(method, "POST") {
		body, err = h.client.PostForm(fullURL, params, h.hepHeaders())
	} else {
		// For GET, append params as query string
		body, err = h.client.GetString(fullURL+"?"+buildQuery(params), h.hepHeaders())
	}
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, fmt.Errorf("HEP JSON decode: %w", err)
	}
	return result, nil
}

// getSecret fetches the AppSecret from /app/AppInfo.
// Matches Icourse163_Textbook._hep_get_secret (bytecode lines 3022-3253).
func (h *hepClient) getSecret() error {
	if h.secret != "" {
		return nil
	}
	// AppInfo is called with signed=True but access_token=False
	// (first call bootstraps the secret; the initial secret is empty so the
	// signature is just SHA1("" + "") but the server accepts it for this endpoint)
	result, err := h.hepJSON("GET", "app/AppInfo", nil, false, true)
	if err != nil {
		return fmt.Errorf("AppInfo request: %w", err)
	}

	data, _ := result["Data"].(map[string]any)
	if data == nil {
		return fmt.Errorf("AppInfo: no Data in response")
	}

	// Try Secret, secret, AppSecret (matches source constant order)
	for _, key := range []string{"Secret", "secret", "AppSecret"} {
		if v, ok := data[key].(string); ok && v != "" {
			h.secret = v
			return nil
		}
	}
	return fmt.Errorf("AppInfo: no Secret/AppSecret in Data")
}

// login performs HEP third-party login using the icourse163 user ID.
// Matches Icourse163_Textbook._hep_login (bytecode lines 3475-3685).
func (h *hepClient) login(c *util.Client) error {
	if h.accessToken != "" {
		return nil
	}
	if h.secret == "" {
		return fmt.Errorf("secret not yet obtained")
	}

	// Get icourse163 user ID from page
	userID, err := getICourseUserID(c, h.cid)
	if err != nil {
		return fmt.Errorf("get icourse user ID: %w", err)
	}
	if userID == "" {
		return fmt.Errorf("cannot find icourse163 user ID (not logged in?)")
	}

	// Step 1: AuthorizeFunctionSearch (access_token=False)
	_, _ = h.hepJSON("GET", "ThirdPlatForm/AuthorizeFunctionSearch", map[string]string{
		"PlatformId": h.appID,
	}, false, true)

	// Step 2: ThirdPartyUserLoginRecord
	dataJSON := fmt.Sprintf(`{"Hep_id":"%s"}`, userID)
	result, err := h.hepJSON("POST", "Passport/ThirdPartyUserLoginRecord", map[string]string{
		"dataJson": dataJSON,
	}, false, true)
	if err != nil {
		return fmt.Errorf("ThirdPartyUserLoginRecord: %w", err)
	}

	data, _ := result["Data"].(map[string]any)
	if data == nil {
		return fmt.Errorf("ThirdPartyUserLoginRecord: no Data in response")
	}
	at, _ := data["AccessToken"].(string)
	if at == "" {
		return fmt.Errorf("ThirdPartyUserLoginRecord: no AccessToken in Data")
	}
	h.accessToken = at
	return nil
}

// getTocMap fetches the table of contents mapping from HEP.
// Matches Icourse163_Textbook._get_hep_toc_map (bytecode lines 3686-3934).
func (h *hepClient) getTocMap(bookID string, leaves []textbookLeaf) (map[string]map[string]any, error) {
	// Build comma-separated ExternalIds
	ids := make([]string, 0, len(leaves))
	for _, lf := range leaves {
		if lf.externalID != "" {
			ids = append(ids, lf.externalID)
		}
	}

	result, err := h.hepJSON("POST", "TableOfContent/GetList", map[string]string{
		"bookId":      bookID,
		"ExternalIds": strings.Join(ids, ","),
	}, true, true)
	if err != nil {
		return nil, err
	}

	tocMap := make(map[string]map[string]any)
	data, _ := result["Data"].(map[string]any)
	if data == nil {
		return tocMap, nil
	}
	dataList, _ := data["data"].([]any)
	for _, item := range dataList {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		extID, _ := entry["ExternalId"].(string)
		if extID != "" {
			tocMap[extID] = entry
		}
	}
	return tocMap, nil
}

// getSection fetches a single content section from HEP.
// Matches Icourse163_Textbook._get_hep_section (bytecode lines 3935-4041).
func (h *hepClient) getSection(externalID string) (map[string]any, error) {
	result, err := h.hepJSON("GET", "content/GetSectionToHep", map[string]string{
		"ExternalId": externalID,
	}, true, true)
	if err != nil {
		return nil, err
	}

	data, _ := result["Data"].(map[string]any)
	if data == nil {
		return nil, fmt.Errorf("GetSectionToHep: no Data for %s", externalID)
	}
	return data, nil
}

// getICourseUserID extracts the user ID from the textbook page or home page.
// Matches Icourse163_Textbook._get_icourse_user_id (bytecode lines 3254-3474).
func getICourseUserID(c *util.Client, cid string) (string, error) {
	urls := []string{
		fmt.Sprintf(textbookPageURLFmt, cid),
		homeURL,
	}

	userIDRe1 := regexp.MustCompile(`id\s*:\s*"?(\d+)"?\s*,\s*nickName`)
	userIDRe2 := regexp.MustCompile(`"id"\s*:\s*"?(\d+)"?[\s\S]{0,200}?"nickName"`)

	for _, u := range urls {
		page, err := c.GetString(u, headers())
		if err != nil {
			continue
		}
		if m := userIDRe1.FindStringSubmatch(page); len(m) > 1 {
			return m[1], nil
		}
		if m := userIDRe2.FindStringSubmatch(page); len(m) > 1 {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("user ID not found in any page")
}

// ---------- icourse163 catalog API ----------

func textbookHeaders(cid string) map[string]string {
	return map[string]string{
		"Content-Type":     "application/x-www-form-urlencoded; charset=UTF-8",
		"X-Requested-With": "XMLHttpRequest",
		"Origin":           "https://www.icourse163.org",
		"Referer":          fmt.Sprintf(textbookPageURLFmt, cid),
	}
}

type textbookInfo struct {
	name   string
	enroll bool
}

func fetchTextbookInfo(c *util.Client, cid string) (textbookInfo, error) {
	body, err := c.PostForm(textbookInfoURL+srckey, map[string]string{
		"textbookId": cid,
	}, textbookHeaders(cid))
	if err != nil {
		return textbookInfo{}, err
	}
	var out struct {
		Result struct {
			TextbookName string `json:"textbookName"`
			Name         string `json:"name"`
			Enroll       bool   `json:"enroll"`
		} `json:"result"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return textbookInfo{}, err
	}
	return textbookInfo{
		name:   firstNonEmpty(out.Result.TextbookName, out.Result.Name),
		enroll: out.Result.Enroll,
	}, nil
}

type textbookCatalogResult struct {
	ycFrontTextbookID string
	catalogVos        []rawCatalogNode
}

type rawCatalogNode struct {
	ID          any              `json:"id"`
	Title       string           `json:"title"`
	YcCatalogID any              `json:"ycCatalogId"`
	Leaf        bool             `json:"leaf"`
	Trial       bool             `json:"trial"`
	Children    []rawCatalogNode `json:"children"`
}

func fetchTextbookCatalog(c *util.Client, cid string) (textbookCatalogResult, error) {
	body, err := c.PostForm(textbookCatalogURL+srckey, map[string]string{
		"textbookId": cid,
	}, textbookHeaders(cid))
	if err != nil {
		return textbookCatalogResult{}, err
	}
	var out struct {
		Result struct {
			YcFrontTextbookID any              `json:"ycFrontTextbookId"`
			CatalogVos        []rawCatalogNode `json:"catalogVos"`
		} `json:"result"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return textbookCatalogResult{}, err
	}
	return textbookCatalogResult{
		ycFrontTextbookID: valueString(out.Result.YcFrontTextbookID),
		catalogVos:        out.Result.CatalogVos,
	}, nil
}

type textbookLeaf struct {
	title      string
	externalID string
}

// flattenTextbookCatalog mirrors Icourse163_Textbook._flatten_catalogs: leaf
// nodes (no children) with a ycCatalogId become downloadable sections.
func flattenTextbookCatalog(nodes []rawCatalogNode, path []string) []textbookLeaf {
	var out []textbookLeaf
	for _, n := range nodes {
		title := sanitize(n.Title)
		childPath := path
		if title != "" {
			childPath = append(append([]string{}, path...), title)
		}
		extID := valueString(n.YcCatalogID)
		if len(n.Children) == 0 && extID != "" {
			name := title
			if name == "" {
				name = extID
			}
			out = append(out, textbookLeaf{title: name, externalID: extID})
		}
		if len(n.Children) > 0 {
			out = append(out, flattenTextbookCatalog(n.Children, childPath)...)
		}
	}
	return out
}

// ---------- helpers ----------

// buildQuery constructs a URL-encoded query string from a map.
func buildQuery(params map[string]string) string {
	v := url.Values{}
	for k, val := range params {
		v.Set(k, val)
	}
	return v.Encode()
}

// normalizeContentHTML cleans up HEP content HTML.
// Matches Icourse163_Textbook._normalize_content_html.
func normalizeContentHTML(content string) string {
	if content == "" {
		return ""
	}
	// Remove script tags
	scriptRe := regexp.MustCompile(`(?i)<script[\s\S]*?</script\s*>`)
	content = scriptRe.ReplaceAllString(content, "")

	// Fix protocol-relative image URLs
	imgRe := regexp.MustCompile(`(?i)(<img\b[^>]*?\s(?:src|data-src)=["'])//`)
	content = imgRe.ReplaceAllString(content, "${1}https://")

	return content
}
