package zhihuishu

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"regexp"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
	xhtml "golang.org/x/net/html"
)

// Crypto constants ported verbatim from Mooc/Courses/Zhihuishu/Zhihuishu_Config.pyc.
// AESEncrypt is constructed with mode=CBC and the default padding=pkcs7
// (Mooc/Component/Mooc_Crypt.pyc AESEncrypt.__init__ defaults).
const (
	studyVideoAESKey = "azp53h0kft7qi78q" // STUDY_VIDEO_AES_KEY
	zhsAESIV         = "1g3qqdh4jvbskb9x" // ZHS_AES_IV
)

// API endpoints ported verbatim from Zhihuishu_Course.pyc class attributes.
const (
	urlCourse    = "https://coursehome.zhihuishu.com/courseHome/%s?ft=map"
	urlContent   = "https://coursehome.zhihuishu.com/home/communication/content/%s/%s"
	urlQuery     = "https://studyservice-api.zhihuishu.com/gateway/t/v1/learning/queryCourse"
	urlJoin      = "https://coursehome.zhihuishu.com/home/toNewInterestKeep/%s/%s"
	urlInfo      = "https://studyservice-api.zhihuishu.com/gateway/t/v1/learning/videolist"
	urlInfoAI    = "https://aistudy.zhihuishu.com/gateway/t/v1/learning/kgCourseTreeInfoList?secretStr=%s"
	urlVideoInit = "https://newbase.zhihuishu.com/video/initVideo?videoID=%s"
)

// courseContext mirrors the cid/crid/rid/tid state the Python Zhihuishu_Course
// threads through _get_cid -> _get_title -> _get_infos.
type courseContext struct {
	cid  string
	crid string
	rid  string
	tid  string
}

type courseHomeVideo struct {
	Title   string
	VideoID string
}

func extractCourseHomeCourse(rawURL, courseID string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := zhihuishuHeaders("https://coursehome.zhihuishu.com/")

	ctx := courseContext{cid: courseID, crid: extractRecruitAndCourseID(rawURL)}

	// _get_cid: when only recruitAndCourseId is known, resolve cid/rid via the
	// AES-signed queryCourse API (Zhihuishu_Course._get_cid).
	if ctx.crid != "" && ctx.cid == "" {
		resolveCidFromCrid(c, &ctx, h)
	}

	// _get_title: scrape the courseHome page for termId/courseName/schoolName/
	// recruitId, then resolve crid via the toNewInterestKeep redirect.
	title := "zhihuishu_" + firstNonEmpty(ctx.cid, ctx.crid, courseID)
	if ctx.cid != "" {
		page, err := c.GetString(fmt.Sprintf(urlCourse, ctx.cid), h)
		if err != nil {
			return nil, fmt.Errorf("fetch courseHome page: %w", err)
		}
		title = courseHomeTitle(page, firstNonEmpty(ctx.cid, courseID))
		ctx.tid = firstNonEmpty(ctx.tid, match1(page, `var\s+termId\s*=\s*(-?\d+);`))
		ctx.rid = firstNonEmpty(ctx.rid, match1(page, `var\s+recruitId\s*=\s*(\d+);`))
		if ctx.crid == "" && ctx.cid != "" && ctx.rid != "" {
			if resolved := resolveCridFromJoin(c, ctx.cid, ctx.rid, h); resolved != "" {
				ctx.crid = resolved
			}
		}
	}

	// _get_infos: enumerate the full course tree. Primary source is the
	// AES-signed videolist / kgCourseTreeInfoList APIs; HTML scrape of the
	// communication/content page is the documented fallback.
	videos := getInfos(c, &ctx, h)
	if len(videos) == 0 {
		return nil, fmt.Errorf("courseHome %s returned no videos (no videolist/kgCourseTree/content entries)", firstNonEmpty(ctx.cid, ctx.crid, courseID))
	}

	var entries []*extractor.MediaInfo
	var firstErr error
	for _, item := range videos {
		if item.VideoID == "" {
			continue
		}
		videoURL, err := getVideoURL(c, item.VideoID, h)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		subURL, _ := getSubtitleURL(c, item.VideoID, h)
		entries = append(entries, &extractor.MediaInfo{
			Site:  "zhihuishu",
			Title: item.Title,
			Streams: map[string]extractor.Stream{
				"default": {
					Quality: "best",
					URLs:    []string{videoURL},
					Format:  pickFormat(videoURL),
					Headers: h,
				},
			},
			Subtitles: subtitleFromURL(subURL),
		})
	}
	if len(entries) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("courseHome %s returned no playable videos: %w", firstNonEmpty(ctx.cid, ctx.crid, courseID), firstErr)
		}
		return nil, fmt.Errorf("courseHome %s returned no playable videos", firstNonEmpty(ctx.cid, ctx.crid, courseID))
	}

	// Collect course resources (files/courseware) from the resource tree.
	// Zhihuishu_Course._download_source uses _get_file_list + _get_file_url etc.
	resourceEntries := collectCourseResources(c, &ctx, h)
	entries = append(entries, resourceEntries...)

	// Collect hash-file entries from AI tree (idHash/idStr -> queryNodeDescription)
	hashEntries := collectHashFileEntries(c, &ctx, h)
	entries = append(entries, hashEntries...)

	return &extractor.MediaInfo{
		Site:    "zhihuishu",
		Title:   title,
		Entries: entries,
		Extra: map[string]any{
			"course_id":          ctx.cid,
			"recruit_id":         ctx.rid,
			"term_id":            ctx.tid,
			"recruit_course_id":  ctx.crid,
			"discovered_entries": len(entries),
			"resource_entries":   len(resourceEntries),
			"hash_entries":       len(hashEntries),
			"raw_url":            rawURL,
		},
	}, nil
}

// getInfos enumerates the course tree exactly as Zhihuishu_Course._get_infos:
//  1. videolist API (AES secretStr from recruitAndCourseId) -> videoChapterDtos
//  2. kgCourseTreeInfoList API (AES secretStr from courseId+recruitId) -> data[].lessons
//  3. communication/content HTML scrape (fallback when APIs yield nothing)
//
// idHash/idStr "hash" lessons from the AI tree are share-course-map resources,
// not videoIDs, and are intentionally not turned into playable entries here.
func getInfos(c *util.Client, ctx *courseContext, h map[string]string) []courseHomeVideo {
	var out []courseHomeVideo

	if ctx.crid != "" {
		plain := fmt.Sprintf(`{"recruitAndCourseId":"%s","dateFormate":%d}`, ctx.crid, time.Now().UnixMilli())
		secret, err := aesEncryptSecret(plain)
		if err == nil {
			body, err := c.PostForm(urlInfo, map[string]string{"secretStr": secret}, h)
			if err == nil {
				ctx.rid, out = parseVideolist(body, ctx.rid)
			}
		}
	}

	if len(out) == 0 && ctx.cid != "" && ctx.rid != "" {
		plain := fmt.Sprintf(`{"courseId":%s,"recruitId":%s,"dateFormate":%d}`, ctx.cid, ctx.rid, time.Now().UnixMilli())
		secret, err := aesEncryptSecret(plain)
		if err == nil {
			body, err := c.GetString(fmt.Sprintf(urlInfoAI, secret), h)
			if err == nil {
				out = parseKgCourseTree(body)
			}
		}
	}

	if len(out) == 0 && ctx.cid != "" && ctx.tid != "" {
		contentURL := fmt.Sprintf(urlContent, ctx.cid, ctx.tid)
		if body, err := c.PostForm(contentURL, map[string]string{}, h); err == nil {
			if scraped, perr := parseCourseHomeVideos(body); perr == nil {
				out = scraped
			}
		}
	}

	return out
}

// videolistResponse maps the studyservice videolist payload.
type videolistResponse struct {
	Data struct {
		RecruitID json.Number `json:"recruitId"`
		Chapters  []struct {
			Name         string `json:"name"`
			VideoLessons []struct {
				Name              string      `json:"name"`
				VideoID           json.Number `json:"videoId"`
				VideoSmallLessons []struct {
					Name    string      `json:"name"`
					VideoID json.Number `json:"videoId"`
				} `json:"videoSmallLessons"`
			} `json:"videoLessons"`
		} `json:"videoChapterDtos"`
	} `json:"data"`
}

func parseVideolist(body, existingRID string) (string, []courseHomeVideo) {
	var resp videolistResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return existingRID, nil
	}
	rid := existingRID
	if rid == "" && resp.Data.RecruitID.String() != "" {
		rid = resp.Data.RecruitID.String()
	}
	var out []courseHomeVideo
	for ci, ch := range resp.Data.Chapters {
		for li, lesson := range ch.VideoLessons {
			if vid := videoIDString(lesson.VideoID); vid != "" {
				out = append(out, courseHomeVideo{
					Title:   fmt.Sprintf("[%d.%d]--%s", ci+1, li+1, sanitizeCourseHomeName(lesson.Name)),
					VideoID: vid,
				})
			}
			for si, small := range lesson.VideoSmallLessons {
				if vid := videoIDString(small.VideoID); vid != "" {
					out = append(out, courseHomeVideo{
						Title:   fmt.Sprintf("[%d.%d.%d]--%s", ci+1, li+1, si+1, sanitizeCourseHomeName(small.Name)),
						VideoID: vid,
					})
				}
			}
		}
	}
	return rid, out
}

// kgCourseTreeResponse maps the aistudy kgCourseTreeInfoList payload. The AI
// tree carries playable nodes as idHash/idStr (share-course-map resources);
// only nodes that also expose a numeric videoId are turned into video entries.
type kgCourseTreeResponse struct {
	Data []struct {
		Name    string `json:"name"`
		Lessons []struct {
			Name         string      `json:"name"`
			VideoID      json.Number `json:"videoId"`
			SmallLessons []struct {
				Name    string      `json:"name"`
				VideoID json.Number `json:"videoId"`
			} `json:"smallLessons"`
		} `json:"lessons"`
	} `json:"data"`
}

func parseKgCourseTree(body string) []courseHomeVideo {
	var resp kgCourseTreeResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil
	}
	var out []courseHomeVideo
	for ci, node := range resp.Data {
		for li, lesson := range node.Lessons {
			if vid := videoIDString(lesson.VideoID); vid != "" {
				out = append(out, courseHomeVideo{
					Title:   fmt.Sprintf("[%d.%d]--%s", ci+1, li+1, sanitizeCourseHomeName(lesson.Name)),
					VideoID: vid,
				})
			}
			for si, small := range lesson.SmallLessons {
				if vid := videoIDString(small.VideoID); vid != "" {
					out = append(out, courseHomeVideo{
						Title:   fmt.Sprintf("[%d.%d.%d]--%s", ci+1, li+1, si+1, sanitizeCourseHomeName(small.Name)),
						VideoID: vid,
					})
				}
			}
		}
	}
	return out
}

// videoIDString returns the videoId as a string, skipping the sentinel -1 and
// empty/zero values (Python: `if videoId and videoId != -1`).
func videoIDString(n json.Number) string {
	s := n.String()
	if s == "" || s == "-1" || s == "0" {
		return ""
	}
	return s
}

// resolveCidFromCrid implements Zhihuishu_Course._get_cid's queryCourse branch.
func resolveCidFromCrid(c *util.Client, ctx *courseContext, h map[string]string) {
	plain := fmt.Sprintf(`{"recruitAndCourseId":"%s","dateFormate":%d}`, ctx.crid, time.Now().UnixMilli())
	secret, err := aesEncryptSecret(plain)
	if err != nil {
		return
	}
	body, err := c.PostForm(urlQuery, map[string]string{"secretStr": secret}, h)
	if err != nil {
		return
	}
	var resp struct {
		Data struct {
			CourseInfo struct {
				CourseID json.Number `json:"courseId"`
			} `json:"courseInfo"`
			RecruitID json.Number `json:"recruitId"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return
	}
	if cid := resp.Data.CourseInfo.CourseID.String(); cid != "" && cid != "0" {
		ctx.cid = cid
	}
	if rid := resp.Data.RecruitID.String(); rid != "" && rid != "0" {
		ctx.rid = rid
	}
}

// resolveCridFromJoin follows the toNewInterestKeep redirect and reads
// recruitAndCourseId from the final URL (Zhihuishu_Course._get_title).
func resolveCridFromJoin(c *util.Client, cid, rid string, h map[string]string) string {
	resp, err := c.Get(fmt.Sprintf(urlJoin, cid, rid), h)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.Request == nil || resp.Request.URL == nil {
		return ""
	}
	return extractRecruitAndCourseID(resp.Request.URL.String())
}

// aesEncryptSecret encrypts the secretStr payload with AES-CBC/PKCS7 and base64
// encodes it, matching Mooc_Crypt.AESEncrypt.aes_encrypt (mode=CBC, pkcs7).
func aesEncryptSecret(plaintext string) (string, error) {
	block, err := aes.NewCipher([]byte(studyVideoAESKey))
	if err != nil {
		return "", err
	}
	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)
	out := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, []byte(zhsAESIV))
	mode.CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	padding := make([]byte, pad)
	for i := range padding {
		padding[i] = byte(pad)
	}
	return append(data, padding...)
}

func courseHomeTitle(page, fallback string) string {
	courseName := stdhtml.UnescapeString(match1(page, `var\s+courseName\s*=\s*"(.*?)";`))
	schoolName := stdhtml.UnescapeString(match1(page, `var\s+schoolName\s*=\s*"(.*?)";`))
	switch {
	case courseName != "" && schoolName != "":
		return sanitize(courseName + "_" + schoolName)
	case courseName != "":
		return sanitize(courseName)
	default:
		return "zhihuishu_" + fallback
	}
}

func parseCourseHomeVideos(body string) ([]courseHomeVideo, error) {
	doc, err := xhtml.Parse(strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	var out []courseHomeVideo
	wraps := findByClass(doc, "div", "online-sections-wrap")
	for ci, wrap := range wraps {
		sections := findByClass(wrap, "div", "sections-wrap")
		for si, sectionWrap := range sections {
			sectionItem := firstByClass(sectionWrap, "div", "section-item")
			if sectionItem != nil {
				if videoID := attr(sectionItem, "videoid"); videoID != "" {
					title := titleFromClass(sectionItem, "online-section-title-text-wrap")
					out = append(out, courseHomeVideo{
						Title:   fmt.Sprintf("[%d.%d]--%s", ci+1, si+1, sanitizeCourseHomeName(title)),
						VideoID: videoID,
					})
				}
			}
			childNodes := findByClass(sectionWrap, "div", "section-childnode-item")
			for di, child := range childNodes {
				if videoID := attr(child, "videoid"); videoID != "" {
					title := titleFromClass(child, "online-section-title-text-wrap")
					out = append(out, courseHomeVideo{
						Title:   fmt.Sprintf("[%d.%d.%d]--%s", ci+1, si+1, di+1, sanitizeCourseHomeName(title)),
						VideoID: videoID,
					})
				}
			}
		}
	}
	return out, nil
}

func sanitizeCourseHomeName(s string) string {
	s = stdhtml.UnescapeString(strings.TrimSpace(s))
	if s == "" {
		return "zhihuishu"
	}
	return sanitize(s)
}

func titleFromClass(n *xhtml.Node, className string) string {
	if n == nil {
		return ""
	}
	if child := firstByClass(n, "", className); child != nil {
		if title := attr(child, "title"); title != "" {
			return title
		}
	}
	return ""
}

func attr(n *xhtml.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func firstByClass(n *xhtml.Node, tag, className string) *xhtml.Node {
	nodes := findByClass(n, tag, className)
	if len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

func findByClass(n *xhtml.Node, tag, className string) []*xhtml.Node {
	var out []*xhtml.Node
	var walk func(*xhtml.Node)
	walk = func(cur *xhtml.Node) {
		if cur == nil {
			return
		}
		if nodeMatchesClass(cur, tag, className) {
			out = append(out, cur)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return out
}

func nodeMatchesClass(n *xhtml.Node, tag, className string) bool {
	if n == nil || n.Type != xhtml.ElementNode {
		return false
	}
	if tag != "" && !strings.EqualFold(n.Data, tag) {
		return false
	}
	if className == "" {
		return true
	}
	for _, a := range n.Attr {
		if !strings.EqualFold(a.Key, "class") {
			continue
		}
		for _, item := range strings.Fields(a.Val) {
			if item == className {
				return true
			}
		}
	}
	return false
}

func getSubtitleURL(c *util.Client, videoID string, h map[string]string) (string, error) {
	body, err := c.GetString(fmt.Sprintf("https://newbase.zhihuishu.com/video/subtitleV1/?id=%s", videoID), h)
	if err != nil {
		return "", err
	}
	if m := regexp.MustCompile(`\\"src\\"\s*:\s*\\"(http[^\\"]+)\\"`).FindStringSubmatch(body); len(m) > 1 {
		return strings.ReplaceAll(m[1], `\/`, `/`), nil
	}
	if m := regexp.MustCompile(`"src"\s*:\s*"(http[^"]+)"`).FindStringSubmatch(body); len(m) > 1 {
		return strings.ReplaceAll(m[1], `\/`, `/`), nil
	}
	return "", nil
}

func subtitleFromURL(u string) []extractor.Subtitle {
	if u == "" {
		return nil
	}
	return []extractor.Subtitle{{Language: "zh", URL: u, Format: "srt"}}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func match1(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

// extractRecruitAndCourseID pulls the recruitAndCourseId token from any
// zhihuishu URL (Mooc_Config courses_re crid1 group).
var recruitAndCourseIDRe = regexp.MustCompile(`(?i)recruitAndCourseId=(\w+)`)

func extractRecruitAndCourseID(u string) string {
	if m := recruitAndCourseIDRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	return ""
}

var sanitizeRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)

func sanitize(s string) string {
	return sanitizeRe.ReplaceAllString(strings.TrimSpace(s), "_")
}
