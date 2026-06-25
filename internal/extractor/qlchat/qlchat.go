// Package qlchat implements source-aligned extractors for qlchat.com / qianliao.net courses.
package qlchat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	referer                 = "https://m.qlchat.com"
	trainReferer            = "https://m.qianliao.net"
	course_list_url         = "https://m.qlchat.com/api/wechat/transfer/h5/live/purchaseCourse"
	learn_list_url          = "https://m.qlchat.com/api/wechat/transfer/h5/topic/listRecentLearn"
	info_url                = "https://m.qlchat.com/api/wechat/transfer/h5/interact/getCourseList"
	video_url               = "https://m.qlchat.com/api/wechat/topic/getMediaActualUrl"
	h5_video_url            = "https://m.qlchat.com/api/wechat/topic/media-url?topicId=%s"
	topic_intro_url         = "https://m.qlchat.com/wechat/page/topic-intro?topicId=%s"
	topic_url               = "https://m.qlchat.com/wechat/page/topic-simple-video?topicId=%s"
	audio_url               = "https://m.qlchat.com/api/wechat/topic/media-url?topicId=%s"
	live_url                = "https://m.qlchat.com/api/wechat/topic/getLivePlayUrl"
	video_list_url          = "https://m.qlchat.com/api/wechat/topic/getTopicSpeak"
	doc_list_url            = "https://m.qlchat.com/api/wechat/topic/getTopicSpeak"
	doc_file_url            = "https://m.qlchat.com/api/wechat/topic/doc-get?docId=%s"
	doc_auth_url            = "https://m.qlchat.com/api/wechat/topic/doc-auth?amount=0&docId=%s"
	audio_list_url          = "https://m.qlchat.com/api/wechat/topic/total-speak-list"
	speak_list_url          = "https://m.qlchat.com/api/wechat/topic/getForumSpeak"
	ppt_list_url            = "https://m.qlchat.com/api/wechat/topic/pptList"
	article_url             = "https://m.qlchat.com/api/wechat/transfer/h5/article/get"
	price_url               = "https://m.qlchat.com/api/wechat/transfer/h5/channel/getDiscountType?channelId=%s"
	purchased_url           = "https://m.qlchat.com/api/wechat/channel/initIntro"
	channel_auth_url        = "https://m.qlchat.com/api/wechat/transfer/baseApi/h5/channel/batchChannelAuth"
	topic_auth_url          = "https://m.qlchat.com/api/wechat/topic/auth?topicId=%s"
	topic_auth_transfer_url = "https://m.qlchat.com/api/wechat/transfer/h5/topic/topicAuth"
	join_free_course_url    = "https://m.qlchat.com/api/wechat/transfer/h5/topic/joinFreeCourse"
	train_user_info_url     = "https://m.qianliao.net/financial/api/transfer?url=/gate/user/getUserInfoById"
	train_course_list_url   = "https://m.qianliao.net/financial/api/transfer?url=/gate/course/myCourseList"
	train_camp_url          = "https://m.qianliao.net/financial/api/transfer?url=/gate/learningCalendar/campData"
	train_period_url        = "https://m.qianliao.net/financial/open-course?periodId=%s"
	train_info_url          = "https://m.qianliao.net/financial/api/transfer?url=%s"
	train_video_url         = "https://m.qianliao.net/financial/listen-video?topicId=%s&campId=%s"
	train_audio_url         = "https://m.qianliao.net/financial/listen-audio?topicId=%s&campId=%s"
	train_course_live_url   = "https://m.qianliao.net/financial/tc-live-course?topicId=%s&campId=%s&roomId=%s"
	train_live_url          = "https://m.qianliao.net/financial/tc-public-live?topicId=%s&periodId=%s&roomId=%s"
	train_m3u8_url          = "https://playvideo.qcloud.com/getplayinfo/v4/%s/%s?psign=%s"
	train_order_url         = "https://m.qianliao.net/financial/api/transfer?url=/gate/order/getOrderListDerail"
)

var patterns = []string{
	`(?:[\w-]+\.)?(?:qlchat|qianliaoknow)\.com/`,
	`(?:[\w-]+\.)?(?:qianliao\.net|qianliao\.tv|xingqudao\.cn|xingqudao\.net|nicegoods\.cn)/`,
}

func init() {
	extractor.Register(&Qlchat{}, extractor.SiteInfo{Name: "Qlchat", URL: "qlchat.com", NeedAuth: true})
}

type Qlchat struct{}

func (q *Qlchat) Patterns() []string { return patterns }

func (q *Qlchat) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("qlchat requires login cookies")
	}
	if isQianliaoTrainURL(rawURL) {
		return extractQianliaoTrain(rawURL, opts)
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := map[string]string{"Referer": referer, "Accept": "application/json, text/plain, */*"}
	state := parseInput(rawURL)
	if state.topicID != "" {
		loadTopicIntro(c, h, &state)
	}
	if state.cid == "" {
		state.cid = first(match1(rawURL, `channelId=(\d+)`), match1(rawURL, `/channelPage/(\d+)\.htm`))
	}
	courses, _ := fetchCourseList(c, h)
	for _, it := range courses {
		if it.CourseID == state.cid || (state.cid == "" && it.CourseID != "") {
			state.cid = it.CourseID
			state.liveID = first(state.liveID, it.LiveID)
			state.title = first(state.title, it.Title)
			break
		}
	}
	if state.title == "" && state.cid != "" {
		body, _ := c.GetString(fmt.Sprintf("https://m.qlchat.com/live/channel/channelPage/%s.htm", url.QueryEscape(state.cid)), h)
		state.title = sanitize(first(match1(body, `"name"\s*:\s*"(.+?)"`), state.title))
		state.liveID = first(state.liveID, match1(body, `"liveId"\s*:\s*"(.+?)"`))
	}
	if state.cid == "" && state.topicID == "" {
		return nil, fmt.Errorf("cannot parse qlchat channelId/topicId from URL")
	}
	if state.cid != "" {
		_, _ = c.GetString(fmt.Sprintf(price_url, url.QueryEscape(state.cid)), h)
		_, _ = postJSON(c, purchased_url, map[string]any{"liveId": state.liveID, "channelId": state.cid}, h)
	}
	if state.single || (state.cid == "" && state.topicID != "") {
		id := first(state.topicID, state.cid)
		entry, err := resolveTopic(c, h, id, first(state.title, "qlchat_"+id))
		if err != nil {
			return nil, err
		}
		return entry, nil
	}
	items, err := fetchCourseItems(c, h, state.cid, state.liveID)
	if err != nil {
		return nil, err
	}
	var entries []*extractor.MediaInfo
	topicIdx := 0
	fileIdx := 0
	for _, it := range items {
		id := jstr(it.BusinessID)
		if id == "" {
			continue
		}
		switch it.BusinessType {
		case "topic":
			topicIdx++
			name := sanitize(fmt.Sprintf("[%d]--%s", topicIdx, first(it.BusinessName, id)))
			entry, err := resolveTopic(c, h, id, name)
			if err == nil && entry != nil {
				entry.Extra = mergeExtra(entry.Extra, map[string]any{"is_auth_topic": it.TopicPo.IsAuthTopic, "audio_assembly_url": it.TopicPo.AudioAssemblyURL})
				entries = append(entries, entry)
			}
			// Surface doc/ppt/article sub-resources for this topic.
			entries = append(entries, resolveTopicDocs(c, h, state.liveID, id, name)...)
			entries = append(entries, resolveTopicPPTs(c, h, id, name)...)
			if article := resolveTopicArticle(c, h, id, name); article != nil {
				entries = append(entries, article)
			}
		case "file":
			fileIdx++
			fileURL := buildFileURL(it.FilePo.URL, it.FilePo.AuthKey)
			if fileURL == "" {
				continue
			}
			fileName := sanitize(fmt.Sprintf("(%d)--%s", fileIdx, first(it.BusinessName, id)))
			entries = append(entries, &extractor.MediaInfo{
				Site:  "qlchat",
				Title: fileName,
				Streams: map[string]extractor.Stream{"best": {
					Quality: "best",
					URLs:    []string{fileURL},
					Format:  pickFormat(fileURL),
					Headers: map[string]string{"Referer": referer},
				}},
				Extra: map[string]any{"file_id": id, "file_type": it.FilePo.Type, "business_type": "file"},
			})
		}
	}
	if len(entries) == 0 && state.topicID != "" {
		entry, err := resolveTopic(c, h, state.topicID, first(state.title, "qlchat_"+state.topicID))
		if err == nil && entry != nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("qlchat: no downloadable media found from getCourseList")
	}
	return &extractor.MediaInfo{Site: "qlchat", Title: sanitize(first(state.title, "qlchat_"+state.cid)), Entries: entries}, nil
}

type qState struct {
	cid, topicID, liveID, title string
	single                      bool
}
type qCourse struct{ CourseID, LiveID, Title string }
type qBusiness struct {
	BusinessType string `json:"businessType"`
	BusinessName string `json:"businessName"`
	BusinessID   any    `json:"businessId"`
	TopicPo      struct {
		AudioAssemblyURL string `json:"audioAssemblyUrl"`
		IsAuthTopic      any    `json:"isAuthTopic"`
		Type             string `json:"type"`
	} `json:"topicPo"`
	FilePo struct {
		URL     string `json:"url"`
		AuthKey string `json:"authKey"`
		Type    string `json:"type"`
	} `json:"filePo"`
}
type qVideo struct {
	Height, Width int
	PlayURL       string `json:"playUrl"`
	Type          string `json:"type"`
}

func parseInput(raw string) qState {
	return qState{cid: first(match1(raw, `channelId=(\d+)`), match1(raw, `/channelPage/(\d+)\.htm`)), topicID: first(match1(raw, `topicId=(\d+)`), match1(raw, `/topic/(\d+)`))}
}
func loadTopicIntro(c *util.Client, h map[string]string, st *qState) {
	body, err := c.GetString(fmt.Sprintf(topic_intro_url, url.QueryEscape(st.topicID)), h)
	if err != nil {
		return
	}
	st.cid = first(st.cid, match1(body, `"channelId"\s*:\s*"?(\d+)"?`))
	if st.cid == "" {
		st.single = true
		st.cid = first(match1(body, `"sourceTopicId"\s*:\s*"?(\d+)"?`), st.topicID)
	}
	st.title = sanitize(first(st.title, match1(body, `"channelName"\s*:\s*"(.*?)"`), match1(body, `"topic"\s*:\s*"(.*?)"`)))
	st.liveID = first(st.liveID, match1(body, `"liveId"\s*:\s*"?(.*?)"?[,}]`))
}
func fetchCourseList(c *util.Client, h map[string]string) ([]qCourse, error) {
	var out []qCourse
	for page := 1; page < 100; page++ {
		var resp struct {
			Data struct {
				List []struct {
					CourseType  string `json:"courseType"`
					BussinessID any    `json:"bussinessId"`
					SkuTitle    string `json:"skuTitle"`
					LiveID      any    `json:"liveId"`
				} `json:"list"`
			} `json:"data"`
		}
		if err := postJSONInto(c, course_list_url, map[string]any{"page": map[string]any{"page": page, "size": 9999}, "pageSize": 9999, "type": "all", "purchaseTypeList": "all"}, h, &resp); err != nil || len(resp.Data.List) == 0 {
			break
		}
		for _, it := range resp.Data.List {
			if it.CourseType == "channel" {
				out = append(out, qCourse{CourseID: jstr(it.BussinessID), LiveID: jstr(it.LiveID), Title: it.SkuTitle})
			}
		}
	}
	var learn struct {
		Data struct {
			LearningList []struct {
				CourseType  string `json:"courseType"`
				BussinessID any    `json:"bussinessId"`
				SkuTitle    string `json:"skuTitle"`
				LiveID      any    `json:"liveId"`
			} `json:"learningList"`
		} `json:"data"`
	}
	if postJSONInto(c, learn_list_url, map[string]any{"page": map[string]any{"page": 1, "size": 9999}, "pageSize": 9999, "type": "all", "beforeOrAfter": "before"}, h, &learn) == nil {
		for _, it := range learn.Data.LearningList {
			if it.CourseType == "channel" {
				out = append(out, qCourse{CourseID: jstr(it.BussinessID), LiveID: jstr(it.LiveID), Title: it.SkuTitle})
			}
		}
	}
	seen, dedup := map[string]bool{}, out[:0]
	for _, it := range out {
		if it.CourseID != "" && !seen[it.CourseID] {
			seen[it.CourseID] = true
			dedup = append(dedup, it)
		}
	}
	return dedup, nil
}
func fetchCourseItems(c *util.Client, h map[string]string, cid, liveID string) ([]qBusiness, error) {
	var out []qBusiness
	for page := 1; page < 100; page++ {
		var resp struct {
			Data struct {
				DataList []qBusiness `json:"dataList"`
			} `json:"data"`
		}
		payload := map[string]any{"page": map[string]any{"page": page, "size": 9999}, "liveId": liveID, "sort": "asc", "channelId": cid}
		if err := postJSONInto(c, info_url, payload, h, &resp); err != nil {
			return out, err
		}
		if len(resp.Data.DataList) == 0 {
			break
		}
		out = append(out, resp.Data.DataList...)
	}
	return out, nil
}
func resolveTopic(c *util.Client, h map[string]string, topicID, title string) (*extractor.MediaInfo, error) {
	play := first(resolveVideo(c, h, topicID, false), resolveVideo(c, h, topicID, true), resolveAudio(c, h, topicID))
	if play == "" {
		return nil, fmt.Errorf("qlchat: empty playUrl for topicId %s", topicID)
	}
	return &extractor.MediaInfo{Site: "qlchat", Title: sanitize(title), Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{play}, Format: pickFormat(play), Headers: map[string]string{"Referer": referer}}}, Extra: map[string]any{"topic_id": topicID}}, nil
}
func resolveVideo(c *util.Client, h map[string]string, topicID string, isLive bool) string {
	if isLive {
		var resp struct {
			Data struct {
				List []qVideo `json:"list"`
			} `json:"data"`
		}
		if postJSONInto(c, live_url, map[string]any{"topicId": topicID}, h, &resp) == nil {
			for _, v := range resp.Data.List {
				if v.Type == "END_VOD" && v.PlayURL != "" {
					return v.PlayURL
				}
			}
		}
	}
	sourceID := first(sourceTopicID(c, h, topicID), topicID)
	var resp struct {
		Data struct {
			Video []qVideo `json:"video"`
		} `json:"data"`
	}
	_ = postJSONInto(c, video_url, map[string]any{"topicId": sourceID, "relayTopicId": topicID}, h, &resp)
	if len(resp.Data.Video) == 0 {
		_ = getJSONInto(c, fmt.Sprintf(h5_video_url, url.QueryEscape(topicID)), h, &resp)
	}
	if len(resp.Data.Video) == 0 {
		return ""
	}
	sort.Slice(resp.Data.Video, func(i, j int) bool {
		return resp.Data.Video[i].Height*10000+resp.Data.Video[i].Width > resp.Data.Video[j].Height*10000+resp.Data.Video[j].Width
	})
	return resp.Data.Video[0].PlayURL
}
func resolveAudio(c *util.Client, h map[string]string, topicID string) string {
	var resp struct {
		Data struct {
			Audio struct {
				PlayURL string `json:"playUrl"`
			} `json:"audio"`
		} `json:"data"`
	}
	_ = getJSONInto(c, fmt.Sprintf(audio_url, url.QueryEscape(topicID)), h, &resp)
	return resp.Data.Audio.PlayURL
}
func sourceTopicID(c *util.Client, h map[string]string, id string) string {
	body, _ := c.GetString(fmt.Sprintf(topic_url, url.QueryEscape(id)), h)
	return match1(body, `"sourceTopicId"\s*:\s*"(\d+)"`)
}

// buildFileURL constructs "url?auth_key=authKey" for file-type items.
func buildFileURL(rawURL, authKey string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if authKey = strings.TrimSpace(authKey); authKey != "" {
		return rawURL + "?auth_key=" + authKey
	}
	return rawURL
}

// resolveTopicDocs fetches doc IDs from getTopicSpeak for the given topicID,
// then calls doc-get and doc-auth for each doc to obtain a downloadable URL.
func resolveTopicDocs(c *util.Client, h map[string]string, liveID, topicID, parentName string) []*extractor.MediaInfo {
	docIDs := fetchDocIDList(c, h, liveID, topicID)
	if len(docIDs) == 0 {
		return nil
	}
	var entries []*extractor.MediaInfo
	for i, docID := range docIDs {
		info := fetchDocFileInfo(c, h, docID, i+1)
		if info == nil || info.fileURL == "" {
			continue
		}
		title := sanitize(fmt.Sprintf("%s_文档_%s", parentName, first(info.fileName, docID)))
		entries = append(entries, &extractor.MediaInfo{
			Site:  "qlchat",
			Title: title,
			Streams: map[string]extractor.Stream{"best": {
				Quality: "best",
				URLs:    []string{info.fileURL},
				Format:  first(info.fileType, pickFormat(info.fileURL)),
				Headers: map[string]string{"Referer": referer},
			}},
			Extra: map[string]any{"doc_id": docID, "file_type": info.fileType, "resource_type": "doc"},
		})
	}
	return entries
}

// fetchDocIDList pages through getTopicSpeak looking for type=="doc" speaks.
func fetchDocIDList(c *util.Client, h map[string]string, liveID, topicID string) []string {
	var docIDs []string
	lastTime := 0
	for i := 0; i < 100; i++ {
		var resp struct {
			Data struct {
				LiveSpeakViews []struct {
					Type       string `json:"type"`
					Content    string `json:"content"`
					CreateTime int    `json:"createTime"`
				} `json:"liveSpeakViews"`
			} `json:"data"`
		}
		payload := map[string]any{
			"time":          lastTime,
			"beforeOrAfter": "after",
			"liveId":        liveID,
			"topicId":       topicID,
		}
		if err := postJSONInto(c, doc_list_url, payload, h, &resp); err != nil {
			break
		}
		if len(resp.Data.LiveSpeakViews) == 0 {
			break
		}
		for _, sp := range resp.Data.LiveSpeakViews {
			if sp.Type == "doc" && strings.TrimSpace(sp.Content) != "" {
				docIDs = append(docIDs, strings.TrimSpace(sp.Content))
			}
		}
		last := resp.Data.LiveSpeakViews[len(resp.Data.LiveSpeakViews)-1]
		if last.Type == "end" {
			break
		}
		if last.CreateTime == lastTime {
			break
		}
		lastTime = last.CreateTime
	}
	return docIDs
}

type docFileResult struct {
	fileType string
	fileName string
	fileURL  string
}

// fetchDocFileInfo calls doc-get to retrieve the doc's convertUrl and name,
// then doc-auth to get authKey to form the full download URL.
func fetchDocFileInfo(c *util.Client, h map[string]string, docID string, idx int) *docFileResult {
	// Step 1: GET doc-get to get convertUrl, type, name
	var docResp struct {
		Data struct {
			Type       string `json:"type"`
			ConvertURL string `json:"convertUrl"`
			Name       string `json:"name"`
		} `json:"data"`
	}
	if err := getJSONInto(c, fmt.Sprintf(doc_file_url, url.QueryEscape(docID)), h, &docResp); err != nil {
		return nil
	}
	convertURL := strings.TrimSpace(docResp.Data.ConvertURL)
	if convertURL == "" {
		return nil
	}
	name := sanitize(fmt.Sprintf("(%d)--%s", idx, first(docResp.Data.Name, docID)))

	// Step 2: GET doc-auth to get authKey
	var authResp struct {
		Data struct {
			AuthKey string `json:"authKey"`
		} `json:"data"`
	}
	_ = getJSONInto(c, fmt.Sprintf(doc_auth_url, url.QueryEscape(docID)), h, &authResp)
	authKey := strings.TrimSpace(authResp.Data.AuthKey)

	fileURL := convertURL
	if authKey != "" {
		fileURL = fmt.Sprintf("%s?auth_key=%s", convertURL, authKey)
	}

	return &docFileResult{
		fileType: docResp.Data.Type,
		fileName: name,
		fileURL:  fileURL,
	}
}

// resolveTopicPPTs fetches PPT file URLs from pptList for the given topicID.
func resolveTopicPPTs(c *util.Client, h map[string]string, topicID, parentName string) []*extractor.MediaInfo {
	var resp struct {
		Data struct {
			Files []struct {
				URL string `json:"url"`
			} `json:"files"`
		} `json:"data"`
	}
	payload := map[string]any{"status": "", "topicId": topicID}
	if err := postJSONInto(c, ppt_list_url, payload, h, &resp); err != nil {
		return nil
	}
	if len(resp.Data.Files) == 0 {
		return nil
	}
	var entries []*extractor.MediaInfo
	for i, f := range resp.Data.Files {
		u := strings.TrimSpace(f.URL)
		if u == "" {
			continue
		}
		title := sanitize(fmt.Sprintf("%s_PPT_%d", parentName, i+1))
		entries = append(entries, &extractor.MediaInfo{
			Site:  "qlchat",
			Title: title,
			Streams: map[string]extractor.Stream{"best": {
				Quality: "best",
				URLs:    []string{u},
				Format:  pickFormat(u),
				Headers: map[string]string{"Referer": referer},
			}},
			Extra: map[string]any{"topic_id": topicID, "resource_type": "ppt"},
		})
	}
	return entries
}

// resolveTopicArticle fetches article text via the article API.
// Articles are HTML/text content; we surface the API URL as a download entry.
func resolveTopicArticle(c *util.Client, h map[string]string, topicID, parentName string) *extractor.MediaInfo {
	var resp struct {
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	payload := map[string]any{"topicId": topicID}
	if err := postJSONInto(c, article_url, payload, h, &resp); err != nil {
		return nil
	}
	content := strings.TrimSpace(resp.Data.Content)
	if content == "" {
		return nil
	}
	title := sanitize(fmt.Sprintf("%s_(文章)", parentName))
	return &extractor.MediaInfo{
		Site:  "qlchat",
		Title: title,
		Extra: map[string]any{"topic_id": topicID, "resource_type": "article", "article_content": content},
	}
}

func postJSONInto(c *util.Client, api string, payload any, h map[string]string, out any) error {
	body, err := postJSON(c, api, payload, h)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(body), out)
}
func getJSONInto(c *util.Client, api string, h map[string]string, out any) error {
	body, err := c.GetString(api, h)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(body), out)
}
func postJSON(c *util.Client, api string, payload any, h map[string]string) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hh := map[string]string{"Content-Type": "application/json;charset=UTF-8"}
	for k, v := range h {
		hh[k] = v
	}
	resp, err := c.Post(api, bytes.NewReader(b), hh)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, api)
	}
	rb, err := io.ReadAll(resp.Body)
	return string(rb), err
}
func mergeExtra(base map[string]any, more map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for k, v := range more {
		base[k] = v
	}
	return base
}
func match1(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(html.UnescapeString(m[1]))
	}
	return ""
}
func jstr(v any) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "<nil>" {
		return ""
	}
	return s
}
func first(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func sanitize(s string) string {
	s = html.UnescapeString(strings.TrimSpace(s))
	return regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`).ReplaceAllString(s, "_")
}
func pickFormat(u string) string {
	p := strings.ToLower(strings.SplitN(strings.SplitN(u, "?", 2)[0], "#", 2)[0])
	if i := strings.LastIndex(p, "."); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return "mp4"
}
