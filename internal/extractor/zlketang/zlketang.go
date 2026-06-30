// Package zlketang implements an extractor for zlketang.com (之了课堂) courses.
package zlketang

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
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL             = "https://www.zlketang.com/"
	originURL              = "https://www.zlketang.com"
	checkURL               = "https://www.zlketang.com/wxpub/api/user_info"
	courseListAPI          = "https://www.zlketang.com/wxpub/api/user_profession_coursev3"
	coursePackageAPI       = "https://www.zlketang.com/wxpub/course/course_package"
	shicaoCourseTreeAPI    = "https://www.zlketang.com/wxpub/shicao/category_tree_course_v2"
	courseDetailAPI        = "https://www.zlketang.com/wxpub/api/course_detail"
	goodsDetailV3API       = "https://www.zlketang.com/wxpub/api/goods_detailv3"
	courseCatalogAPI       = "https://www.zlketang.com/wxpub/api/course"
	orderListAPI           = "https://www.zlketang.com/wxpub/api/orderv2"
	shicaoMyCourseListAPI  = "https://www.zlketang.com/wxpub/shicao/my_course_list"
	courseVideoAPI         = "https://www.zlketang.com/wxpub/api/course_video_switchv2"
	freeCourseVideoAPI     = "https://www.zlketang.com/wxpub/free_api/course_video_switch_v2"
	practiceCourseVideoAPI = "https://www.zlketang.com/wxpub/free_api/practice_course_video_switch_v2"
	videoPlayAPI           = "https://www.zlketang.com/wxpub/api/video_play_detail"
	liveDetailAPI          = "https://www.zlketang.com/wxpub/api/live_detail"
	qcloudPlayAPITmpl      = "https://playvideo.qcloud.com/getplayinfo/v4/%s/%s"
	personalReferer        = "https://www.zlketang.com/personal/index.html?name=1"
	commodityRefererTmpl   = "https://www.zlketang.com/public/wxpub/page/zl_course/commodity.html?product_id=%s"
	livePlayRefererTmpl    = "https://www.zlketang.com/zl_course/live_play.html?live_id=%s"
	rsaPublicKeyPEM        = "-----BEGIN PUBLIC KEY-----\nMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQC3pDA7GTxOvNbXRGMi9QSIzQEI\n+EMD1HcUPJSQSFuRkZkWo4VQECuPRg/xVjqwX1yUrHUvGQJsBwTS/6LIcQiSwYsO\nqf+8TWxGQOJyW46gPPQVzTjNTiUoq435QB0v11lNxvKWBQIZLmacUZ2r1APta7i/\nMY4Lx9XlZVMZNUdUywIDAQAB\n-----END PUBLIC KEY-----"
	playAESKey             = "bl538e945d5d3c41047b3b50j34ca72c"
	liveAESKey             = "cm538e94525e3c41f47bcb50j34ca6x8"
)

var (
	patterns     = []string{`(?:[\w-]+\.)?zlketang\.com/`}
	courseIDRe   = regexp.MustCompile(`(?:course_id|courseId|course|zl_play_course_id|zl_commodity_course_id)=([0-9A-Za-z_\-]+)`)
	productIDRe  = regexp.MustCompile(`(?:product_id|productId|product|zl_product_id|zl_commodity_product_id)=([0-9A-Za-z_\-]+)`)
	mediaURLRe   = regexp.MustCompile(`https?://[^"'\s<>]+(?:\.m3u8|\.mp4|\.pdf|\.pptx?|\.docx?|\.xlsx?|\.zip|\.rar|\.7z|\.txt|\.mp3)[^"'\s<>]*`)
	titleCleanRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
	fileURLKeys  = []string{"file_url", "fileUrl", "download_url", "downloadUrl", "attach_url", "attachUrl", "resource_url", "resourceUrl", "pdf_url", "pdfUrl", "ppt_url", "pptUrl", "doc_url", "docUrl", "url"}
	shicaoTypes  = map[string]bool{"1": true, "15": true, "16": true}
)

func init() {
	extractor.Register(&Zlketang{}, extractor.SiteInfo{Name: "Zlketang", URL: "zlketang.com", NeedAuth: true})
}

type Zlketang struct{}

func (s *Zlketang) Patterns() []string { return patterns }

type zlContext struct {
	c         *util.Client
	headers   map[string]string
	cid       string
	pid       string
	onlyFiles bool
}

type zlNode struct {
	CourseID    string
	SubCourseID string
	SubjectID   string
	Profession  string
	Year        string
	TeacherID   string
	CourseType  string
	Title       string
	Raw         map[string]any
}

type zlVideo struct {
	Title           string
	SectionID       string
	CourseID        string
	LiveID          string
	VideoID         string
	ItemType        string
	PlayAuth        map[string]any
	SourceURL       string
	SourceType      string
	CourseSectionID string
}

type zlFile struct {
	Title  string
	URL    string
	Format string
}

type zlPlaySource struct {
	URL   string
	Extra map[string]any
}

func (s *Zlketang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("zlketang requires login cookies")
	}
	cid, pid := parseIDs(rawURL)
	if cid == "" && pid == "" {
		return nil, fmt.Errorf("zlketang: cannot parse course_id/product_id from URL")
	}
	ctx := &zlContext{c: util.NewClient(), headers: headersFromJar(opts.Cookies), cid: cid, pid: pid, onlyFiles: zlketangOnlyFilesMode(opts.Quality)}
	ctx.c.SetCookieJar(opts.Cookies)
	_, _ = ctx.requestJSON(checkURL, nil, refererURL)

	payloads, title := ctx.loadTopLevelPayloads()
	nodes := ctx.buildAvailableNodes(payloads)
	if len(nodes) == 0 && (ctx.cid != "" || ctx.pid != "") {
		nodes = []zlNode{{CourseID: ctx.cid, SubCourseID: ctx.cid, Title: firstNonEmpty(title, ctx.cid, ctx.pid)}}
	}
	var videos []zlVideo
	var files []zlFile
	for i, node := range nodes {
		detailPayloads := ctx.loadNodePayloads(node)
		files = append(files, collectFiles(detailPayloads, node.Title)...)
		if !ctx.onlyFiles {
			videoData := ctx.getVideoData(node)
			videos = append(videos, parseVideoList(videoData, node, i+1)...)
			videos = append(videos, parseVideoList(detailPayloads, node, i+1)...)
		}
	}
	files = append(files, collectFiles(payloads, title)...)
	files = uniqueZLFiles(files)
	if !ctx.onlyFiles && len(videos) == 0 {
		videos = append(videos, collectDirectVideos(payloads, title)...)
	}
	if len(videos) == 0 && len(files) == 0 {
		return nil, fmt.Errorf("zlketang: no videos or course files found for course_id=%s product_id=%s", cid, pid)
	}

	entries := make([]*extractor.MediaInfo, 0, len(videos)+len(files))
	for i, v := range videos {
		if entry, err := ctx.resolveVideo(v, i+1); err == nil {
			entries = append(entries, entry)
		}
	}
	for i, f := range files {
		entries = append(entries, fileEntry(f, i+1))
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("zlketang: discovered nodes but no playable URL resolved")
	}
	return &extractor.MediaInfo{Site: "zlketang", Title: cleanTitle(firstNonEmpty(title, cid, pid)), Entries: entries}, nil
}

func zlketangOnlyFilesMode(quality string) bool {
	mode := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(quality)))
	switch mode {
	case "2", "pdf", "onlypdf", "file", "files", "material", "materials", "courseware", "coursewares", "attachment", "attachments", "资料", "课件":
		return true
	default:
		return false
	}
}

func parseIDs(raw string) (courseID, productID string) {
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		if strings.Contains(u.Fragment, "?") {
			if fq, e := url.ParseQuery(strings.SplitN(u.Fragment, "?", 2)[1]); e == nil {
				for k, vs := range fq {
					for _, v := range vs {
						q.Add(k, v)
					}
				}
			}
		}
		courseID = firstNonEmpty(q.Get("course_id"), q.Get("courseId"), q.Get("zl_play_course_id"), q.Get("zl_commodity_course_id"), q.Get("id"))
		productID = firstNonEmpty(q.Get("product_id"), q.Get("productId"), q.Get("zl_product_id"), q.Get("zl_commodity_product_id"))
	}
	if courseID == "" {
		if m := courseIDRe.FindStringSubmatch(raw); len(m) > 1 {
			courseID = m[1]
		}
	}
	if productID == "" {
		if m := productIDRe.FindStringSubmatch(raw); len(m) > 1 {
			productID = m[1]
		}
	}
	return courseID, productID
}

func headersFromJar(jar http.CookieJar) map[string]string {
	h := map[string]string{"Log-Platform-Type": "pc_web", "Origin": originURL, "Referer": refererURL, "Accept": "application/json, text/plain, */*", "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36 Edg/142.0.0.0"}
	var parts []string
	for _, raw := range []string{refererURL, originURL + "/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			parts = append(parts, ck.Name+"="+ck.Value)
		}
	}
	if len(parts) > 0 {
		h["cookie"] = strings.Join(uniqueStrings(parts), "; ")
		h["Cookie"] = h["cookie"]
	}
	return h
}

func (x *zlContext) loadTopLevelPayloads() ([]any, string) {
	var payloads []any
	if data, err := x.requestJSON(courseListAPI, x.apiParams(nil), personalReferer); err == nil {
		payloads = append(payloads, data)
	}
	if x.pid != "" {
		if data, err := x.requestJSON(goodsDetailV3API, map[string]string{"share_type": "6", "product_id": x.pid}, fmt.Sprintf(commodityRefererTmpl, x.pid)); err == nil {
			payloads = append(payloads, data)
		}
	}
	if x.cid != "" {
		if data, err := x.requestJSON(coursePackageAPI, x.apiParams(map[string]string{"course_id": x.cid}), personalReferer); err == nil {
			payloads = append(payloads, data)
		}
		if data, err := x.requestJSON(shicaoCourseTreeAPI, x.apiParams(map[string]string{"course_id": x.cid}), personalReferer); err == nil {
			payloads = append(payloads, data)
		}
		if data, err := x.requestJSON(courseCatalogAPI, x.apiParams(map[string]string{"subject_id": x.cid}), personalReferer); err == nil {
			payloads = append(payloads, data)
		}
	}
	if data, err := x.requestJSON(orderListAPI, map[string]string{"start": "0"}, "https://www.zlketang.com/personal/index.html?name=3"); err == nil {
		payloads = append(payloads, data)
	}
	title := firstTitle(payloads)
	return payloads, title
}

func (x *zlContext) apiParams(extra map[string]string) map[string]string {
	p := map[string]string{"t": fmt.Sprint(time.Now().UnixMilli()), "platform_type": "web", "devtype": "web", "channel": "web", "from": "web"}
	for k, v := range extra {
		if v != "" {
			p[k] = v
		}
	}
	return p
}

func (x *zlContext) requestJSON(api string, params map[string]string, referer string) (map[string]any, error) {
	body, err := x.c.GetString(urlWithQuery(api, params), x.headersWithReferer(referer))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (x *zlContext) headersWithReferer(referer string) map[string]string {
	h := copyHeaders(x.headers)
	if referer != "" {
		h["Referer"] = referer
	}
	return h
}

func (x *zlContext) buildAvailableNodes(payloads []any) []zlNode {
	seen := map[string]bool{}
	var out []zlNode
	for _, node := range walkMaps(payloads) {
		courseID := firstString(node, "course_id", "courseId", "id")
		subID := firstString(node, "sub_course_id", "subCourseId", "course_id", "courseId", "id")
		productID := firstString(node, "product_id", "productId", "bind_product_id")
		if x.cid != "" && courseID != "" && courseID != x.cid && subID != x.cid {
			continue
		}
		if x.pid != "" && productID != "" && productID != x.pid {
			continue
		}
		if courseID == "" && subID == "" && productID == "" {
			continue
		}
		key := firstNonEmpty(subID, courseID, productID)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, zlNode{CourseID: firstNonEmpty(courseID, x.cid), SubCourseID: firstNonEmpty(subID, courseID, x.cid), SubjectID: firstString(node, "subject_id", "subjectId"), Profession: firstString(node, "profession_id", "professionId"), Year: firstString(node, "year"), TeacherID: firstString(node, "teacher_id", "teacherId", "id"), CourseType: firstString(node, "course_type", "courseType"), Title: firstNonEmpty(firstString(node, "course_name", "courseName", "subject_name", "sub_course_name", "name", "title"), key), Raw: node})
	}
	return out
}

func (x *zlContext) loadNodePayloads(node zlNode) []any {
	var out []any
	candidates := x.nodeParamCandidates(node)
	for _, p := range candidates {
		if data, err := x.requestJSON(courseDetailAPI, x.apiParams(p), personalReferer); err == nil {
			out = append(out, data)
		}
	}
	if node.SubjectID != "" {
		if data, err := x.requestJSON(courseCatalogAPI, x.apiParams(map[string]string{"subject_id": node.SubjectID, "profession_id": node.Profession}), personalReferer); err == nil {
			out = append(out, data)
		}
	}
	return out
}

func (x *zlContext) nodeParamCandidates(node zlNode) []map[string]string {
	base := map[string]string{"course_id": firstNonEmpty(node.CourseID, x.cid), "sub_course_id": node.SubCourseID, "year": node.Year, "teacher_id": node.TeacherID, "subject_id": node.SubjectID}
	var out []map[string]string
	seen := map[string]bool{}
	for _, keys := range [][]string{{"course_id", "sub_course_id", "year", "teacher_id"}, {"course_id", "year", "teacher_id"}, {"sub_course_id", "year", "teacher_id"}, {"course_id"}, {"sub_course_id"}} {
		p := map[string]string{}
		for _, k := range keys {
			if base[k] != "" {
				p[k] = base[k]
			}
		}
		key := fmt.Sprint(p)
		if len(p) > 0 && !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}

func (x *zlContext) getVideoData(node zlNode) []any {
	var out []any
	for _, api := range []string{courseVideoAPI, freeCourseVideoAPI, practiceCourseVideoAPI} {
		for _, p := range x.nodeParamCandidates(node) {
			if node.CourseType != "" {
				p["course_type"] = node.CourseType
			}
			data, err := x.requestJSON(api, x.apiParams(p), personalReferer)
			if err == nil && len(data) > 0 {
				out = append(out, data)
			}
		}
		if !shicaoTypes[node.CourseType] && api == courseVideoAPI {
			// Source tries the paid API first, then special/free APIs when tree data indicates that path.
			continue
		}
	}
	return out
}

func parseVideoList(payloads []any, node zlNode, chapterIndex int) []zlVideo {
	seen := map[string]bool{}
	var out []zlVideo
	for _, m := range walkMaps(payloads) {
		itemType := firstString(m, "item_type", "itemType", "type")
		sectionID := firstString(m, "course_section_id", "courseSectionId", "item_id", "itemId", "section_id", "id")
		liveID := firstString(m, "live_id", "liveId", "play_live_id")
		videoID := firstString(m, "video_id", "videoId", "txvid", "alivid", "fileId")
		direct := firstMediaURL(m)
		if sectionID == "" && liveID == "" && videoID == "" && direct == "" {
			continue
		}
		key := firstNonEmpty(sectionID, liveID, videoID, direct)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		title := firstNonEmpty(firstString(m, "item_name", "itemName", "dir_name", "name", "title"), node.Title, "untitled_video")
		out = append(out, zlVideo{Title: cleanTitle(fmt.Sprintf("[%d.%d]--%s", chapterIndex, len(out)+1, title)), SectionID: sectionID, CourseID: firstNonEmpty(firstString(m, "course_id", "courseId"), node.CourseID), LiveID: liveID, VideoID: videoID, ItemType: itemType, PlayAuth: m, SourceURL: direct, CourseSectionID: sectionID})
	}
	return out
}

func collectDirectVideos(payloads []any, title string) []zlVideo {
	seen := map[string]bool{}
	var out []zlVideo
	for _, p := range payloads {
		for _, u := range extractMediaURLs(p) {
			if !seen[u] && (strings.Contains(strings.ToLower(u), ".m3u8") || strings.Contains(strings.ToLower(u), ".mp4")) {
				seen[u] = true
				out = append(out, zlVideo{Title: cleanTitle(fmt.Sprintf("[%02d]--%s", len(out)+1, firstNonEmpty(title, "video"))), SourceURL: u, SourceType: pickFormat(u)})
			}
		}
	}
	return out
}

func (x *zlContext) resolveVideo(v zlVideo, index int) (*extractor.MediaInfo, error) {
	playURL := normalizeMediaURL(v.SourceURL, nil)
	format := pickFormat(playURL)
	extra := map[string]any{"course_section_id": v.CourseSectionID, "live_id": v.LiveID, "video_id": v.VideoID, "item_type": v.ItemType}
	if playURL == "" {
		var data map[string]any
		if v.LiveID != "" || v.ItemType == "2" {
			liveData := x.getLiveDetail(v.LiveID)
			data = x.pickLivePlayAuth(liveData)
			if direct := normalizeMediaURL(firstString(data, "url", "video_url", "m3u8_url"), nil); direct != "" {
				playURL = direct
			}
		} else {
			data = x.getVideoPlayData(v.SectionID, v.CourseID)
			if direct := normalizeMediaURL(firstString(data, "url", "video_url", "m3u8_url", "master_m3u8_url", "hls"), data); direct != "" {
				playURL = direct
			}
		}
		if playURL == "" && len(data) > 0 {
			if source := x.requestQCloudPlayInfo(data, v); source.URL != "" {
				playURL = source.URL
				for k, val := range source.Extra {
					extra[k] = val
				}
			}
		}
		for k, val := range data {
			extra[k] = val
		}
	}
	if playURL == "" && v.VideoID != "" {
		if source := x.requestQCloudPlayInfo(v.PlayAuth, v); source.URL != "" {
			playURL = source.URL
			for k, val := range source.Extra {
				extra[k] = val
			}
		}
	}
	if playURL == "" {
		return nil, fmt.Errorf("zlketang: no playable source for %s", firstNonEmpty(v.SectionID, v.LiveID, v.VideoID))
	}
	format = pickFormat(playURL)
	return &extractor.MediaInfo{Site: "zlketang", Title: cleanTitle(firstNonEmpty(v.Title, fmt.Sprintf("[%02d]--video", index))), Streams: map[string]extractor.Stream{"default": {Quality: "best", URLs: []string{playURL}, Format: format, NeedMerge: format == "m3u8", Headers: map[string]string{"Referer": refererURL, "User-Agent": x.headers["User-Agent"]}}}, Extra: extra}, nil
}

func (x *zlContext) getVideoPlayData(courseSectionID, courseID string) map[string]any {
	if courseSectionID == "" && courseID == "" {
		return nil
	}
	p := x.apiParams(map[string]string{"course_section_id": courseSectionID, "course_id": courseID, "play_course_section_id": courseSectionID})
	resp, err := x.requestJSON(videoPlayAPI, p, personalReferer)
	if err != nil || fmt.Sprint(resp["errcode"]) != "0" {
		return nil
	}
	data := asMap(resp["data"])
	decoded := map[string]any{}
	if v := decodeSignedData(data["domains"], data["domains_sign"], playAESKey); v != nil {
		decoded["domains"] = v
	}
	if v := decodeSignedData(data["play_auth"], data["play_auth_sign"], playAESKey); v != nil {
		for k, val := range asMap(v) {
			decoded[k] = val
		}
		decoded["play_auth"] = v
	}
	if v := decodeSignedData(data["hls"], data["hls_sign"], playAESKey); v != nil {
		decoded["hls"] = v
		if u := normalizeMediaURL(fmt.Sprint(v), map[string]any{"domains": decoded["domains"]}); u != "" {
			decoded["m3u8_url"] = u
		}
	}
	for k, val := range data {
		if _, ok := decoded[k]; !ok {
			decoded[k] = val
		}
	}
	return decoded
}

func (x *zlContext) getLiveDetail(liveID string) map[string]any {
	if liveID == "" {
		return nil
	}
	resp, err := x.requestJSON(liveDetailAPI, x.apiParams(map[string]string{"live_id": liveID, "play_live_id": liveID}), fmt.Sprintf(livePlayRefererTmpl, liveID))
	if err != nil || fmt.Sprint(resp["errcode"]) != "0" {
		return nil
	}
	data := asMap(resp["data"])
	for k, v := range data {
		if strings.HasSuffix(k, "_sign") {
			continue
		}
		sign := data[k+"_sign"]
		if sign != nil {
			if dec := decodeSignedData(v, sign, liveAESKey); dec != nil {
				data[k] = dec
			}
		}
	}
	return data
}

func (x *zlContext) pickLivePlayAuth(liveData map[string]any) map[string]any {
	if liveData == nil {
		return nil
	}
	for _, item := range extractItems(firstNonNil(liveData["vod_urls"], liveData["vodUrls"], liveData["urls"])) {
		pt := firstString(item, "play_type", "playType")
		if pt == "3" || pt == "4" || pt == "" {
			return item
		}
	}
	return liveData
}

func decodeSignedData(value any, sign any, aesKey string) any {
	if value == nil {
		return nil
	}
	signInt, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(sign)))
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return nil
	}
	if signInt != 0 && isHex(text) {
		if dec := decryptHexText(text, aesKey); dec != "" {
			text = dec
		}
	}
	return safeJSONLoads(text, value)
}

func decryptHexText(encryptedHex, aesKey string) string {
	b, err := hex.DecodeString(strings.TrimSpace(encryptedHex))
	if err != nil || len(b) == 0 {
		return ""
	}
	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil || len(b)%block.BlockSize() != 0 {
		return ""
	}
	out := make([]byte, len(b))
	for bs := 0; bs < len(b); bs += block.BlockSize() {
		block.Decrypt(out[bs:bs+block.BlockSize()], b[bs:bs+block.BlockSize()])
	}
	out = pkcs7Unpad(out, block.BlockSize())
	return string(out)
}

func pkcs7Unpad(data []byte, blockSize int) []byte {
	if len(data) == 0 {
		return data
	}
	pad := int(data[len(data)-1])
	if pad < 1 || pad > blockSize || pad > len(data) || !bytes.Equal(data[len(data)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return data
	}
	return data[:len(data)-pad]
}

func safeJSONLoads(text string, fallback any) any {
	text = strings.TrimSpace(strings.ReplaceAll(text, `\/`, `/`))
	if text == "" {
		return fallback
	}
	var out any
	if err := json.Unmarshal([]byte(text), &out); err == nil {
		return out
	}
	return fallback
}

func normalizeMediaURL(value string, context map[string]any) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\/`, `/`))
	if value == "" || value == "<nil>" {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if context != nil {
		if domain := pickDomain(context["domains"]); domain != "" {
			return strings.TrimRight(domain, "/") + "/" + strings.TrimLeft(value, "/")
		}
	}
	return value
}

func pickDomain(v any) string {
	switch t := safeJSONLoads(fmt.Sprint(v), v).(type) {
	case string:
		return normalizeMediaURL(t, nil)
	case []any:
		for _, it := range t {
			if d := pickDomain(it); d != "" {
				return d
			}
		}
	case map[string]any:
		for _, k := range []string{"domain", "url", "host"} {
			if d := normalizeMediaURL(fmt.Sprint(t[k]), nil); d != "" && d != "<nil>" {
				return d
			}
		}
	}
	return ""
}

func (x *zlContext) requestQCloudPlayInfo(playAuth map[string]any, v zlVideo) zlPlaySource {
	appID := firstNonEmpty(firstString(playAuth, "app_id", "appId"), firstString(v.PlayAuth, "app_id", "appId"))
	videoID := firstNonEmpty(firstString(playAuth, "txvid", "video_id", "videoId", "fileId"), v.VideoID)
	if appID == "" || videoID == "" {
		return zlPlaySource{}
	}
	params := map[string]string{"psign": firstString(playAuth, "p_sign", "psign"), "keyId": "1"}
	overlayKey, overlayIV := genOverlay(), genOverlay()
	if overlayKey != "" {
		params["cipheredOverlayKey"] = rsaEncryptOverlay(overlayKey)
	}
	if overlayIV != "" {
		params["cipheredOverlayIv"] = rsaEncryptOverlay(overlayIV)
	}
	api := fmt.Sprintf(qcloudPlayAPITmpl, url.PathEscape(appID), url.PathEscape(videoID))
	resp, err := x.requestJSON(api, params, refererURL)
	if err != nil {
		return zlPlaySource{}
	}
	info := parseZLQCloudPlayInfo(resp, overlayKey, overlayIV)
	extra := map[string]any{"source_type": "qcloud", "qcloud_app_id": appID, "qcloud_file_id": videoID}
	if info.DRMToken != "" {
		extra["drm_token"] = info.DRMToken
	}
	if info.MasterURL != "" {
		finalURL, text := x.loadFinalQCloudM3U8(info)
		if text != "" {
			extra["source_type"] = "m3u8_text"
			extra["m3u8_text"] = text
			extra["m3u8_url"] = firstNonEmpty(finalURL, info.MasterURL)
			return zlPlaySource{URL: zlM3U8DataURL(text), Extra: extra}
		}
		if finalURL != "" {
			return zlPlaySource{URL: finalURL, Extra: extra}
		}
		return zlPlaySource{URL: info.MasterURL, Extra: extra}
	}
	for _, u := range extractMediaURLs(resp) {
		if strings.Contains(strings.ToLower(u), ".m3u8") || strings.Contains(strings.ToLower(u), ".mp4") {
			if strings.Contains(strings.ToLower(u), ".m3u8") {
				if finalURL, text := x.loadFinalQCloudM3U8(zlQCloudPlayInfo{MasterURL: u, DRMToken: info.DRMToken, OverlayKey: overlayKey, OverlayIV: overlayIV}); text != "" {
					extra["source_type"] = "m3u8_text"
					extra["m3u8_text"] = text
					extra["m3u8_url"] = firstNonEmpty(finalURL, u)
					return zlPlaySource{URL: zlM3U8DataURL(text), Extra: extra}
				}
			}
			return zlPlaySource{URL: u, Extra: extra}
		}
	}
	return zlPlaySource{}
}

func genOverlay() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func rsaEncryptOverlay(text string) string {
	block, _ := pem.Decode([]byte(rsaPublicKeyPEM))
	if block == nil {
		return ""
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return ""
	}
	pub, ok := key.(*rsa.PublicKey)
	if !ok {
		return ""
	}
	enc, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(text))
	if err != nil {
		return ""
	}
	return hex.EncodeToString(enc)
}

type zlQCloudPlayInfo struct {
	MasterURL  string
	DRMToken   string
	OverlayKey string
	OverlayIV  string
}

func parseZLQCloudPlayInfo(root map[string]any, overlayKey, overlayIV string) zlQCloudPlayInfo {
	info := zlQCloudPlayInfo{OverlayKey: overlayKey, OverlayIV: overlayIV}
	for _, m := range walkMaps(root) {
		if info.MasterURL == "" {
			if ml := asMap(m["masterPlayList"]); len(ml) > 0 {
				info.MasterURL = normalizeMediaURL(firstString(ml, "url", "playUrl", "playURL"), nil)
			}
		}
		if info.MasterURL == "" {
			info.MasterURL = normalizeMediaURL(firstString(m, "url", "playUrl", "playURL", "m3u8_url", "master_m3u8_url", "hls"), m)
		}
		if info.DRMToken == "" {
			info.DRMToken = firstString(m, "drmToken", "drm_token")
		}
	}
	return info
}

func (x *zlContext) loadFinalQCloudM3U8(info zlQCloudPlayInfo) (string, string) {
	if info.MasterURL == "" || !strings.Contains(strings.ToLower(info.MasterURL), ".m3u8") {
		return info.MasterURL, ""
	}
	h := map[string]string{"Referer": refererURL, "User-Agent": x.headers["User-Agent"]}
	body, err := x.c.GetString(info.MasterURL, h)
	if err != nil || !strings.Contains(body, "#EXTM3U") {
		return info.MasterURL, ""
	}
	variantURL := info.MasterURL
	text := body
	if strings.Contains(body, "#EXT-X-STREAM-INF") {
		variantURL = selectZLQCloudVariant(body, info.MasterURL)
		if variantURL != info.MasterURL {
			if b, err := x.c.GetString(variantURL, h); err == nil && strings.Contains(b, "#EXTM3U") {
				text = b
			}
		}
	}
	return variantURL, x.rewriteZLQCloudM3U8(text, variantURL, info)
}

func selectZLQCloudVariant(masterText, masterURL string) string {
	type candidate struct {
		bw  int
		url string
	}
	var candidates []candidate
	lines := strings.Split(strings.ReplaceAll(masterText, "\r\n", "\n"), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			continue
		}
		bw := 0
		if m := regexp.MustCompile(`BANDWIDTH=(\d+)`).FindStringSubmatch(line); len(m) > 1 {
			bw, _ = strconv.Atoi(m[1])
		}
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" || strings.HasPrefix(next, "#") {
				continue
			}
			candidates = append(candidates, candidate{bw: bw, url: joinZLURL(masterURL, next)})
			break
		}
	}
	if len(candidates) == 0 {
		return masterURL
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].bw > candidates[j].bw })
	return candidates[0].url
}

func (x *zlContext) rewriteZLQCloudM3U8(text, baseURL string, info zlQCloudPlayInfo) string {
	if !strings.Contains(text, "#EXTM3U") {
		return ""
	}
	uriRe := regexp.MustCompile(`URI="([^"]+)"`)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, "URI=") {
				lines[i] = uriRe.ReplaceAllStringFunc(line, func(match string) string {
					m := uriRe.FindStringSubmatch(match)
					if len(m) < 2 {
						return match
					}
					u := joinZLURL(baseURL, m[1])
					if strings.Contains(trimmed, "#EXT-X-KEY") {
						u = appendZLToken(u, info.DRMToken)
						if key := x.qcloudKeyDataURL(u, baseURL, info); key != "" {
							u = key
						}
					}
					return `URI="` + u + `"`
				})
			}
			continue
		}
		lines[i] = joinZLURL(baseURL, trimmed)
	}
	return strings.Join(lines, "\n")
}

func (x *zlContext) qcloudKeyDataURL(keyURL, referer string, info zlQCloudPlayInfo) string {
	key, iv, ok := overlayMaterial(info.OverlayKey, info.OverlayIV)
	if !ok {
		return ""
	}
	h := map[string]string{"Referer": referer, "User-Agent": x.headers["User-Agent"]}
	raw, err := x.c.GetBytes(keyURL, h)
	if err != nil || len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return ""
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, raw)
	if len(plain) != aes.BlockSize {
		if unpadded := maybePKCS7Key(plain); len(unpadded) == aes.BlockSize {
			plain = unpadded
		} else {
			return ""
		}
	}
	return "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(plain)
}

func overlayMaterial(keyHex, ivHex string) ([]byte, []byte, bool) {
	key, err := hex.DecodeString(strings.TrimSpace(keyHex))
	if err != nil {
		return nil, nil, false
	}
	iv, err := hex.DecodeString(strings.TrimSpace(ivHex))
	if err != nil || len(iv) != aes.BlockSize {
		return nil, nil, false
	}
	switch len(key) {
	case 16, 24, 32:
		return key, iv, true
	default:
		return nil, nil, false
	}
}

func maybePKCS7Key(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(data) {
		return nil
	}
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return nil
		}
	}
	return data[:len(data)-padding]
}

func appendZLToken(rawURL, token string) string {
	rawURL = strings.TrimSpace(rawURL)
	token = strings.TrimSpace(token)
	if rawURL == "" || token == "" || strings.Contains(rawURL, "token=") {
		return rawURL
	}
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + "token=" + url.QueryEscape(token)
}

func joinZLURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "data:") {
		return ref
	}
	if strings.HasPrefix(ref, "//") {
		return "https:" + ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func zlM3U8DataURL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}

func collectFiles(payloads []any, parentTitle string) []zlFile {
	seen := map[string]bool{}
	var out []zlFile
	for _, node := range walkMaps(payloads) {
		name := firstNonEmpty(firstString(node, "file_name", "fileName", "name", "title", "item_name", "dir_name"), parentTitle, "资料")
		for _, k := range fileURLKeys {
			u := strings.TrimSpace(fmt.Sprint(node[k]))
			if u == "" || u == "<nil>" || seen[u] {
				continue
			}
			if !strings.HasPrefix(u, "http") && !strings.HasPrefix(u, "//") {
				continue
			}
			if strings.Contains(strings.ToLower(u), ".m3u8") || strings.Contains(strings.ToLower(u), ".mp4") || strings.Contains(strings.ToLower(u), ".mp3") {
				continue
			}
			seen[u] = true
			out = append(out, zlFile{Title: cleanTitle(name), URL: normalizeMediaURL(u, nil), Format: pickFormat(u)})
		}
	}
	for _, p := range payloads {
		for _, u := range extractMediaURLs(p) {
			if !seen[u] && !strings.Contains(strings.ToLower(u), ".m3u8") && !strings.Contains(strings.ToLower(u), ".mp4") {
				seen[u] = true
				out = append(out, zlFile{Title: firstNonEmpty(parentTitle, "资料"), URL: u, Format: pickFormat(u)})
			}
		}
	}
	return out
}

func fileEntry(f zlFile, index int) *extractor.MediaInfo {
	return &extractor.MediaInfo{Site: "zlketang", Title: cleanTitle(firstNonEmpty(f.Title, fmt.Sprintf("[%02d]--资料", index))), Streams: map[string]extractor.Stream{"default": {Quality: "source", URLs: []string{f.URL}, Format: f.Format, Headers: map[string]string{"Referer": refererURL}}}}
}

func uniqueZLFiles(files []zlFile) []zlFile {
	seen := map[string]bool{}
	out := make([]zlFile, 0, len(files))
	for _, f := range files {
		if f.URL == "" || seen[f.URL] {
			continue
		}
		seen[f.URL] = true
		out = append(out, f)
	}
	return out
}

func firstMediaURL(m map[string]any) string {
	for _, k := range []string{"url", "video_url", "videoUrl", "m3u8_url", "master_m3u8_url", "source", "play_url"} {
		if u := normalizeMediaURL(fmt.Sprint(m[k]), m); strings.HasPrefix(u, "http") {
			return u
		}
	}
	return ""
}

func extractMediaURLs(v any) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			for _, m := range mediaURLRe.FindAllString(strings.ReplaceAll(t, `\/`, `/`), -1) {
				if !seen[m] {
					seen[m] = true
					out = append(out, m)
				}
			}
		case []any:
			for _, it := range t {
				walk(it)
			}
		case map[string]any:
			for _, val := range t {
				walk(val)
			}
		}
	}
	walk(v)
	return out
}

func extractItems(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m := asMap(it); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	}
	if s, ok := v.(string); ok {
		parsed := safeJSONLoads(s, nil)
		if parsed != nil && parsed != v {
			return extractItems(parsed)
		}
	}
	m := asMap(v)
	if len(m) == 0 {
		return nil
	}
	for _, k := range []string{"data", "list", "options", "children", "courseList", "course_list", "vod_urls"} {
		if out := extractItems(m[k]); len(out) > 0 {
			return out
		}
	}
	if len(m) > 0 {
		return []map[string]any{m}
	}
	return nil
}

func walkMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for _, it := range t {
				walk(it)
			}
		case map[string]any:
			out = append(out, t)
			for _, val := range t {
				walk(val)
			}
		}
	}
	walk(v)
	return out
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
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

func firstTitle(payloads []any) string {
	for _, node := range walkMaps(payloads) {
		if t := firstString(node, "course_name", "courseName", "subject_name", "product_name", "name", "title"); t != "" {
			return t
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

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func copyHeaders(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
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

func urlWithQuery(base string, params map[string]string) string {
	u, _ := url.Parse(base)
	q := u.Query()
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if params[k] != "" {
			q.Set(k, params[k])
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }

func pickFormat(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, ".m3u8"):
		return "m3u8"
	case strings.Contains(lower, ".mp4"):
		return "mp4"
	case strings.Contains(lower, ".mp3"):
		return "mp3"
	case strings.Contains(lower, ".pdf"):
		return "pdf"
	case strings.Contains(lower, ".ppt"):
		return "ppt"
	case strings.Contains(lower, ".doc"):
		return "doc"
	case strings.Contains(lower, ".xls"):
		return "xls"
	case strings.Contains(lower, ".zip"):
		return "zip"
	case strings.Contains(lower, ".rar"):
		return "rar"
	}
	return "m3u8"
}

func isHex(s string) bool {
	if len(s)%2 != 0 || s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
