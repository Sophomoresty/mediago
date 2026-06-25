// Package med66 implements source-aligned Med66 course extraction.
package med66

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	LOGIN_URL            = "https://www.med66.com/OtherItem/loginAgain/index.shtml"
	MEMBER_HOME_URL      = "https://member.med66.com/homes/mycourse"
	COURSE_INFO_URL      = "https://member.med66.com/homes/mycourse/courseInfo"
	COURSEWARE_INFO_URL   = "https://member.med66.com/homes/course/courseClassWareInfo"
	ELEARNING_HOME_URL   = "https://elearning.med66.com/"
	LIVE_REPLAY_INFO_URL = "https://live.cdeledu.com/liveapi/entry/getReplayInfo"
	LIVE_REFERER_URL     = "https://live.cdeledu.com/"
	MATERIALS_URL        = "https://elearning.med66.com/xcware/download/teachingMaterials.shtm?cwareID={cware_id}&iskcjy=1&identity={identity}"
	MATERIAL_DOWNLOAD_URL = "https://elearning.med66.com/data2file/downloadFile/getWordVipFile?cwareID=&fileUrl={file_url}&fileReName={file_name}"
)

var patterns = []string{`(?:[\w-]+\.)?med66\.com/`, `live\.cdeledu\.com/`}

func init() {
	extractor.Register(&Med66{}, extractor.SiteInfo{Name: "Med66", URL: "med66.com", NeedAuth: true})
}

type Med66 struct{}

func (m *Med66) Patterns() []string { return patterns }

var (
	courseIDRe      = regexp.MustCompile(`(?i)(?:courseId|course_id)=((?:med)?\d+)`)
	goToLiveRe     = regexp.MustCompile(`goToLive\(\s*['"]([^'"]+)['"](?:\s*,\s*['"]([^'"]*)['"])?`)
	h5VarsRe       = regexp.MustCompile(`window\.cdelmedia\.h5Vars\s*=\s*JSON\.parse\('(?s:(.*?))'\)`)
	openURLRe      = regexp.MustCompile(`window\.open\(["']([^"']+)["']`)
	videoIDRe      = regexp.MustCompile(`(?:videoID|videoId|video_id)=([0-9A-Za-z_\-]+)`)
	attrRe         = regexp.MustCompile(`(?i)(data-[a-z0-9_-]+)\s*=\s*["']([^"']+)["']`)
	htmlTagRe      = regexp.MustCompile(`<[^>]+>`)
	directFileExts = map[string]bool{".pdf": true, ".doc": true, ".docx": true, ".ppt": true, ".pptx": true, ".zip": true, ".rar": true, ".7z": true, ".caj": true}
)

func (m *Med66) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("med66 requires login cookies")
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := med66Headers()
	uid := cookieValue(opts.Cookies, []string{"https://member.med66.com/", "https://www.med66.com/"}, "cdeluid")

	if isReplayURL(rawURL) {
		entry, err := resolveReplayEntry(c, rawURL, rawURL, uid, "med66_replay")
		if err != nil {
			return nil, err
		}
		return entry, nil
	}

	cid := extractCourseID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("cannot parse med66 courseId from URL: %s", rawURL)
	}

	course, err := fetchCourse(c, headers, cid)
	if err != nil {
		return nil, err
	}
	wares, err := fetchCoursewares(c, headers, course)
	if err != nil {
		return nil, err
	}

	var entries []*extractor.MediaInfo
	for i, ware := range wares {
		wareIndex := i + 1

		// 1. Direct material: showType=5 or file extension in URL
		if isDirectMaterial(ware) {
			if entry := buildDirectMaterialEntry(ware, wareIndex); entry != nil {
				entries = append(entries, entry)
			}
			continue
		}

		if !isRecordedWare(ware) {
			continue
		}

		pageURL := normalizeURL(firstString(ware, "cwDirURL", "dirURL", "cwURL"), ELEARNING_HOME_URL)
		if pageURL == "" {
			continue
		}

		body, err := c.GetString(pageURL, map[string]string{"Referer": MEMBER_HOME_URL})
		if err != nil {
			continue
		}

		if strings.Contains(body, "课程暂未开通") || strings.Contains(body, "暂未开通") {
			continue
		}

		cwareID := firstString(ware, "cwareId", "cwareID", "cware_id")
		identity := firstString(ware, "identity")
		if identity == "" {
			identity = course.EduSubjectID
		}

		// 2. Live replay videos (goToLive links)
		if strings.Contains(body, "goToLive(") {
			matches := goToLiveRe.FindAllStringSubmatch(body, -1)
			for j, match := range matches {
				playURL := normalizeURL(match[1], ELEARNING_HOME_URL)
				if playURL == "" {
					continue
				}
				title := fmt.Sprintf("%02d.%02d %s", wareIndex, j+1, titleFromWare(ware))
				entry, err := resolveReplayEntry(c, playURL, pageURL, uid, title)
				if err == nil && entry != nil {
					entry.Extra["course_id"] = course.CourseID
					entry.Extra["cware_id"] = cwareID
					entries = append(entries, entry)
				}
			}
		} else {
			// 3. Regular recorded videos (continueStudyVideo / h5Vars)
			regularVideos := parseRegularVideoTree(body, ware, wareIndex)
			for j, rv := range regularVideos {
				entry, err := resolveRegularVideo(c, rv, wareIndex, j+1)
				if err == nil && entry != nil {
					entry.Extra["course_id"] = course.CourseID
					entry.Extra["cware_id"] = cwareID
					entries = append(entries, entry)
				}
			}
		}

		// 4. Material files from MATERIALS_URL
		if cwareID != "" {
			materialEntries := parseMaterialTree(c, cwareID, identity, ware, wareIndex)
			entries = append(entries, materialEntries...)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("med66: no playable entries found (course locked or schema changed)")
	}

	return &extractor.MediaInfo{Site: "med66", Title: course.Title, Entries: entries}, nil
}

type med66Course struct {
	CourseID        string
	Title           string
	EduSubjectID    string
	ClassType       string
	ClassID         string
	LinkedCourseIDs string
}

type anyMap map[string]any

func fetchCourse(c *util.Client, headers map[string]string, cid string) (med66Course, error) {
	body, err := c.PostForm(COURSE_INFO_URL, map[string]string{}, headers)
	if err != nil {
		return med66Course{}, fmt.Errorf("courseInfo: %w", err)
	}
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return med66Course{}, fmt.Errorf("parse courseInfo: %w", err)
	}
	for _, obj := range collectMaps(root) {
		courseID := strAny(obj["courseId"])
		if courseID == "" {
			courseID = strAny(obj["course_id"])
		}
		if courseID != cid {
			continue
		}
		return med66Course{
			CourseID:        courseID,
			Title:           firstString(obj, "title", "homeTitle", "selCourseTitle", "courseEduName", "listName"),
			EduSubjectID:    firstString(obj, "eduSubjectId", "eduSubjectID"),
			ClassType:       firstString(obj, "classType"),
			ClassID:         firstString(obj, "classId", "viewClassId"),
			LinkedCourseIDs: firstString(obj, "linkedCourseIds"),
		}, nil
	}
	return med66Course{}, fmt.Errorf("med66 courseInfo: courseId %s not found", cid)
}

func fetchCoursewares(c *util.Client, headers map[string]string, course med66Course) ([]anyMap, error) {
	form := map[string]string{
		"eduSubjectId":    course.EduSubjectID,
		"classType":       course.ClassType,
		"classId":         course.ClassID,
		"linkedCourseIds": course.LinkedCourseIDs,
		"courseId":        course.CourseID,
	}
	body, err := c.PostForm(COURSEWARE_INFO_URL, form, headers)
	if err != nil {
		return nil, fmt.Errorf("courseClassWareInfo: %w", err)
	}
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("parse courseClassWareInfo: %w", err)
	}
	var out []anyMap
	for _, obj := range collectMaps(root) {
		for _, key := range []string{"homeCwareList", "homeWareList", "courseWareList", "wareList"} {
			if list, ok := obj[key].([]any); ok {
				for _, item := range list {
					if m, ok := item.(map[string]any); ok {
						out = append(out, anyMap(m))
					}
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("med66: empty wareList for courseId %s", course.CourseID)
	}
	return out, nil
}

// isDirectMaterial returns true when the courseware entry has showType=5 or a
// direct file URL with a downloadable extension (pdf, docx, etc.).
// Source: Med66_Course._is_direct_material (line 461)
func isDirectMaterial(ware anyMap) bool {
	rawURL := normalizeURL(firstString(ware, "cwDirURL", "cwURL"), ELEARNING_HOME_URL)
	if rawURL == "" {
		return false
	}
	if toInt(firstString(ware, "showType")) == 5 {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	return directFileExts[ext]
}

// buildDirectMaterialEntry creates a MediaInfo entry for a direct-download
// material file (no API resolution needed).
// Source: Med66_Course._build_direct_material_tree (line 479)
func buildDirectMaterialEntry(ware anyMap, wareIndex int) *extractor.MediaInfo {
	rawURL := normalizeURL(firstString(ware, "cwDirURL", "cwURL"), ELEARNING_HOME_URL)
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(u.Path), "."))
	if ext == "" {
		ext = "pdf"
	}
	name := firstString(ware, "cwName", "title")
	if name == "" {
		name = "课程资料"
	}
	title := fmt.Sprintf("(%d.1.1.1)--%s", wareIndex, util.SanitizeFilename(name))
	return &extractor.MediaInfo{
		Site:  "med66",
		Title: title,
		Streams: map[string]extractor.Stream{
			"default": {
				Quality: "source",
				URLs:    []string{rawURL},
				Format:  ext,
				Headers: map[string]string{"Referer": ELEARNING_HOME_URL},
			},
		},
		Extra: map[string]any{"type": "file", "file_fmt": ext},
	}
}

// med66RegularVideo represents a non-live-replay recorded video entry.
type med66RegularVideo struct {
	Title    string
	PlayURL  string
	VideoID  string
	CwareID  string
	Identity string
}

// parseRegularVideoTree extracts non-live-replay video entries from the
// courseware page HTML (continueStudyVideo / window.open links).
// Source: Zhengbao_Course._parse_video_tree (line 440)
func parseRegularVideoTree(body string, ware anyMap, wareIndex int) []med66RegularVideo {
	var out []med66RegularVideo
	seen := map[string]bool{}
	for _, block := range strings.Split(body, "continueStudyVideo") {
		if !strings.Contains(block, "window.open") {
			continue
		}
		m := openURLRe.FindStringSubmatch(block)
		if len(m) < 2 {
			continue
		}
		playURL := normalizeURL(m[1], ELEARNING_HOME_URL)
		if playURL == "" {
			continue
		}
		vid := ""
		if vm := videoIDRe.FindStringSubmatch(playURL); len(vm) > 1 {
			vid = vm[1]
		}
		if vid == "" {
			vid = fmt.Sprintf("video-%d", len(out)+1)
		}
		if seen[vid] {
			continue
		}
		seen[vid] = true
		title := extractNearbyText(block)
		if title == "" {
			title = titleFromWare(ware)
		}
		out = append(out, med66RegularVideo{
			Title:    fmt.Sprintf("[%d.%d]--%s", wareIndex, len(out)+1, title),
			PlayURL:  playURL,
			VideoID:  vid,
			CwareID:  firstString(ware, "cwareId", "cwareID", "cware_id"),
			Identity: firstString(ware, "identity"),
		})
	}
	return out
}

// resolveRegularVideo resolves a non-live-replay video by fetching the play
// page and extracting window.cdelmedia.h5Vars JSON for the videoPath.
// Source: Zhengbao_Course._resolve_video_play_info (line 672)
func resolveRegularVideo(c *util.Client, rv med66RegularVideo, wareIndex, videoIndex int) (*extractor.MediaInfo, error) {
	if rv.PlayURL == "" {
		return nil, fmt.Errorf("med66: empty play URL")
	}
	playURL := normalizeURL(rv.PlayURL, ELEARNING_HOME_URL)
	format := pickFormat(playURL)
	extra := map[string]any{
		"video_id":  rv.VideoID,
		"play_page": playURL,
		"cware_id":  rv.CwareID,
		"identity":  rv.Identity,
	}

	// If the URL is not already a direct media file, fetch the play page
	// and extract h5Vars to find the videoPath.
	if !strings.Contains(strings.ToLower(playURL), ".m3u8") && !strings.Contains(strings.ToLower(playURL), ".mp4") {
		body, err := c.GetString(playURL, map[string]string{"Referer": ELEARNING_HOME_URL})
		if err != nil {
			return nil, err
		}
		vars := parseH5Vars(body)
		if len(vars) == 0 {
			return nil, fmt.Errorf("med66: no h5Vars in play page for %s", rv.VideoID)
		}
		if p := h5VarFirstString(vars, "videoPath", "video_path", "path", "url"); p != "" {
			playURL = normalizeURL(strings.ReplaceAll(p, `\/`, `/`), ELEARNING_HOME_URL)
			format = pickFormat(playURL)
		}
		if sub := h5VarFirstString(vars, "srtPath", "subPath", "subtitle", "subtitleUrl"); sub != "" {
			extra["subtitle"] = normalizeURL(sub, ELEARNING_HOME_URL)
		}
	}
	if playURL == "" {
		return nil, fmt.Errorf("med66: no videoPath resolved for %s", rv.VideoID)
	}
	name := util.SanitizeFilename(firstNonEmpty(rv.Title, fmt.Sprintf("[%02d.%02d]--%s", wareIndex, videoIndex, rv.VideoID)))
	return &extractor.MediaInfo{
		Site:  "med66",
		Title: name,
		Streams: map[string]extractor.Stream{
			"default": {
				Quality:   "source",
				URLs:      []string{playURL},
				Format:    format,
				NeedMerge: format == "m3u8",
				Headers:   map[string]string{"Referer": ELEARNING_HOME_URL},
			},
		},
		Extra: extra,
	}, nil
}

// parseH5Vars extracts the window.cdelmedia.h5Vars JSON from a play page.
// Source: Zhengbao_Course._resolve_video_play_info (line 685)
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

// h5VarFirstString extracts the first non-empty string from h5Vars map.
func h5VarFirstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

// parseMaterialTree fetches the material listing page and extracts
// downloadable file entries with their download URLs.
// Source: Zhengbao_Course._parse_material_tree (line 531) with
// Med66 URLs from Med66_Config (MATERIALS_URL, MATERIAL_DOWNLOAD_URL).
func parseMaterialTree(c *util.Client, cwareID, identity string, ware anyMap, wareIndex int) []*extractor.MediaInfo {
	api := strings.ReplaceAll(MATERIALS_URL, "{cware_id}", url.QueryEscape(cwareID))
	api = strings.ReplaceAll(api, "{identity}", url.QueryEscape(identity))

	body, err := c.GetString(api, map[string]string{"Referer": ELEARNING_HOME_URL})
	if err != nil || body == "" {
		return nil
	}

	type fileSpec struct {
		Key    string
		Format string
		Suffix string
	}
	specs := []fileSpec{
		{"data-fileurl", "docx", ""},
		{"data-pdfurl", "pdf", ""},
		{"data-sepurl", "docx", "-答案分离"},
		{"data-seppdfurl", "pdf", "-答案分离"},
	}

	var entries []*extractor.MediaInfo
	seen := map[string]bool{}
	for _, attrs := range extractAttrs(body) {
		name := firstNonEmpty(attrs["data-videoname"], attrs["title"])
		if name == "" {
			name = titleFromWare(ware)
		}
		if name == "" {
			name = "课程资料"
		}

		for _, spec := range specs {
			tok := strings.TrimSpace(attrs[spec.Key])
			if tok == "" {
				continue
			}
			dedup := spec.Key + "|" + tok
			if seen[dedup] {
				continue
			}
			seen[dedup] = true

			fileName := util.SanitizeFilename(name + spec.Suffix)
			dlURL := buildMaterialDownloadURL(tok, fileName, spec.Format)
			if dlURL == "" {
				continue
			}

			title := fmt.Sprintf("(%d.%d)--%s", wareIndex, len(entries)+1, fileName)
			entries = append(entries, &extractor.MediaInfo{
				Site:  "med66",
				Title: title,
				Streams: map[string]extractor.Stream{
					"default": {
						Quality: "source",
						URLs:    []string{dlURL},
						Format:  spec.Format,
						Headers: map[string]string{"Referer": ELEARNING_HOME_URL},
					},
				},
				Extra: map[string]any{"type": "file", "file_fmt": spec.Format, "file_token": tok},
			})
		}
	}
	return entries
}

// buildMaterialDownloadURL constructs the full download URL from a file token.
// Source: Zhengbao_Course._build_material_url (line 749) with Med66 URL template.
func buildMaterialDownloadURL(fileToken, fileName, fmtHint string) string {
	if fileToken == "" {
		return ""
	}
	fullName := fileName
	if fmtHint != "" && !strings.HasSuffix(strings.ToLower(fullName), "."+strings.ToLower(fmtHint)) {
		fullName += "." + fmtHint
	}
	u := strings.ReplaceAll(MATERIAL_DOWNLOAD_URL, "{file_url}", url.QueryEscape(fileToken))
	u = strings.ReplaceAll(u, "{file_name}", url.QueryEscape(fullName))
	return u
}

// extractAttrs parses HTML tags and extracts data-* attributes as maps.
func extractAttrs(body string) []map[string]string {
	var out []map[string]string
	for _, tag := range regexp.MustCompile(`<[^>]+>`).FindAllString(body, -1) {
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

// extractNearbyText extracts visible text from an HTML fragment.
func extractNearbyText(html string) string {
	text := htmlTagRe.ReplaceAllString(html, " ")
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 80 {
		text = string(runes[:80])
	}
	return strings.TrimSpace(text)
}

// toInt converts a string value to int, returning 0 on failure.
func toInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func resolveReplayEntry(c *util.Client, playURL, referer, uid, title string) (*extractor.MediaInfo, error) {
	payload, err := resolveReplayPayload(c, playURL, referer)
	if err != nil {
		return nil, err
	}
	replay := payload.Replay
	viewer := strings.TrimSpace(uid)
	if viewer == "" {
		viewer = payload.Query.Get("userid")
	}
	candidates := uniqueNonEmpty(replay.AccessKey, payload.Token)
	if len(candidates) == 0 {
		candidates = []string{""}
	}

	var playInfo *shared.CssLcloudPlayInfo
	var lastErr error
	for _, token := range candidates {
		playInfo, lastErr = shared.CssLcloudResolvePlayInfo(c, shared.CssLcloudPayload{
			LiveRoomID:  firstNonEmpty(replay.LiveRoomID, replay.LiveID),
			UserID:      replay.AccessID,
			AccessID:    replay.AccessID,
			RecordID:    replay.RecordID,
			ViewerName:  viewer,
			ViewerToken: token,
			Referer:     LIVE_REFERER_URL,
		})
		if lastErr == nil {
			break
		}
	}
	if playInfo == nil || lastErr != nil {
		return nil, fmt.Errorf("med66 csslcloud replay: %w", lastErr)
	}

	streams := map[string]extractor.Stream{}
	for idx, s := range playInfo.VideoList {
		if s.URL == "" {
			continue
		}
		key := fmt.Sprintf("definition_%d", s.Definition)
		if s.Definition == 0 {
			key = fmt.Sprintf("stream_%d", idx+1)
		}
		streams[key] = extractor.Stream{Quality: key, URLs: []string{s.URL}, Format: pickFormat(s.URL), AudioURL: playInfo.AudioURL, Headers: map[string]string{"Referer": LIVE_REFERER_URL}}
	}
	if len(streams) == 0 && playInfo.VideoURL != "" {
		streams["best"] = extractor.Stream{Quality: "best", URLs: []string{playInfo.VideoURL}, Format: pickFormat(playInfo.VideoURL), AudioURL: playInfo.AudioURL, Headers: map[string]string{"Referer": LIVE_REFERER_URL}}
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("med66 csslcloud: no media URL")
	}

	extra := map[string]any{"recordId": replay.RecordID, "accessid": replay.AccessID, "liveRoomId": firstNonEmpty(replay.LiveRoomID, replay.LiveID)}
	if strings.Contains(playInfo.VideoURL, ".m3u8") {
		if text, err := c.GetString(playInfo.VideoURL, map[string]string{"Referer": LIVE_REFERER_URL}); err == nil {
			if rewritten, err := shared.CssLcloudRewriteM3U8Keys(c, text, LIVE_REFERER_URL); err == nil {
				extra["m3u8_text"] = rewritten
			}
		}
	}
	return &extractor.MediaInfo{Site: "med66", Title: title, Streams: streams, Extra: extra}, nil
}

type liveReplayPayload struct {
	Replay liveReplayReplay
	Token  string
	Query  url.Values
}

type liveReplayReplay struct {
	LiveRoomID string `json:"liveRoomId"`
	LiveID     string `json:"liveId"`
	AccessID   string `json:"accessid"`
	RecordID   string `json:"recordId"`
	AccessKey  string `json:"accesskey"`
}

func resolveReplayPayload(c *util.Client, playURL, referer string) (liveReplayPayload, error) {
	finalURL := playURL
	if !strings.Contains(playURL, "liveapi/entry/getReplayInfo") {
		resp, err := c.Get(playURL, map[string]string{"Referer": referer})
		if err != nil {
			return liveReplayPayload{}, fmt.Errorf("resolve replay redirect: %w", err)
		}
		resp.Body.Close()
		finalURL = resp.Request.URL.String()
	}
	query, err := stripReplayQuery(finalURL)
	if err != nil {
		return liveReplayPayload{}, err
	}
	body, err := getJSONWithParams(c, LIVE_REPLAY_INFO_URL, query, map[string]string{"Referer": LIVE_REFERER_URL})
	if err != nil {
		return liveReplayPayload{}, fmt.Errorf("getReplayInfo: %w", err)
	}
	var root struct {
		Replay liveReplayReplay `json:"replay"`
		Token  string           `json:"token"`
		Data   struct {
			Replay liveReplayReplay `json:"replay"`
			Token  string           `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return liveReplayPayload{}, fmt.Errorf("parse getReplayInfo: %w", err)
	}
	replay := root.Replay
	if replay.RecordID == "" {
		replay = root.Data.Replay
	}
	token := firstNonEmpty(root.Token, root.Data.Token)
	if firstNonEmpty(replay.LiveRoomID, replay.LiveID) == "" || replay.AccessID == "" || replay.RecordID == "" {
		return liveReplayPayload{}, fmt.Errorf("med66 getReplayInfo: missing replay liveRoomId/accessid/recordId")
	}
	return liveReplayPayload{Replay: replay, Token: token, Query: query}, nil
}

func getJSONWithParams(c *util.Client, endpoint string, params url.Values, headers map[string]string) (string, error) {
	u, _ := url.Parse(endpoint)
	u.RawQuery = params.Encode()
	return c.GetString(u.String(), headers)
}

func stripReplayQuery(s string) (url.Values, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Del("oldTime")
	q.Del("oldKey")
	return q, nil
}

func med66Headers() map[string]string {
	return map[string]string{"Origin": "https://member.med66.com", "Referer": MEMBER_HOME_URL, "Accept": "application/json, text/plain, */*"}
}

func isReplayURL(s string) bool {
	return strings.Contains(s, "live.cdeledu.com") || strings.Contains(s, "recordId=") || strings.Contains(s, "liveRoomId=")
}

func extractCourseID(s string) string {
	if m := courseIDRe.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

func isRecordedWare(m anyMap) bool {
	return firstString(m, "cwDirURL", "dirURL", "cwURL", "cwareId", "cwareID") != ""
}

func titleFromWare(m anyMap) string {
	if t := firstString(m, "cwName", "cwShowName", "title"); t != "" {
		return util.SanitizeFilename(t)
	}
	return "课件"
}
