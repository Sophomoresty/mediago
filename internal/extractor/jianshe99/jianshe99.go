package jianshe99

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	MEMBER_HOME_URL       = "https://member.jianshe99.com/homes/mycourse"
	MEMBER_ORIGIN         = "https://member.jianshe99.com"
	ELEARNING_HOME_URL    = "https://elearning.jianshe99.com/"
	DOORMAN_BASE_URL      = "https://gateway.jianshe99.com/ln/op/"
	DOORMAN_DOMAIN        = "jianshe99"
	SITE_ID               = "4"
	MATERIALS_URL         = "https://elearning.jianshe99.com/xcware/myhome/teachingMaterials.shtm?cwareID=%s&identity=%s"
	material_download_url = "https://elearning.jianshe99.com/data2file/downloadFile/getVideoJyFileDocx?fileUrl=%s&fileName=%s"

	// Doorman crypto constants are shared platform-wide (defined in
	// cdeledu's Zhengbao_Config and resolved via that module's namespace
	// even when called from the jianshe99 subclass). Reused verbatim.
	doorman_app_id  = "b3316459-ceeb-47f8-a469-12751ff3075e"
	doorman_aes_key = "823s4125660ijf;*"
	doorman_aes_iv  = "qyu148#4(1p_1^4;"

	course_group_path    = "~/c-home/w-home/f/ru/userCourseClassList"
	course_detail_path   = "~/c-home/w-home/f/ru/getUserHomeCourse"
	course_subject_path  = "~/c-home/a-home/f/ru/getUserHomeCourse"
	live_replay_referer  = "https://live.cdeledu.com/"
	live_replay_info_url = "https://live.cdeledu.com/liveapi/entry/getReplayInfo"
	cc_replay_login_url  = "https://view.csslcloud.net/api/room/replay/login"
	cc_replay_vod_url    = "https://view.csslcloud.net/api/record/vod"
	cc_replay_version    = "3.6.1"
	video_list_url       = "https://elearning.jianshe99.com/xcware/video/videoList/videoList.shtm?cwareID=%s&courseIds=%s"
)

var patterns = []string{`(?:[\w-]+\.)?jianshe99\.com/|(?:[\w-]+\.)?cdeledu\.com/`}

func init() {
	extractor.Register(&Jianshe99{}, extractor.SiteInfo{Name: "Jianshe99", URL: "jianshe99.com", NeedAuth: true})
}

type Jianshe99 struct{}

func (j *Jianshe99) Patterns() []string { return patterns }

var (
	cwareRe        = regexp.MustCompile(`(?i)cwareID=([^&#]+)`)
	courseIDsRe    = regexp.MustCompile(`(?i)(?:courseIds|courseId)=([^&#]+)`)
	identityRe     = regexp.MustCompile(`(?i)identity=([^&#]+)`)
	anchorRe       = regexp.MustCompile(`(?is)<a\b[^>]*(?:href|onclick)=["'][^"']*(?:videoPlay|continueStudyVideo|/dispatch/th/live/callback/play)[^"']*["'][^>]*>.*?</a>`)
	urlInAttrRe    = regexp.MustCompile(`(?is)(https?:)?//[^"'<> ]+/dispatch/th/live/callback/play[^"'<> ]*|/dispatch/th/live/callback/play[^"'<> ]*`)
	onclickArgRe   = regexp.MustCompile(`(?is)(?:window\.open|videoPlay|continueStudyVideo)\(\s*["']([^"']+)["']`)
	videoIDRe      = regexp.MustCompile(`(?i)(?:videoID|videoId|video_id)=([0-9A-Za-z_\-]+)`)
	h5VarsRe       = regexp.MustCompile(`window\.cdelmedia\.h5Vars\s*=\s*JSON\.parse\('(?s:(.*?))'\)`)
	attrRe         = regexp.MustCompile(`(?i)(data-[a-z0-9_-]+)\s*=\s*["']([^"']+)["']`)
	htmlTitleRe    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	fieldStringRe  = regexp.MustCompile(`(?is)["']?%s["']?\s*[:=]\s*["']([^"']+)["']`)
	m3u8URLPattern = regexp.MustCompile(`(?i)\.m3u8(?:[?#].*)?$`)
)

func (j *Jianshe99) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("jianshe99 requires login cookies (use --cookies or --cookies-from-browser)")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := map[string]string{"Referer": MEMBER_HOME_URL, "Origin": MEMBER_ORIGIN}

	// A single live-replay callback URL: resolve it directly via csslcloud.
	if isLiveReplayURL(rawURL) {
		entry, err := resolveReplay(c, rawURL, rawURL, "jianshe99_replay", nil)
		if err != nil {
			return nil, err
		}
		return entry, nil
	}

	// Resolve the set of courseware pages (videoList.shtm) to walk. When the
	// URL already points at a videoList page (or carries a cwareID), use it
	// directly; otherwise fall back to the doorman gateway to discover the
	// course's courseware list from a bare courseId.
	cwares := buildCwarePages(c, opts.Cookies, rawURL)
	if len(cwares) == 0 {
		return nil, fmt.Errorf("cannot parse jianshe99 cwareID from URL and doorman discovery returned nothing: %s", rawURL)
	}
	uid := uidFromJar(opts.Cookies)

	var entries []*extractor.MediaInfo
	courseTitle := ""
	for ci, cw := range cwares {
		body, err := c.GetString(cw.PageURL, headers)
		if err != nil {
			continue
		}
		if courseTitle == "" {
			courseTitle = extractHTMLTitle(body)
		}
		lessons := parseLessons(body)
		for i, lesson := range lessons {
			title := firstNonEmpty(lesson.Title, fmt.Sprintf("课时%d", i+1))
			if lesson.Kind == lessonReplay {
				entry, err := resolveReplay(c, lesson.PlayURL, cw.PageURL, title, []byte(body))
				if err != nil {
					continue
				}
				entries = append(entries, entry)
				continue
			}
			entry, err := resolveVOD(c, lesson, cw.PageURL, fmt.Sprintf("[%d.%d]--%s", ci+1, i+1, title))
			if err != nil {
				continue
			}
			entries = append(entries, entry)
		}
		// Teaching materials attached to this courseware.
		entries = append(entries, parseMaterialTree(c, cw, uid)...)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("jianshe99: parsed courseware but no playable media or material resolved")
	}
	return &extractor.MediaInfo{
		Site:    "jianshe99",
		Title:   util.SanitizeFilename(firstNonEmpty(courseTitle, cwares[0].CwareID, "jianshe99")),
		Entries: entries,
	}, nil
}

type lessonKind int

const (
	lessonVOD lessonKind = iota
	lessonReplay
)

type lessonRef struct {
	PlayURL  string
	Title    string
	VideoID  string
	CwareID  string
	Identity string
	Kind     lessonKind
}

type cwarePage struct {
	PageURL  string
	CwareID  string
	Identity string
}

func parseLessons(body string) []lessonRef {
	seen := map[string]bool{}
	var out []lessonRef
	for _, m := range anchorRe.FindAllString(body, -1) {
		raw := firstNonEmpty(extractFirst(onclickArgRe, m), extractFirst(urlInAttrRe, m))
		u := normalizeURL(html.UnescapeString(raw))
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		kind := lessonVOD
		if isLiveReplayURL(u) {
			kind = lessonReplay
		}
		out = append(out, lessonRef{
			PlayURL: u,
			Title:   cleanText(stripTags(m)),
			VideoID: extractFirst(videoIDRe, u),
			Kind:    kind,
		})
	}
	if len(out) == 0 {
		for _, u := range urlInAttrRe.FindAllString(body, -1) {
			u = normalizeURL(html.UnescapeString(u))
			if u != "" && !seen[u] {
				seen[u] = true
				out = append(out, lessonRef{PlayURL: u, Kind: lessonReplay})
			}
		}
	}
	return out
}

// resolveVOD resolves a recorded (VOD) lesson by loading its play page and
// extracting window.cdelmedia.h5Vars (videoPath m3u8 + srtPath subtitle).
func resolveVOD(c *util.Client, lesson lessonRef, referer, title string) (*extractor.MediaInfo, error) {
	playURL := normalizeURL(lesson.PlayURL)
	if playURL == "" {
		return nil, fmt.Errorf("jianshe99 vod: empty play URL")
	}
	videoURL := playURL
	format := pickFormat(videoURL)
	extra := map[string]any{"video_id": lesson.VideoID, "play_page": playURL}

	// If the anchor URL is not already a direct media URL, the play page holds
	// h5Vars with the real videoPath.
	if !m3u8URLPattern.MatchString(playURL) && !strings.Contains(strings.ToLower(playURL), ".mp4") {
		body, err := c.GetString(playURL, map[string]string{"Referer": ELEARNING_HOME_URL})
		if err != nil {
			return nil, err
		}
		vars := parseH5Vars(body)
		if len(vars) == 0 {
			return nil, fmt.Errorf("jianshe99 vod: no h5Vars in play page")
		}
		if p := firstString(vars, "videoPath", "video_path", "path", "url"); p != "" {
			videoURL = normalizeURL(strings.ReplaceAll(p, `\/`, `/`))
			format = pickFormat(videoURL)
		}
		if sub := firstString(vars, "srtPath", "subtitle", "subtitleUrl"); sub != "" {
			extra["subtitle"] = normalizeURL(strings.ReplaceAll(sub, `\/`, `/`))
		}
	}
	if videoURL == "" {
		return nil, fmt.Errorf("jianshe99 vod: no videoPath resolved")
	}
	return &extractor.MediaInfo{
		Site:  "jianshe99",
		Title: util.SanitizeFilename(firstNonEmpty(title, lesson.VideoID)),
		Streams: map[string]extractor.Stream{
			"best": {
				Quality:   "source",
				URLs:      []string{videoURL},
				Format:    format,
				NeedMerge: format == "m3u8",
				Headers:   map[string]string{"Referer": ELEARNING_HOME_URL},
			},
		},
		Extra: extra,
	}, nil
}

// parseH5Vars decodes the JSON.parse('...') payload assigned to
// window.cdelmedia.h5Vars on a play page.
func parseH5Vars(body string) map[string]any {
	m := h5VarsRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return nil
	}
	escaped := strings.ReplaceAll(m[1], `\/`, `/`)
	escaped = strings.ReplaceAll(escaped, `\'`, `'`)
	quoted := `"` + strings.ReplaceAll(escaped, `"`, `\"`) + `"`
	if unquoted, err := strconv.Unquote(quoted); err == nil {
		escaped = unquoted
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(escaped), &out); err != nil {
		return nil
	}
	return out
}

// parseMaterialTree fetches the teaching-materials page for a courseware and
// returns one downloadable entry per attached document.
func parseMaterialTree(c *util.Client, cw cwarePage, uid string) []*extractor.MediaInfo {
	if cw.CwareID == "" {
		return nil
	}
	api := fmt.Sprintf(MATERIALS_URL, url.QueryEscape(cw.CwareID), url.QueryEscape(cw.Identity))
	body, err := c.GetString(api, map[string]string{"Referer": ELEARNING_HOME_URL})
	if err != nil || body == "" {
		return nil
	}
	specs := []struct{ Key, Format, Suffix string }{
		{"data-fileurl", "docx", ""},
		{"data-pdfurl", "pdf", ""},
		{"data-sepurl", "docx", "-答案分离"},
		{"data-seppdfurl", "pdf", "-答案分离"},
	}
	var out []*extractor.MediaInfo
	seen := map[string]bool{}
	for _, attrs := range extractAttrs(body) {
		name := firstNonEmpty(attrs["data-videoname"], attrs["title"], "课程资料")
		for _, spec := range specs {
			tok := strings.TrimSpace(attrs[spec.Key])
			if tok == "" || seen[spec.Key+tok] {
				continue
			}
			seen[spec.Key+tok] = true
			fileName := util.SanitizeFilename(name + spec.Suffix)
			dl := buildMaterialURL(tok, fileName, spec.Format, uid)
			out = append(out, &extractor.MediaInfo{
				Site:  "jianshe99",
				Title: fileName,
				Streams: map[string]extractor.Stream{
					"default": {
						Quality: "source",
						URLs:    []string{dl},
						Format:  spec.Format,
						Headers: map[string]string{"Referer": ELEARNING_HOME_URL},
					},
				},
			})
		}
	}
	return out
}

// buildMaterialURL formats the jianshe99 material download endpoint. fileUrl is
// the raw token from the data-*url attribute; fileName is the display name. For
// doc/pdf formats the source appends a percent-encoded "&uid={}&type=word|pdf"
// segment onto the formatted URL.
func buildMaterialURL(fileToken, fileName, fmtHint, uid string) string {
	base := fmt.Sprintf(material_download_url, url.QueryEscape(fileToken), url.QueryEscape(fileName))
	switch strings.ToLower(fmtHint) {
	case "doc", "docx", "word":
		return base + url.QueryEscape(fmt.Sprintf("&uid=%s&type=word", uid))
	case "pdf":
		return base + url.QueryEscape(fmt.Sprintf("&uid=%s&type=pdf", uid))
	}
	return base
}

func extractAttrs(body string) []map[string]string {
	tagRe := regexp.MustCompile(`(?is)<[^>]+>`)
	var out []map[string]string
	for _, tag := range tagRe.FindAllString(body, -1) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = strings.ReplaceAll(m[2], `\/`, `/`)
		}
		if len(attrs) > 0 {
			out = append(out, attrs)
		}
	}
	return out
}

func resolveReplay(c *util.Client, playURL, referer, title string, pageBody []byte) (*extractor.MediaInfo, error) {
	payload, err := fetchReplayPayload(c, playURL, referer)
	if err != nil && len(pageBody) > 0 {
		payload = payloadFromText(string(pageBody))
	}
	if err != nil && payload.LiveRoomID == "" {
		payload = payloadFromQuery(playURL)
	}
	if payload.LiveRoomID == "" || payload.AccessID == "" || payload.RecordID == "" {
		return nil, fmt.Errorf("jianshe99 replay payload missing liveRoomId/accessid/recordId")
	}
	payload.Referer = live_replay_referer
	playInfo, err := shared.CssLcloudResolvePlayInfo(c, payload)
	if err != nil {
		return nil, err
	}

	extra := map[string]any{"session_id": playInfo.SessionID, "record_id": payload.RecordID}
	if m3u8URLPattern.MatchString(playInfo.VideoURL) {
		text, err := c.GetString(playInfo.VideoURL, map[string]string{"Referer": live_replay_referer})
		if err == nil {
			if prepared, err := shared.CssLcloudRewriteM3U8Keys(c, text, live_replay_referer); err == nil {
				extra["prepared_m3u8_text"] = prepared
			}
		}
	}
	return &extractor.MediaInfo{
		Site:  "jianshe99",
		Title: util.SanitizeFilename(firstNonEmpty(title, payload.RecordID)),
		Streams: map[string]extractor.Stream{
			"best": {
				Quality:  "best",
				URLs:     []string{playInfo.VideoURL},
				Format:   pickFormat(playInfo.VideoURL),
				AudioURL: playInfo.AudioURL,
				Headers:  map[string]string{"Referer": live_replay_referer},
			},
		},
		Extra: extra,
	}, nil
}

type replayInfoResponse struct {
	Result any `json:"result"`
	Data   struct {
		Replay replayPayload `json:"replay"`
		Vod    replayPayload `json:"vod"`
	} `json:"data"`
	Replay replayPayload `json:"replay"`
	Vod    replayPayload `json:"vod"`
}

type replayPayload struct {
	LiveRoomID  string `json:"liveRoomId"`
	LiveID      string `json:"liveId"`
	RoomID      string `json:"roomid"`
	AccessID    string `json:"accessid"`
	AccessKey   string `json:"accesskey"`
	RecordID    string `json:"recordId"`
	RecordIDAlt string `json:"recordid"`
	UserID      string `json:"userid"`
	UID         string `json:"uid"`
	ViewerName  string `json:"viewername"`
	ViewerToken string `json:"viewertoken"`
	Token       string `json:"token"`
}

func fetchReplayPayload(c *util.Client, playURL, referer string) (shared.CssLcloudPayload, error) {
	api := live_replay_info_url + "?url=" + url.QueryEscape(stripFragment(playURL))
	body, err := c.GetString(api, map[string]string{"Referer": firstNonEmpty(referer, live_replay_referer)})
	if err != nil {
		return shared.CssLcloudPayload{}, err
	}
	var resp replayInfoResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return shared.CssLcloudPayload{}, err
	}
	candidates := []replayPayload{resp.Data.Replay, resp.Data.Vod, resp.Replay, resp.Vod}
	for _, p := range candidates {
		if out := normalizePayload(p); out.LiveRoomID != "" || out.RecordID != "" {
			return out, nil
		}
	}
	return payloadFromText(body), nil
}

func normalizePayload(p replayPayload) shared.CssLcloudPayload {
	liveID := firstNonEmpty(p.LiveRoomID, p.LiveID, p.RoomID)
	userID := firstNonEmpty(p.UserID, p.UID)
	token := firstNonEmpty(p.ViewerToken, p.Token)
	return shared.CssLcloudPayload{
		LiveRoomID:  liveID,
		UserID:      userID,
		AccessID:    firstNonEmpty(p.AccessID, p.AccessKey),
		RecordID:    firstNonEmpty(p.RecordID, p.RecordIDAlt),
		ViewerName:  firstNonEmpty(p.ViewerName, userID),
		ViewerToken: token,
	}
}

func payloadFromText(s string) shared.CssLcloudPayload {
	return shared.CssLcloudPayload{
		LiveRoomID:  firstField(s, "liveRoomId", "liveId", "roomid", "liveid"),
		UserID:      firstField(s, "userid", "uid", "userId"),
		AccessID:    firstField(s, "accessid", "accessId", "accesskey"),
		RecordID:    firstField(s, "recordId", "recordid"),
		ViewerName:  firstField(s, "viewername", "viewerName"),
		ViewerToken: firstField(s, "viewertoken", "viewerToken", "token"),
	}
}

func payloadFromQuery(raw string) shared.CssLcloudPayload {
	u, err := url.Parse(raw)
	if err != nil {
		return shared.CssLcloudPayload{}
	}
	q := u.Query()
	return shared.CssLcloudPayload{
		LiveRoomID:  firstNonEmpty(q.Get("liveRoomId"), q.Get("liveId"), q.Get("roomid"), q.Get("liveid")),
		UserID:      firstNonEmpty(q.Get("userid"), q.Get("uid")),
		AccessID:    firstNonEmpty(q.Get("accessid"), q.Get("accessId"), q.Get("accesskey")),
		RecordID:    firstNonEmpty(q.Get("recordId"), q.Get("recordid")),
		ViewerName:  firstNonEmpty(q.Get("viewername"), q.Get("userid")),
		ViewerToken: firstNonEmpty(q.Get("viewertoken"), q.Get("viewerToken"), q.Get("token")),
	}
}

// buildCwarePages determines the videoList.shtm pages to walk. A direct
// videoList URL or a URL carrying a cwareID yields a single page. Otherwise,
// when only a courseId is present, the doorman gateway is used to discover the
// course's recorded courseware.
func buildCwarePages(c *util.Client, jar http.CookieJar, raw string) []cwarePage {
	if strings.Contains(raw, "videoList.shtm") {
		return []cwarePage{{PageURL: raw, CwareID: extractFirst(cwareRe, raw), Identity: extractFirst(identityRe, raw)}}
	}
	if cwareID := extractFirst(cwareRe, raw); cwareID != "" {
		courseIDs := extractFirst(courseIDsRe, raw)
		page := fmt.Sprintf(video_list_url, url.QueryEscape(cwareID), url.QueryEscape(courseIDs))
		return []cwarePage{{PageURL: page, CwareID: cwareID, Identity: extractFirst(identityRe, raw)}}
	}
	return discoverCwaresViaDoorman(c, jar, raw)
}

func discoverCwaresViaDoorman(c *util.Client, jar http.CookieJar, raw string) []cwarePage {
	courseID := extractFirst(courseIDsRe, raw)
	if courseID == "" {
		return nil
	}
	d := newDoorman(c, jar)
	if d.uid == "" || d.sid == "" {
		return nil
	}
	// Resolve subject ids for this course (eduSubjectID is required by the
	// courseware-detail query). Fall back to a single empty subject so the
	// query is still attempted.
	subjectIDs := d.discoverSubjectIDs(courseID)
	if len(subjectIDs) == 0 {
		subjectIDs = []string{""}
	}
	seen := map[string]bool{}
	var out []cwarePage
	for _, subj := range subjectIDs {
		params := map[string]any{
			"courseIds":    []string{courseID},
			"courseId":     courseID,
			"eduSubjectID": subj,
			"siteId":       SITE_ID,
		}
		resp := d.request(course_detail_path, params)
		for _, w := range collectCwares(resp) {
			cwareID := firstString(w, "cwareId", "cwareID", "cwId")
			if cwareID == "" || seen[cwareID] {
				continue
			}
			seen[cwareID] = true
			identity := firstString(w, "identity", "eduSubjectId")
			if identity == "" {
				identity = subj
			}
			page := fmt.Sprintf(video_list_url, url.QueryEscape(cwareID), url.QueryEscape(courseID))
			out = append(out, cwarePage{PageURL: page, CwareID: cwareID, Identity: identity})
		}
	}
	return out
}

// --- doorman (ln/op gateway) ---

type doorman struct {
	c         *util.Client
	headers   map[string]string
	uid       string
	sid       string
	publicKey string
	timeDiff  *int64
}

func newDoorman(c *util.Client, jar http.CookieJar) *doorman {
	d := &doorman{c: c, headers: map[string]string{
		"Origin":     MEMBER_ORIGIN,
		"Referer":    MEMBER_HOME_URL,
		"Accept":     "application/json, text/plain, */*",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36",
	}}
	var parts []string
	for _, rawURL := range []string{MEMBER_HOME_URL, ELEARNING_HOME_URL, "https://www.jianshe99.com/", "https://gateway.jianshe99.com/"} {
		u, _ := url.Parse(rawURL)
		for _, ck := range jar.Cookies(u) {
			parts = append(parts, ck.Name+"="+ck.Value)
			switch strings.ToLower(ck.Name) {
			case "cdeluid", "uid":
				d.uid = ck.Value
			case "sid":
				d.sid = ck.Value
			}
		}
	}
	if uniq := uniqueStrings(parts); len(uniq) > 0 {
		joined := strings.Join(uniq, "; ")
		d.headers["cookie"] = joined
		d.headers["Cookie"] = joined
	}
	return d
}

func (d *doorman) request(resourcePath string, params map[string]any) map[string]any {
	if d.uid == "" || d.sid == "" {
		return map[string]any{}
	}
	publicKey := d.getPublicKey()
	serverTime := time.Now().UnixMilli() + d.getTimeDiffer()
	aesKey := ""
	if publicKey != "" {
		aesKey = encryptAESKey(publicKey)
	}
	encParams := encryptParams(params, serverTime)
	payload := map[string]any{
		"resourcePath": resourcePath,
		"domain":       DOORMAN_DOMAIN,
		"publicKey":    publicKey,
		"params":       encParams,
		"ve":           "0",
		"lt":           time.Now().UnixMilli(),
		"fs":           "201",
		"ap":           doorman_app_id,
		"af":           "1",
		"aesKey":       aesKey,
		"sid":          d.sid,
		"appVersion":   "",
		"appType":      "pc",
		"platform":     "0",
	}
	// jianshe99 sets doorman_site_id = None, so no siteID payload key.
	return d.postJSON(doormanURL(resourcePath, DOORMAN_DOMAIN), payload, MEMBER_HOME_URL)
}

func (d *doorman) discoverSubjectIDs(courseID string) []string {
	resp := d.request(course_subject_path, map[string]any{
		"courseIds": []string{courseID},
		"uid":       d.uid,
	})
	seen := map[string]bool{}
	var out []string
	for _, node := range walkMaps(resp) {
		for _, k := range []string{"eduSubjectId", "eduSubjectID", "finalEduSubjectId", "identity"} {
			if v := firstString(node, k); v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

func (d *doorman) getPublicKey() string {
	if d.publicKey != "" {
		return d.publicKey
	}
	payload := map[string]any{"appVersion": "", "appType": "", "platform": "pc", "time": time.Now().UnixMilli(), "resourcePath": "+/key/public", "domain": "cdel"}
	resp := d.postJSON(doormanURL("+/key/public", "cdel"), payload, MEMBER_HOME_URL)
	if s := strings.TrimSpace(fmt.Sprint(resp["result"])); s != "" && s != "<nil>" {
		d.publicKey = s
	}
	return d.publicKey
}

func (d *doorman) getTimeDiffer() int64 {
	if d.timeDiff != nil {
		return *d.timeDiff
	}
	local := time.Now().UnixMilli()
	payload := map[string]any{"appVersion": "", "appType": "", "platform": "pc", "time": local, "resourcePath": "+/server/time", "domain": "cdel"}
	resp := d.postJSON(doormanURL("+/server/time", "cdel"), payload, MEMBER_HOME_URL)
	server, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(resp["result"])), 10, 64)
	diff := int64(0)
	if server > 0 {
		diff = server - local
	}
	d.timeDiff = &diff
	return diff
}

func (d *doorman) postJSON(api string, payload map[string]any, referer string) map[string]any {
	buf, _ := json.Marshal(payload)
	h := map[string]string{}
	for k, v := range d.headers {
		h[k] = v
	}
	h["Origin"] = MEMBER_ORIGIN
	h["Content-Type"] = "application/json;charset=UTF-8"
	if referer != "" {
		h["Referer"] = referer
	}
	resp, err := d.c.Post(api, bytes.NewReader(buf), h)
	if err != nil {
		return map[string]any{}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func doormanURL(resourcePath, domain string) string {
	return strings.TrimRight(DOORMAN_BASE_URL, "/") + "/" + domain + "@" + resourcePath
}

func encryptParams(params map[string]any, serverTime int64) string {
	body := map[string]any{}
	for k, v := range params {
		body[k] = v
	}
	body["time"] = serverTime
	plain, _ := json.Marshal(body)
	block, err := aes.NewCipher([]byte(doorman_aes_key))
	if err != nil {
		return ""
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(doorman_aes_iv)).CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out)
}

func encryptAESKey(publicKeyHex string) string {
	der, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil {
		return ""
	}
	var pub *rsa.PublicKey
	if anyKey, err := x509.ParsePKIXPublicKey(der); err == nil {
		pub, _ = anyKey.(*rsa.PublicKey)
	}
	if pub == nil {
		if k, err := x509.ParsePKCS1PublicKey(der); err == nil {
			pub = k
		}
	}
	if pub == nil {
		return ""
	}
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(doorman_aes_key))
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
}

func collectCwares(v any) []map[string]any {
	wareKeys := []string{"buyCwareList", "freeCwareList", "homeCwareList", "homeCwareTopList", "courseWareList", "wareList"}
	seen := map[string]bool{}
	var out []map[string]any
	for _, node := range walkMaps(v) {
		for _, key := range wareKeys {
			for _, item := range extractItems(node[key]) {
				id := firstString(item, "cwareId", "cwareID", "cwId")
				if id == "" || seen[id] || !isRecordedCware(item) {
					continue
				}
				seen[id] = true
				out = append(out, item)
			}
		}
	}
	return out
}

func isRecordedCware(m map[string]any) bool {
	if firstString(m, "useful") == "2" {
		return false
	}
	dir := normalizeURL(firstString(m, "cwDirURL", "dirURL"))
	if strings.Contains(dir, "videoList") || strings.Contains(dir, "courseView") {
		return true
	}
	return firstString(m, "cwareId", "cwareID", "cwId") != ""
}

func firstField(s string, names ...string) string {
	for _, name := range names {
		re := regexp.MustCompile(fmt.Sprintf(fieldStringRe.String(), regexp.QuoteMeta(name)))
		if v := extractFirst(re, s); v != "" {
			return html.UnescapeString(v)
		}
	}
	return ""
}

func isLiveReplayURL(s string) bool { return strings.Contains(s, "/dispatch/th/live/callback/play") }

func stripFragment(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return s[:i]
	}
	return s
}

func normalizeURL(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, `\/`, `/`))
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	if strings.HasPrefix(s, "/") {
		return strings.TrimRight(ELEARNING_HOME_URL, "/") + s
	}
	return s
}

func extractHTMLTitle(body string) string {
	return cleanText(stripTags(extractFirst(htmlTitleRe, body)))
}

func extractFirst(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

func extractItems(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m, ok := it.(map[string]any); ok && len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	}
	if m, ok := v.(map[string]any); ok && len(m) > 0 {
		return []map[string]any{m}
	}
	return nil
}

func walkMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, v := range t {
				walk(v)
			}
		case []any:
			for _, v := range t {
				walk(v)
			}
		}
	}
	walk(v)
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func uniqueStrings(vals []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func cleanText(s string) string { return strings.Join(strings.Fields(html.UnescapeString(s)), " ") }

func stripTags(s string) string {
	return regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(s, " ")
}

func pickFormat(s string) string {
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, ".m3u8"):
		return "m3u8"
	case strings.Contains(lower, ".pdf"):
		return "pdf"
	case strings.Contains(lower, ".ppt"):
		return "ppt"
	case strings.Contains(lower, ".doc"):
		return "doc"
	case strings.Contains(lower, ".xls"):
		return "xls"
	}
	return "mp4"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func uidFromJar(jar http.CookieJar) string {
	if jar == nil {
		return ""
	}
	u, _ := url.Parse("https://www.jianshe99.com")
	for _, ck := range jar.Cookies(u) {
		if ck.Name == "uid" || ck.Name == "userId" || ck.Name == "stuId" {
			return ck.Value
		}
	}
	return ""
}
