// Package fenbi implements the ke.fenbi.com lecture / episode extractor.
package fenbi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	referer                          = "https://pc.fenbi.com/pwa"
	origin                           = "https://pc.fenbi.com"
	login_check_url                  = "https://login.fenbi.com/api/users/current"
	ke_check_url                     = "https://ke.fenbi.com/win/v3/users/current?nickname=true"
	course_list_url                  = "https://ke.fenbi.com/win/v3/courses"
	visible_lectures_url             = "https://ke.fenbi.com/win/%s/v3/my/lectures/visible?start=%s&len=%s"
	lecture_detail_url               = "https://ke.fenbi.com/win/%s/v3/lectures/%s"
	lecture_detail_api_url           = "https://ke.fenbi.com/api/%s/v3/lectures/%s"
	lecture_summary_url              = "https://ke.fenbi.com/win/%s/v3/my/lectures/%s/summary"
	lecture_set_contents_url         = "https://ke.fenbi.com/win/%s/v3/lecturesets/%s/contents?start=%s&len=%s"
	lecture_episode_nodes_url        = "https://ke.fenbi.com/win/%s/v3/lectures/%s/episode_nodes"
	my_lecture_episode_set_nodes_url = "https://ke.fenbi.com/win/%s/v3/my/lectures/%s/episode_sets/%s/episode_nodes"
	episode_detail_url               = "https://ke.fenbi.com/win/%s/v3/episodes/%s"
	episode_detail_api_url           = "https://ke.fenbi.com/api/%s/v3/episodes/%s"
	media_meta_url                   = "https://ke.fenbi.com/win/%s/v3/episodes/%s/mediafile/meta"
	material_url                     = "https://live.fenbi.com/win/%s/v3/livereplay/materials/%s/path"
	vertical_material_url            = "https://ke.fenbi.com/win/v3/vertical_feed/material/path"
	page_size                        = 200
)

var patterns = []string{`(?:pc|ke|live|www)\.fenbi\.com/|(?:^|\s)(?:fenbi|粉笔)(?:\s|$)`}

var pwaQueryParams = map[string]string{
	"apcid":      "0",
	"app":        "fenbi",
	"gav":        "2",
	"device_app": "fenbi",
	"version":    "",
	"ua":         "cef",
	"hav":        "100",
	"kav":        "100",
	"av":         "100",
	"deviceId":   "",
	"vendor":     "web",
	"platform":   "win",
}

func init() {
	extractor.Register(&Fenbi{}, extractor.SiteInfo{Name: "Fenbi", URL: "fenbi.com", NeedAuth: true})
}

type Fenbi struct{}

func (f *Fenbi) Patterns() []string { return patterns }

var (
	winLectureRe = regexp.MustCompile(`(?i)/(?:win|api)/([A-Za-z0-9_]+)/v3/lectures/(\d+)`)
	winEpisodeRe = regexp.MustCompile(`(?i)/(?:win|api)/([A-Za-z0-9_]+)/v3/episodes/(\d+)`)
	prefixRe     = regexp.MustCompile(`(?i)/(?:win|api)/([A-Za-z0-9_]+)/v3/`)
	lectureRe    = regexp.MustCompile(`(?i)(?:lecture_id|lectureId|lectures?|biz_id|content_id)[=/](\d+)|[?&](?:lecture_id|biz_id|content_id)=(\d+)`)
	episodeRe    = regexp.MustCompile(`(?i)(?:episode_id|episodeId|episodes?)[=/](\d+)|[?&]episode_id=(\d+)`)
)

type requestIDs struct {
	Prefix    string
	LectureID string
	EpisodeID string
}

type episodeNode struct {
	ID    string
	Title string
	Raw   map[string]any
}

func (f *Fenbi) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("fenbi requires login cookies (use --cookies or --cookies-from-browser)")
	}
	ids := parseIDs(rawURL)
	if ids.Prefix == "" {
		ids.Prefix = "gwy"
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := map[string]string{
		"Accept":           "application/json, text/plain, */*",
		"Origin":           origin,
		"Referer":          referer,
		"X-Requested-With": "XMLHttpRequest",
	}
	_ = checkLogin(c, headers)

	if ids.LectureID == "" && ids.EpisodeID == "" {
		entries, err := resolveVisibleLectures(c, headers)
		if err != nil {
			return nil, err
		}
		if len(entries) == 0 {
			return nil, fmt.Errorf("fenbi: no visible lectures found from course list")
		}
		return &extractor.MediaInfo{Site: "fenbi", Title: "fenbi_courses", Entries: entries}, nil
	}

	if ids.EpisodeID != "" && ids.LectureID == "" {
		entry, _, err := resolveEpisode(c, headers, ids.Prefix, ids.EpisodeID, "fenbi_"+ids.EpisodeID, nil)
		if err != nil {
			return nil, err
		}
		return entry, nil
	}

	entries, title, err := resolveLecture(c, headers, ids)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("fenbi: no playable episodes found for lecture %s", ids.LectureID)
	}
	return &extractor.MediaInfo{Site: "fenbi", Title: util.SanitizeFilename(firstNonEmpty(title, "fenbi_"+ids.LectureID)), Entries: entries}, nil
}

func resolveVisibleLectures(c *util.Client, headers map[string]string) ([]*extractor.MediaInfo, error) {
	lectures, err := fetchAllVisibleLectures(c, headers)
	if err != nil {
		return nil, err
	}
	entries := make([]*extractor.MediaInfo, 0, len(lectures))
	seen := map[string]bool{}
	for _, lecture := range lectures {
		prefix := firstNonEmpty(valueString(lecture, "prefix", "coursePrefix", "course"), "gwy")
		lectureID := firstNonEmpty(valueString(lecture, "lectureId", "lecture_id", "userLectureId", "user_lecture_id", "id", "bizId", "biz_id", "contentId", "content_id"))
		if lectureID == "" || seen[prefix+":"+lectureID] {
			continue
		}
		seen[prefix+":"+lectureID] = true
		title := firstNonEmpty(pickTitle(lecture), "fenbi_"+lectureID)
		childEntries, resolvedTitle, err := resolveLecture(c, headers, requestIDs{Prefix: prefix, LectureID: lectureID})
		if err == nil && len(childEntries) > 0 {
			entries = append(entries, &extractor.MediaInfo{
				Site:    "fenbi",
				Title:   util.SanitizeFilename(firstNonEmpty(resolvedTitle, title)),
				Entries: childEntries,
				Extra:   map[string]any{"prefix": prefix, "lecture_id": lectureID, "raw": lecture},
			})
			continue
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:  "fenbi",
			Title: util.SanitizeFilename(title),
			Extra: map[string]any{"prefix": prefix, "lecture_id": lectureID, "raw": lecture},
		})
	}
	return entries, nil
}

func fetchAllVisibleLectures(c *util.Client, headers map[string]string) ([]map[string]any, error) {
	prefixes, err := fetchCoursePrefixes(c, headers)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	seen := map[string]bool{}
	for _, prefix := range prefixes {
		rows := fetchVisibleLectures(c, headers, prefix)
		for _, row := range rows {
			id := firstNonEmpty(valueString(row, "lectureId", "lecture_id", "userLectureId", "user_lecture_id", "id", "bizId", "biz_id", "contentId", "content_id"))
			key := prefix + ":" + id
			if id != "" && seen[key] {
				continue
			}
			if id != "" {
				seen[key] = true
			}
			row["prefix"] = firstNonEmpty(valueString(row, "prefix"), prefix)
			out = append(out, row)
		}
	}
	return out, nil
}

func fetchCoursePrefixes(c *util.Client, headers map[string]string) ([]string, error) {
	payload, err := requestJSON(c, course_list_url, headers)
	if err != nil {
		return nil, err
	}
	root := unwrapData(payload)
	var prefixes []string
	for _, item := range listMaps(root, "list", "items", "data", "datas", "courses", "courseList") {
		prefixes = appendUnique(prefixes, firstNonEmpty(valueString(item, "prefix", "course", "coursePrefix", "name")))
	}
	if m, ok := root.(map[string]any); ok {
		for key, value := range m {
			switch value.(type) {
			case map[string]any, []any:
				prefixes = appendUnique(prefixes, key)
			}
		}
	}
	if len(prefixes) == 0 {
		prefixes = []string{"gwy"}
	}
	return prefixes, nil
}

func fetchVisibleLectures(c *util.Client, headers map[string]string, prefix string) []map[string]any {
	if prefix == "" {
		prefix = "gwy"
	}
	var out []map[string]any
	for start := 0; start <= 5000; start += page_size {
		api := fmt.Sprintf(visible_lectures_url, url.PathEscape(prefix), strconv.Itoa(start), strconv.Itoa(page_size))
		payload, err := requestJSON(c, api, headers)
		if err != nil {
			break
		}
		root := unwrapData(payload)
		items := listMaps(root, "lectures", "lectureList", "items", "list", "datas", "data")
		out = append(out, items...)
		total := toInt(firstAny(root, "total", "count", "totalCount"), 0)
		if len(items) < page_size || (total > 0 && len(out) >= total) {
			break
		}
	}
	return out
}

func checkLogin(c *util.Client, headers map[string]string) error {
	for _, api := range []string{login_check_url, ke_check_url} {
		body, err := c.GetString(api, headers)
		if err != nil {
			continue
		}
		var payload any
		if json.Unmarshal([]byte(body), &payload) == nil {
			return nil
		}
	}
	return nil
}

func resolveLecture(c *util.Client, headers map[string]string, ids requestIDs) ([]*extractor.MediaInfo, string, error) {
	payloads := []any{}
	for _, api := range []string{
		fmt.Sprintf(lecture_detail_url, url.PathEscape(ids.Prefix), url.PathEscape(ids.LectureID)),
		fmt.Sprintf(lecture_detail_api_url, url.PathEscape(ids.Prefix), url.PathEscape(ids.LectureID)),
		fmt.Sprintf(lecture_summary_url, url.PathEscape(ids.Prefix), url.PathEscape(ids.LectureID)),
		fmt.Sprintf(lecture_episode_nodes_url, url.PathEscape(ids.Prefix), url.PathEscape(ids.LectureID)),
	} {
		if p, err := requestJSON(c, api, headers); err == nil {
			payloads = append(payloads, p)
		}
	}
	var nodes []episodeNode
	var title string
	var direct []*extractor.MediaInfo
	for _, payload := range payloads {
		title = firstNonEmpty(pickTitle(payload), title)
		if media := findMediaURL(payload); media != "" {
			direct = append(direct, mediaInfo(firstNonEmpty(pickTitle(payload), ids.LectureID), media, headers))
		}
		nodes = append(nodes, collectEpisodes(payload)...)
	}
	if ids.EpisodeID != "" {
		nodes = append([]episodeNode{{ID: ids.EpisodeID, Title: title}}, nodes...)
	}
	if len(nodes) == 0 && len(direct) > 0 {
		return direct, title, nil
	}
	seen := map[string]bool{}
	entries := make([]*extractor.MediaInfo, 0, len(nodes))
	for _, node := range nodes {
		if node.ID == "" || seen[node.ID] {
			continue
		}
		seen[node.ID] = true
		entry, files, err := resolveEpisode(c, headers, ids.Prefix, node.ID, node.Title, node.Raw, map[string]any{"lecture_id": ids.LectureID, "content_id": ids.LectureID})
		if err == nil {
			entries = append(entries, entry)
			entries = append(entries, files...)
		}
	}
	if len(entries) == 0 && len(direct) > 0 {
		return direct, title, nil
	}
	return entries, title, nil
}

func resolveEpisode(c *util.Client, headers map[string]string, prefix, episodeID, fallbackTitle string, hints ...map[string]any) (*extractor.MediaInfo, []*extractor.MediaInfo, error) {
	payloads := []any{}
	videoInfo := map[string]any{"prefix": prefix, "episode_id": episodeID}
	for _, hint := range hints {
		mergeVideoInfo(videoInfo, hint)
	}
	for _, api := range []string{
		fmt.Sprintf(episode_detail_url, url.PathEscape(prefix), url.PathEscape(episodeID)),
		fmt.Sprintf(episode_detail_api_url, url.PathEscape(prefix), url.PathEscape(episodeID)),
	} {
		if p, err := requestJSON(c, api, headers); err == nil {
			payloads = append(payloads, p)
			mergeVideoInfo(videoInfo, p)
		}
	}
	for _, params := range mediaMetaParamSets(videoInfo) {
		api := fmt.Sprintf(media_meta_url, url.PathEscape(prefix), url.PathEscape(episodeID))
		if p, err := requestJSON(c, api, headers, params); err == nil {
			payloads = append(payloads, p)
			mergeVideoInfo(videoInfo, p)
		}
	}
	for _, payload := range payloads {
		if media := findMediaURL(payload); media != "" {
			title := util.SanitizeFilename(firstNonEmpty(pickTitle(payload), fallbackTitle, episodeID))
			entry := mediaInfo(title, media, headers)
			files := materialEntries(c, headers, videoInfo, payloads)
			if len(files) > 0 {
				entry.Extra = map[string]any{"materials": materialSummaries(files)}
			}
			return entry, files, nil
		}
	}
	return nil, nil, fmt.Errorf("fenbi: no media meta URL for episode %s", episodeID)
}

func mediaMetaParamSets(videoInfo map[string]any) []map[string]string {
	contentID := firstNonEmpty(infoString(videoInfo, "content_id", "contentId", "lecture_id", "lectureId"), infoString(videoInfo, "episode_id", "episodeId"))
	bizType := infoString(videoInfo, "biz_type", "bizType")
	bizID := firstNonEmpty(infoString(videoInfo, "biz_id", "bizId"), contentID)
	type combo struct{ bizType, bizID, contentID string }
	candidates := []combo{
		{bizType: "-10", bizID: "0", contentID: contentID},
		{bizType: "1", bizID: contentID, contentID: contentID},
		{bizType: "2", bizID: contentID, contentID: contentID},
		{bizType: bizType, bizID: bizID, contentID: contentID},
		{bizType: "0", bizID: contentID, contentID: contentID},
	}
	var out []map[string]string
	seen := map[string]bool{}
	for _, c := range candidates {
		c.bizType = firstNonEmpty(c.bizType, "0")
		c.bizID = firstNonEmpty(c.bizID, "0")
		c.contentID = firstNonEmpty(c.contentID, "0")
		key := c.bizType + ":" + c.bizID + ":" + c.contentID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, map[string]string{"biz_type": c.bizType, "biz_id": c.bizID, "content_id": c.contentID})
	}
	out = append(out, nil)
	return out
}

func materialEntries(c *util.Client, headers map[string]string, videoInfo map[string]any, payloads []any) []*extractor.MediaInfo {
	materials := collectMaterialCandidates(payloads...)
	var out []*extractor.MediaInfo
	seen := map[string]bool{}
	for i, material := range materials {
		fileURL := pickURLFromResponse(material)
		if fileURL == "" {
			fileURL = resolveMaterialURL(c, headers, videoInfo, material)
		}
		if fileURL == "" || seen[fileURL] {
			continue
		}
		seen[fileURL] = true
		name := firstNonEmpty(materialName(material), fmt.Sprintf("课件%d", i+1))
		format := fileExt(firstNonEmpty(valueString(material, "ext", "fileType", "typeName"), fileURL))
		out = append(out, &extractor.MediaInfo{
			Site:  "fenbi",
			Title: util.SanitizeFilename(name),
			Streams: map[string]extractor.Stream{"file": {
				Quality: "file",
				URLs:    []string{fileURL},
				Format:  format,
				Headers: headers,
			}},
			Extra: map[string]any{"kind": "material", "material": material},
		})
	}
	return out
}

func resolveMaterialURL(c *util.Client, headers map[string]string, videoInfo map[string]any, material map[string]any) string {
	materialID := firstNonEmpty(valueString(material, "materialId", "id", "material_id", "fileId", "file_id", "noteMaterialId", "note_material_id"))
	if materialID == "" {
		return ""
	}
	prefix := firstNonEmpty(infoString(videoInfo, "prefix"), "gwy")
	episodeID := infoString(videoInfo, "episode_id", "episodeId")
	contentID := firstNonEmpty(infoString(videoInfo, "content_id", "contentId", "lecture_id", "lectureId"), episodeID)
	bizType := infoString(videoInfo, "biz_type", "bizType")
	bizID := firstNonEmpty(infoString(videoInfo, "biz_id", "bizId"), contentID)

	materialIDs := appendUnique(nil, materialID)
	if len(materialID) > 4 && materialID[len(materialID)-4:] == ".pdf" {
		materialIDs = appendUnique(materialIDs, materialID[:len(materialID)-4])
	}
	type combo struct{ bizType, bizID string }
	combos := []combo{{bizType, bizID}, {"0", contentID}, {"-10", "0"}, {"1", contentID}, {"2", contentID}}
	seenCombos := map[string]bool{}
	for _, id := range materialIDs {
		for _, candidate := range combos {
			bt := firstNonEmpty(candidate.bizType, "0")
			bi := firstNonEmpty(candidate.bizID, "0")
			key := id + ":" + bt + ":" + bi
			if seenCombos[key] {
				continue
			}
			seenCombos[key] = true
			params := withPWAParams(map[string]string{"biz_id": bi, "biz_type": bt, "episode_id": episodeID})
			api := fmt.Sprintf(material_url, url.PathEscape(prefix), url.PathEscape(id))
			if payload, err := requestJSON(c, api, headers, params); err == nil {
				if u := pickURLFromResponse(payload); u != "" {
					return u
				}
			}
		}
	}
	for _, id := range materialIDs {
		params := withPWAParams(map[string]string{"material_id": id, "episode_id": episodeID, "biz_type": firstNonEmpty(bizType, "0"), "biz_id": firstNonEmpty(bizID, contentID, "0")})
		if payload, err := requestJSON(c, vertical_material_url, headers, params); err == nil {
			if u := pickURLFromResponse(payload); u != "" {
				return u
			}
		}
	}
	return ""
}

func materialSummaries(files []*extractor.MediaInfo) []map[string]any {
	out := make([]map[string]any, 0, len(files))
	for _, file := range files {
		for _, stream := range file.Streams {
			if len(stream.URLs) > 0 {
				out = append(out, map[string]any{"title": file.Title, "url": stream.URLs[0], "format": stream.Format})
				break
			}
		}
	}
	return out
}

func withPWAParams(params map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range pwaQueryParams {
		out[k] = v
	}
	for k, v := range params {
		out[k] = v
	}
	return out
}

func requestJSON(c *util.Client, api string, headers map[string]string, params ...map[string]string) (any, error) {
	if len(params) > 0 && params[0] != nil {
		api = addQuery(api, params[0])
	}
	body, err := c.GetString(api, headers)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func addQuery(api string, params map[string]string) string {
	u, err := url.Parse(api)
	if err != nil {
		return api
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func parseIDs(raw string) requestIDs {
	out := requestIDs{Prefix: "gwy"}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		out.Prefix = firstNonEmpty(q.Get("prefix"), out.Prefix)
		out.LectureID = firstNonEmpty(q.Get("lecture_id"), q.Get("lectureId"), q.Get("biz_id"), q.Get("content_id"))
		out.EpisodeID = firstNonEmpty(q.Get("episode_id"), q.Get("episodeId"))
	}
	if m := winLectureRe.FindStringSubmatch(raw); m != nil {
		out.Prefix, out.LectureID = m[1], m[2]
	}
	if m := winEpisodeRe.FindStringSubmatch(raw); m != nil {
		out.Prefix, out.EpisodeID = m[1], m[2]
	}
	if m := prefixRe.FindStringSubmatch(raw); m != nil && out.Prefix == "gwy" {
		out.Prefix = m[1]
	}
	out.LectureID = firstNonEmpty(out.LectureID, rx(lectureRe, raw))
	out.EpisodeID = firstNonEmpty(out.EpisodeID, rx(episodeRe, raw))
	return out
}

func rx(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}
