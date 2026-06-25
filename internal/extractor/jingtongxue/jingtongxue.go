// Package jingtongxue implements an extractor for jingtongxue.com courses.
//
// API endpoints from decompiled Mooc/Courses/Jingtongxue/:
//
//	https://www.jingtongxue.com/s/api/course/v1/user/get/courses
//	https://www.jingtongxue.com/s/api/saas-business/front/commodity/getDetail/{commodity_id}
//	https://www.jingtongxue.com/s/api/saas-business/front/commodity/listVideoChapter/{commodity_id}/{class_type_id}
//	https://www.jingtongxue.com/s/api/saas-business/front/commodity/listVideoLecture/{class_type_id}/{chapter_id}
//	https://www.jingtongxue.com/s/api/saas-business/front/commodity/video/{module_id}/{class_type_id}/{lecture_id}
//	https://www.jingtongxue.com/s/api/saas-business/front/commodity/video/getVideoPlayParam
//	https://www.jingtongxue.com/s/api/saas-business/front/commodity/findClassResourceMenu
//	https://www.jingtongxue.com/s/api/saas-business/front/commodity/findResource/{commodity_id}/{class_type_id}
//	https://www.jingtongxue.com/s/api/saas-business/front/commodity/getDownloadLink/{resource_id}
//	https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid={siteid}
package jingtongxue

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlReferer        = "https://www.jingtongxue.com/s/pc/"
	urlOrigin         = "https://www.jingtongxue.com"
	urlDomain         = "https://www.jingtongxue.com"
	urlAPIBase        = "https://www.jingtongxue.com/s/api"
	pathCourseList    = "/course/v1/user/get/courses"
	pathDetail        = "/saas-business/front/commodity/getDetail/%s"
	pathChapter       = "/saas-business/front/commodity/listVideoChapter/%s/%s"
	pathLecture       = "/saas-business/front/commodity/listVideoLecture/%s/%s"
	pathVideoInfo     = "/saas-business/front/commodity/video/%s/%s/%s"
	pathPlayParam     = "/saas-business/front/commodity/video/getVideoPlayParam"
	pathResourceMenu  = "/saas-business/front/commodity/findClassResourceMenu"
	pathResource      = "/saas-business/front/commodity/findResource/%s/%s"
	pathDownloadLink  = "/saas-business/front/commodity/getDownloadLink/%s"
	urlBokeCCVideoAPI = "https://p.bokecc.com/servlet/getvideofile?vid=%s&siteid=%s"
	urlBokeCCReferer  = "https://p.bokecc.com/"
)

var patterns = []string{`(?:[\w-]+\.)?jingtongxue\.com/`}

func init() {
	extractor.Register(&Jingtongxue{}, extractor.SiteInfo{Name: "Jingtongxue", URL: "jingtongxue.com", NeedAuth: true})
}

type Jingtongxue struct{}

func (s *Jingtongxue) Patterns() []string { return patterns }

var jtxIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)/course/video/([^/?#]+)/([^/?#]+)/([^/?#]+)`),
	regexp.MustCompile(`(?i)/course/detail/([^/?#]+)/([^/?#]+)`),
	regexp.MustCompile(`(?i)(?:commodityId|comId|commodity_id)=([^&#]+)`),
	regexp.MustCompile(`(?i)(?:classTypeId|courseId|class_type_id)=([^&#]+)`),
}

func (s *Jingtongxue) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("jingtongxue requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	headers := jtxHeaders()

	courseID, classTypeID, moduleID, targetLectureID := parseJingtongxueURL(rawURL)
	courses, err := fetchJingtongxueCourses(c, headers)
	if err != nil {
		return nil, err
	}
	courseID, classTypeID, title := chooseJingtongxueCourse(courses, courseID, classTypeID)
	if courseID == "" && len(courses) == 1 {
		courseID, classTypeID, title = courses[0].CommodityID, firstText(courses[0].ClassTypeID), courses[0].Title
	}
	if courseID == "" {
		return nil, fmt.Errorf("cannot parse jingtongxue commodityId from URL: %s", rawURL)
	}

	detail, err := fetchJingtongxueDetail(c, courseID, headers)
	if err == nil {
		if title == "" {
			title = firstText(detail.Name, detail.Title, detail.ClassTypePo.Name)
		}
		if classTypeID == "" {
			classTypeID = firstText(detail.ClassTypeID, detail.ClassTypePo.ID)
		}
	}
	if classTypeID == "" {
		return nil, fmt.Errorf("jingtongxue: missing classTypeId for commodityId=%s", courseID)
	}
	if title == "" {
		title = "jingtongxue_" + courseID
	}

	chapters, err := fetchJingtongxueChapters(c, courseID, classTypeID, headers)
	if err != nil {
		return nil, err
	}
	entries := make([]*extractor.MediaInfo, 0)
	for chapterIndex, chapter := range chapters {
		chapterID := firstText(chapter.ID, chapter.ChapterID)
		if chapterID == "" {
			continue
		}
		chapterModuleID := firstText(chapter.ModuleID, moduleID)
		lectures, err := fetchJingtongxueLectures(c, classTypeID, chapterID, headers)
		if err != nil {
			continue
		}
		for lectureIndex, lecture := range lectures {
			video := normalizeJingtongxueVideo(lecture, chapterModuleID, classTypeID)
			if targetLectureID != "" && video.LectureID != targetLectureID {
				continue
			}
			entry, err := buildJingtongxueEntry(c, headers, video, chapterIndex+1, lectureIndex+1)
			if err != nil {
				continue
			}
			entries = append(entries, entry)
		}
	}

	// Fetch courseware/resource files (resource_menu_api + resource_api).
	resourceEntries := fetchJingtongxueResources(c, courseID, classTypeID, headers)
	entries = append(entries, resourceEntries...)

	if len(entries) == 0 {
		return nil, fmt.Errorf("jingtongxue: no playable video entries or resource files for commodityId=%s classTypeId=%s", courseID, classTypeID)
	}
	return &extractor.MediaInfo{Site: "jingtongxue", Title: title, Entries: entries, Extra: map[string]any{"commodity_id": courseID, "class_type_id": classTypeID, "detail": detail}}, nil
}

func jtxHeaders() map[string]string {
	return map[string]string{
		"Memory":       "1",
		"Content-Type": "application/json;charset=UTF-8",
		"Origin":       urlOrigin,
		"Referer":      urlReferer,
		"Accept":       "application/json, text/plain, */*",
	}
}

func jtxGetJSON(c *util.Client, path string, params map[string]string, headers map[string]string, out any) error {
	apiURL := path
	if !strings.HasPrefix(path, "http") {
		apiURL = urlAPIBase + path
	}
	u, err := url.Parse(apiURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("domain", urlDomain)
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	body, err := c.GetString(u.String(), headers)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("jingtongxue parse %s: %w", u.String(), err)
	}
	return nil
}

type jtxEnvelope[T any] struct {
	Code    any    `json:"code"`
	Success bool   `json:"success"`
	Msg     string `json:"msg"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

func (r jtxEnvelope[T]) ok() bool {
	code := stringValue(r.Code)
	return r.Success || code == "1" || code == "200" || code == "0" || code == ""
}

type jtxCourse struct {
	CommodityID string         `json:"commodityId"`
	ComID       string         `json:"comId"`
	ID          any            `json:"id"`
	CourseID    any            `json:"courseId"`
	ClassTypeID any            `json:"classTypeId"`
	ClassTypePo jtxClassTypePo `json:"classTypePo"`
	Name        string         `json:"name"`
	CourseName  string         `json:"courseName"`
	Title       string         `json:"title"`
	Raw         map[string]any `json:"-"`
}

type jtxClassTypePo struct {
	ID    any    `json:"id"`
	Name  string `json:"name"`
	Price any    `json:"price"`
}

func (c jtxCourse) normalized() jtxCourse {
	c.CommodityID = firstText(c.CommodityID, c.ComID, c.ID)
	c.ClassTypeID = firstText(c.ClassTypeID, c.CourseID, c.ClassTypePo.ID)
	c.Title = firstText(c.Title, c.CourseName, c.Name, c.ClassTypePo.Name)
	return c
}

func fetchJingtongxueCourses(c *util.Client, headers map[string]string) ([]jtxCourse, error) {
	var all []jtxCourse
	seen := map[string]bool{}
	for _, status := range []string{"1", "2", "3"} {
		for offset := 0; ; offset += 50 {
			var raw jtxEnvelope[any]
			err := jtxGetJSON(c, pathCourseList, map[string]string{"status": status, "pageSize": "50", "offset": strconv.Itoa(offset)}, headers, &raw)
			if err != nil {
				return nil, fmt.Errorf("jingtongxue course list: %w", err)
			}
			if !raw.ok() {
				return nil, fmt.Errorf("jingtongxue course list failed: code=%s msg=%s", stringValue(raw.Code), firstText(raw.Msg, raw.Message))
			}
			records := jtxExtractRecords(raw.Data)
			if len(records) == 0 {
				break
			}
			for _, rec := range records {
				b, _ := json.Marshal(rec)
				var item jtxCourse
				if err := json.Unmarshal(b, &item); err != nil {
					continue
				}
				item = item.normalized()
				if item.CommodityID == "" || firstText(item.ClassTypeID) == "" {
					continue
				}
				key := item.CommodityID + ":" + firstText(item.ClassTypeID)
				if !seen[key] {
					seen[key] = true
					all = append(all, item)
				}
			}
			if len(records) < 50 {
				break
			}
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("jingtongxue: empty course list; login cookie may be invalid")
	}
	return all, nil
}

func chooseJingtongxueCourse(courses []jtxCourse, courseID, classTypeID string) (string, string, string) {
	for _, course := range courses {
		if courseID != "" && course.CommodityID != courseID {
			continue
		}
		if classTypeID != "" && firstText(course.ClassTypeID) != classTypeID {
			continue
		}
		return firstText(courseID, course.CommodityID), firstText(classTypeID, course.ClassTypeID), course.Title
	}
	return courseID, classTypeID, ""
}

type jtxDetail struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	ClassTypeID any            `json:"classTypeId"`
	ClassTypePo jtxClassTypePo `json:"classTypePo"`
	BuyFlag     any            `json:"buyFlag"`
	UserVIPFlag any            `json:"userVipFlag"`
	PriceFlag   any            `json:"priceFlag"`
}

func fetchJingtongxueDetail(c *util.Client, commodityID string, headers map[string]string) (jtxDetail, error) {
	var out jtxEnvelope[jtxDetail]
	path := fmt.Sprintf(pathDetail, url.PathEscape(commodityID))
	if err := jtxGetJSON(c, path, map[string]string{"liveSet": "1"}, headers, &out); err != nil {
		return jtxDetail{}, fmt.Errorf("jingtongxue detail: %w", err)
	}
	if !out.ok() {
		return jtxDetail{}, fmt.Errorf("jingtongxue detail failed: code=%s", stringValue(out.Code))
	}
	return out.Data, nil
}

type jtxChapter struct {
	ID          any    `json:"id"`
	ChapterID   any    `json:"chapterId"`
	ChapterName string `json:"chapterName"`
	Name        string `json:"name"`
	ModuleID    any    `json:"moduleId"`
}

func fetchJingtongxueChapters(c *util.Client, commodityID, classTypeID string, headers map[string]string) ([]jtxChapter, error) {
	var out jtxEnvelope[any]
	path := fmt.Sprintf(pathChapter, url.PathEscape(commodityID), url.PathEscape(classTypeID))
	if err := jtxGetJSON(c, path, nil, headers, &out); err != nil {
		return nil, fmt.Errorf("jingtongxue chapters: %w", err)
	}
	if !out.ok() {
		return nil, fmt.Errorf("jingtongxue chapters failed: code=%s", stringValue(out.Code))
	}
	var chapters []jtxChapter
	for _, rec := range jtxExtractRecords(out.Data) {
		b, _ := json.Marshal(rec)
		var item jtxChapter
		if err := json.Unmarshal(b, &item); err == nil {
			chapters = append(chapters, item)
		}
	}
	return chapters, nil
}

type jtxLecture struct {
	ID          any      `json:"id"`
	LectureID   any      `json:"lectureId"`
	LecID       any      `json:"lecId"`
	LectureName string   `json:"lectureName"`
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	FileType    string   `json:"fileType"`
	ModuleID    any      `json:"moduleId"`
	VideoID     any      `json:"videoId"`
	VideoCcID   any      `json:"videoCcId"`
	WebVideoID  any      `json:"webVideoId"`
	SiteID      any      `json:"siteid"`
	Video       jtxVideo `json:"video"`
}

type jtxVideo struct {
	ID          any    `json:"id"`
	VideoName   string `json:"videoName"`
	VideoCcID   any    `json:"videoCcId"`
	WebVideoID  any    `json:"webVideoId"`
	StorageType string `json:"storageType"`
	SiteID      any    `json:"siteid"`
	VideoSize   any    `json:"videoSize"`
	VodeoSize   any    `json:"vodeoSize"`
}

func fetchJingtongxueLectures(c *util.Client, classTypeID, chapterID string, headers map[string]string) ([]jtxLecture, error) {
	var out jtxEnvelope[any]
	path := fmt.Sprintf(pathLecture, url.PathEscape(classTypeID), url.PathEscape(chapterID))
	if err := jtxGetJSON(c, path, nil, headers, &out); err != nil {
		return nil, fmt.Errorf("jingtongxue lectures: %w", err)
	}
	if !out.ok() {
		return nil, fmt.Errorf("jingtongxue lectures failed: code=%s", stringValue(out.Code))
	}
	var lectures []jtxLecture
	for _, rec := range jtxExtractRecords(out.Data) {
		b, _ := json.Marshal(rec)
		var item jtxLecture
		if err := json.Unmarshal(b, &item); err == nil {
			lectures = append(lectures, item)
		}
	}
	return lectures, nil
}

type jtxVideoInfo struct {
	LectureID   string
	ModuleID    string
	ClassTypeID string
	Title       string
	VideoID     string
	VideoCcID   string
	WebVideoID  string
	StorageType string
	SiteID      string
	Size        int64
	Raw         jtxLecture
}

func normalizeJingtongxueVideo(lecture jtxLecture, moduleID, classTypeID string) jtxVideoInfo {
	return jtxVideoInfo{
		LectureID:   firstText(lecture.ID, lecture.LectureID, lecture.LecID),
		ModuleID:    firstText(lecture.ModuleID, moduleID),
		ClassTypeID: classTypeID,
		Title:       firstText(lecture.LectureName, lecture.Name, lecture.Title, lecture.Video.VideoName, "未命名"),
		VideoID:     firstText(lecture.VideoID, lecture.Video.ID),
		VideoCcID:   firstText(lecture.VideoCcID, lecture.Video.VideoCcID),
		WebVideoID:  firstText(lecture.WebVideoID, lecture.Video.WebVideoID),
		StorageType: firstText(lecture.Video.StorageType),
		SiteID:      firstText(lecture.SiteID, lecture.Video.SiteID),
		Size:        int64FromAny(firstText(lecture.Video.VideoSize, lecture.Video.VodeoSize)),
		Raw:         lecture,
	}
}

func buildJingtongxueEntry(c *util.Client, headers map[string]string, video jtxVideoInfo, chapterIndex, lectureIndex int) (*extractor.MediaInfo, error) {
	if video.LectureID == "" && video.VideoID == "" && video.VideoCcID == "" {
		return nil, fmt.Errorf("jingtongxue lecture has no video id")
	}
	playURL, siteID, err := resolveJingtongxuePlayURL(c, headers, video)
	if err != nil {
		return nil, err
	}
	format := mediaExt(playURL)
	stream := extractor.Stream{Quality: "best", URLs: []string{playURL}, Format: format, Size: video.Size, Headers: map[string]string{"Referer": urlReferer}}
	if format == "m3u8" {
		stream.NeedMerge = true
	}
	return &extractor.MediaInfo{Site: "jingtongxue", Title: fmt.Sprintf("[%d.%d]--%s", chapterIndex, lectureIndex, video.Title), Streams: map[string]extractor.Stream{"best": stream}, Extra: map[string]any{"lecture_id": video.LectureID, "module_id": video.ModuleID, "class_type_id": video.ClassTypeID, "video_id": video.VideoID, "video_cc_id": video.VideoCcID, "siteid": siteID}}, nil
}

func resolveJingtongxuePlayURL(c *util.Client, headers map[string]string, video jtxVideoInfo) (string, string, error) {
	var play any
	if video.ModuleID != "" && video.ClassTypeID != "" && video.LectureID != "" {
		var out jtxEnvelope[any]
		err := jtxGetJSON(c, pathPlayParam, map[string]string{"broswer": "pc", "lectureId": video.LectureID, "classTypeId": video.ClassTypeID, "moduleId": video.ModuleID}, headers, &out)
		if err == nil && out.ok() {
			play = out.Data
		}
	}
	if direct := findDirectJingtongxueURL(play); direct != "" {
		return direct, video.SiteID, nil
	}
	if video.VideoCcID == "" && strings.EqualFold(video.StorageType, "VIDEO_STORAGE_TYPE_CC") {
		video.VideoCcID = video.VideoID
	}
	if video.VideoCcID != "" {
		siteID := firstText(video.SiteID, findStringKey(play, "siteid"), findStringKey(play, "ccUserId"))
		if siteID == "" {
			siteID = fetchJingtongxueSiteIDFromVideoInfo(c, headers, video)
		}
		if siteID == "" {
			return "", "", fmt.Errorf("jingtongxue bokecc: missing siteid for vid=%s", video.VideoCcID)
		}
		playURL, err := shared.BokeCCResolve(c, video.VideoCcID, siteID, map[string]string{"Referer": urlBokeCCReferer})
		if err != nil {
			return "", siteID, err
		}
		return playURL, siteID, nil
	}
	return "", video.SiteID, fmt.Errorf("jingtongxue: no direct or BokeCC play URL for lecture=%s", video.LectureID)
}

func fetchJingtongxueSiteIDFromVideoInfo(c *util.Client, headers map[string]string, video jtxVideoInfo) string {
	if video.ModuleID == "" || video.ClassTypeID == "" || video.LectureID == "" {
		return ""
	}
	var out jtxEnvelope[any]
	path := fmt.Sprintf(pathVideoInfo, url.PathEscape(video.ModuleID), url.PathEscape(video.ClassTypeID), url.PathEscape(video.LectureID))
	if err := jtxGetJSON(c, path, nil, headers, &out); err != nil || !out.ok() {
		return ""
	}
	return firstText(findStringKey(out.Data, "siteid"), findStringKey(out.Data, "ccUserId"))
}

// ---------------------------------------------------------------------------
// Resource / courseware download flow
// Source: Jingtongxue_Course._get_source_info, _parse_resource_file,
//         _get_file_url, _download_one_file
// APIs:   resource_menu_api, resource_api, download_link_api
// ---------------------------------------------------------------------------

// jtxResourceMenuItem represents one item from the resource menu API.
type jtxResourceMenuItem struct {
	ID   any    `json:"id"`
	Name string `json:"name"`
}

// jtxResourceFile represents one resource/file from the resource API.
type jtxResourceFile struct {
	ID          any    `json:"id"`
	ResourceID  any    `json:"resourceId"`
	Name        string `json:"name"`
	FileName    string `json:"fileName"`
	Title       string `json:"title"`
	Download    string `json:"download"`
	DownloadURL string `json:"downloadUrl"`
	FilePath    string `json:"filePath"`
	Path        string `json:"path"`
	URL         string `json:"url"`
	Format      string `json:"format"`
	FileType    string `json:"fileType"`
	Suffix      string `json:"suffix"`
	Ext         string `json:"ext"`
}

// fetchJingtongxueResourceMenu fetches the resource menu categories.
// Source: _get_source_info calls resource_menu_api with classTypeId param.
func fetchJingtongxueResourceMenu(c *util.Client, classTypeID string, headers map[string]string) []jtxResourceMenuItem {
	var out jtxEnvelope[any]
	if err := jtxGetJSON(c, pathResourceMenu, map[string]string{"classTypeId": classTypeID}, headers, &out); err != nil || !out.ok() {
		return nil
	}
	// data is expected to be a list; if not, return empty
	dataList, ok := out.Data.([]any)
	if !ok || len(dataList) == 0 {
		return nil
	}
	var items []jtxResourceMenuItem
	for _, rec := range dataList {
		b, _ := json.Marshal(rec)
		var item jtxResourceMenuItem
		if err := json.Unmarshal(b, &item); err == nil {
			items = append(items, item)
		}
	}
	return items
}

// fetchJingtongxueResourceList fetches resources under one menu category.
// Source: _get_source_info calls resource_api with commodity_id, class_type_id,
//         page, pageSize, and optional firstMenuId.
func fetchJingtongxueResourceList(c *util.Client, commodityID, classTypeID, menuID string, headers map[string]string) []jtxResourceFile {
	params := map[string]string{"page": "1", "pageSize": "100"}
	if menuID != "" {
		params["firstMenuId"] = menuID
	}
	path := fmt.Sprintf(pathResource, url.PathEscape(commodityID), url.PathEscape(classTypeID))
	var out jtxEnvelope[any]
	if err := jtxGetJSON(c, path, params, headers, &out); err != nil || !out.ok() {
		return nil
	}
	records := jtxExtractRecords(out.Data)
	var files []jtxResourceFile
	for _, rec := range records {
		b, _ := json.Marshal(rec)
		var item jtxResourceFile
		if err := json.Unmarshal(b, &item); err == nil {
			files = append(files, item)
		}
	}
	return files
}

// fetchJingtongxueDownloadLink resolves a download URL via the download_link_api.
// Source: _get_file_url calls download_link_api.format(resource_id) when no
//         direct URL is available on the resource record.
func fetchJingtongxueDownloadLink(c *util.Client, resourceID string, headers map[string]string) string {
	path := fmt.Sprintf(pathDownloadLink, url.PathEscape(resourceID))
	var out jtxEnvelope[any]
	if err := jtxGetJSON(c, path, nil, headers, &out); err != nil || !out.ok() {
		return ""
	}
	switch d := out.Data.(type) {
	case string:
		return normalizeURL(strings.TrimSpace(d), urlReferer)
	case map[string]any:
		for _, k := range []string{"url", "downloadUrl", "path", "filePath"} {
			if v, ok := d[k]; ok {
				if s := stringValue(v); s != "" {
					return normalizeURL(s, urlReferer)
				}
			}
		}
	}
	return ""
}

// resourceFileURL resolves the final download URL for a resource file.
// Mirrors source _get_file_url: prefer direct URL, fallback to download_link_api.
func resourceFileURL(c *util.Client, res jtxResourceFile, headers map[string]string) string {
	// Try direct URL from the resource record first.
	for _, raw := range []string{res.Download, res.DownloadURL, res.FilePath, res.Path, res.URL} {
		u := normalizeURL(strings.TrimSpace(raw), urlReferer)
		if u != "" && strings.HasPrefix(u, "http") {
			return u
		}
	}
	// Fallback: call download_link_api with the resource id.
	resID := firstText(res.ID, res.ResourceID)
	if resID == "" {
		return ""
	}
	return fetchJingtongxueDownloadLink(c, resID, headers)
}

// resourceFileExt determines the file extension from the resource metadata or URL.
// Mirrors source _parse_resource_file extension logic.
func resourceFileExt(res jtxResourceFile, fileURL string) string {
	ext := strings.TrimLeft(strings.ToLower(strings.TrimSpace(firstText(res.Format, res.FileType, res.Suffix, res.Ext))), ".")
	if ext != "" {
		return ext
	}
	// Infer from URL: take last path segment, extract extension.
	if fileURL == "" {
		return "pdf" // default per source
	}
	seg := fileURL
	if idx := strings.LastIndex(seg, "/"); idx >= 0 {
		seg = seg[idx+1:]
	}
	if idx := strings.Index(seg, "?"); idx >= 0 {
		seg = seg[:idx]
	}
	if idx := strings.LastIndex(seg, "."); idx >= 0 {
		return strings.ToLower(seg[idx+1:])
	}
	return "pdf" // default per source
}

// resourceFileName returns a sanitized display name for a resource file.
func resourceFileName(res jtxResourceFile, fileURL, ext string) string {
	name := firstText(res.Name, res.FileName, res.Title)
	if name == "" && fileURL != "" {
		seg := fileURL
		if idx := strings.LastIndex(seg, "/"); idx >= 0 {
			seg = seg[idx+1:]
		}
		if idx := strings.Index(seg, "?"); idx >= 0 {
			seg = seg[:idx]
		}
		name = seg
	}
	if name == "" {
		name = firstText(res.ID, res.ResourceID)
	}
	// Strip the extension suffix from name if it matches, like source _strip_file_ext.
	if ext != "" && strings.HasSuffix(strings.ToLower(name), "."+ext) {
		name = name[:len(name)-len(ext)-1]
	}
	return name
}

// fetchJingtongxueResources fetches all courseware/resource files and returns
// them as MediaInfo entries. This mirrors the source _get_source_info +
// _parse_resource_file + _get_file_url flow.
func fetchJingtongxueResources(c *util.Client, commodityID, classTypeID string, headers map[string]string) []*extractor.MediaInfo {
	if commodityID == "" || classTypeID == "" {
		return nil
	}
	menuItems := fetchJingtongxueResourceMenu(c, classTypeID, headers)
	// Source: if no menu items returned, synthesize one with empty id.
	if len(menuItems) == 0 {
		menuItems = []jtxResourceMenuItem{{Name: "资料"}}
	}

	var entries []*extractor.MediaInfo
	for menuIdx, menu := range menuItems {
		menuID := firstText(menu.ID)
		menuName := strings.TrimSpace(menu.Name)
		if menuName == "" {
			menuName = "资料"
		}
		resources := fetchJingtongxueResourceList(c, commodityID, classTypeID, menuID, headers)
		for fileIdx, res := range resources {
			fileURL := resourceFileURL(c, res, headers)
			if fileURL == "" {
				resID := firstText(res.ID, res.ResourceID)
				if resID == "" {
					continue // no id and no URL -- skip
				}
				// Keep entry with empty URL; downstream can retry.
			}
			ext := resourceFileExt(res, fileURL)
			name := resourceFileName(res, fileURL, ext)
			if name == "" {
				name = fmt.Sprintf("resource_%d_%d", menuIdx+1, fileIdx+1)
			}

			displayTitle := fmt.Sprintf("(%d.%d)--%s", menuIdx+1, fileIdx+1, name)
			format := ext
			if format == "" {
				format = "pdf"
			}

			stream := extractor.Stream{
				Quality: "best",
				URLs:    []string{fileURL},
				Format:  format,
				Headers: map[string]string{"Referer": urlReferer},
			}

			entry := &extractor.MediaInfo{
				Site:    "jingtongxue",
				Title:   displayTitle,
				Streams: map[string]extractor.Stream{"best": stream},
				Extra: map[string]any{
					"type":          "file",
					"file_id":       firstText(res.ID, res.ResourceID),
					"file_fmt":      ext,
					"menu_category": menuName,
				},
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

func parseJingtongxueURL(rawURL string) (courseID, classTypeID, moduleID, lectureID string) {
	if m := jtxIDPatterns[0].FindStringSubmatch(rawURL); len(m) == 4 {
		moduleID = m[1]
		classTypeID = m[2]
		courseID = m[3]
	}
	if m := jtxIDPatterns[1].FindStringSubmatch(rawURL); len(m) == 3 {
		courseID = firstText(courseID, m[1])
		classTypeID = firstText(classTypeID, m[2])
	}
	if u, err := url.Parse(rawURL); err == nil {
		q := u.Query()
		if frag, err := url.Parse(u.Fragment); err == nil {
			for k, vals := range frag.Query() {
				for _, v := range vals {
					q.Add(k, v)
				}
			}
		}
		courseID = firstText(courseID, q.Get("commodityId"), q.Get("comId"), q.Get("commodity_id"))
		classTypeID = firstText(classTypeID, q.Get("classTypeId"), q.Get("courseId"), q.Get("class_type_id"))
		moduleID = firstText(moduleID, q.Get("moduleId"), q.Get("module_id"))
		lectureID = firstText(q.Get("lecId"), q.Get("lectureId"), q.Get("lecture_id"))
	}
	return
}

func jtxExtractRecords(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		for _, key := range []string{"records", "rows", "list", "items", "data"} {
			if recs := jtxExtractRecords(x[key]); len(recs) > 0 {
				return recs
			}
		}
	}
	return nil
}

func findDirectJingtongxueURL(v any) string {
	switch x := v.(type) {
	case string:
		u := normalizeURL(x, urlReferer)
		lu := strings.ToLower(u)
		if strings.HasPrefix(u, "http") && (strings.Contains(lu, ".m3u8") || strings.Contains(lu, ".mp4")) {
			return u
		}
	case map[string]any:
		for _, key := range []string{"videoSrc", "webVideoDomain", "url", "playUrl", "m3u8", "m3u8Url", "filePath", "path"} {
			if u := findDirectJingtongxueURL(x[key]); u != "" {
				return u
			}
		}
		for _, item := range x {
			if u := findDirectJingtongxueURL(item); u != "" {
				return u
			}
		}
	case []any:
		for _, item := range x {
			if u := findDirectJingtongxueURL(item); u != "" {
				return u
			}
		}
	}
	return ""
}

func findStringKey(v any, key string) string {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if strings.EqualFold(k, key) {
				if s := firstText(val); s != "" {
					return s
				}
			}
			if s := findStringKey(val, key); s != "" {
				return s
			}
		}
	case []any:
		for _, item := range x {
			if s := findStringKey(item, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func normalizeURL(raw, base string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "?") {
		if u, err := url.Parse(base); err == nil {
			ref, _ := url.Parse(raw)
			return u.ResolveReference(ref).String()
		}
	}
	return raw
}

func mediaExt(u string) string {
	lu := strings.ToLower(u)
	switch {
	case strings.Contains(lu, ".m3u8"):
		return "m3u8"
	case strings.Contains(lu, ".flv"):
		return "flv"
	default:
		return "mp4"
	}
}

func firstText(values ...any) string {
	for _, v := range values {
		if s := stringValue(v); s != "" {
			return s
		}
	}
	return ""
}

func stringValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	case json.Number:
		return strings.TrimSpace(x.String())
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return strings.TrimSpace(fmt.Sprint(x))
	default:
		return ""
	}
}

func int64FromAny(v any) int64 {
	s := stringValue(v)
	if s == "" {
		return 0
	}
	if strings.Contains(s, ".") {
		f, _ := strconv.ParseFloat(s, 64)
		return int64(f)
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
