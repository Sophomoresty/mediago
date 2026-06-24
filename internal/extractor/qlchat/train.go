package qlchat

import (
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

type trainState struct {
	CID    string
	Title  string
	IsCamp bool
}

type trainCourse struct {
	CourseID string
	Title    string
	IsCamp   bool
}

type trainItem struct {
	TopicID string
	RoomID  string
	Type    string
	Name    string
	PlayURL string
}

func isQianliaoTrainURL(raw string) bool {
	l := strings.ToLower(raw)
	return strings.Contains(l, "qianliao.net/") ||
		strings.Contains(l, "qianliao.tv/") ||
		strings.Contains(l, "xingqudao.cn/") ||
		strings.Contains(l, "xingqudao.net/") ||
		strings.Contains(l, "nicegoods.cn/") ||
		strings.Contains(raw, "#小程序://兴趣岛")
}

func extractQianliaoTrain(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := map[string]string{"Referer": trainReferer, "Accept": "application/json, text/plain, */*"}

	if err := checkQianliaoLogin(c, h); err != nil {
		return nil, err
	}

	st := parseTrainState(rawURL)
	if st.CID == "" {
		if redirected := followTrainURL(c, h, rawURL); redirected != "" && redirected != rawURL {
			st = parseTrainState(redirected)
		}
	}

	courses, listErr := fetchTrainCourseList(c, h)
	if st.CID == "" {
		if listErr != nil {
			return nil, fmt.Errorf("qianliao train course list: %w", listErr)
		}
		if len(courses) == 0 {
			return nil, fmt.Errorf("qianliao train: cannot parse campId/periodId and myCourseList is empty")
		}
		st.CID = courses[0].CourseID
		st.IsCamp = courses[0].IsCamp
		st.Title = courses[0].Title
	}
	for _, co := range courses {
		if co.CourseID == st.CID {
			st.IsCamp = co.IsCamp
			st.Title = first(st.Title, co.Title)
			break
		}
	}

	if title := fetchTrainTitle(c, h, st); title != "" {
		st.Title = title
	}
	items, err := fetchTrainItems(c, h, st)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("qianliao train: no VIDEO/AUDIO/LIVE courses from playListOfCourse")
	}

	entries := make([]*extractor.MediaInfo, 0, len(items))
	var firstErr error
	for i, item := range items {
		mi, err := resolveTrainItem(c, h, st, item, i+1)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if mi != nil {
			entries = append(entries, mi)
		}
	}
	if len(entries) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("qianliao train: no playable media URLs resolved")
	}
	return &extractor.MediaInfo{Site: "qlchat", Title: sanitize(first(st.Title, "qianliao_"+st.CID)), Entries: entries, Extra: map[string]any{"course_id": st.CID, "is_camp": st.IsCamp}}, nil
}

func parseTrainState(raw string) trainState {
	if redirected := match1(raw, `redirect_url=([^&#]+)`); redirected != "" {
		if decoded, err := url.QueryUnescape(redirected); err == nil {
			raw = decoded
		}
	}
	if cid := first(match1(raw, `[?&]campId=(\d+)`), match1(raw, `/financial/.*?campId=(\d+)`)); cid != "" {
		return trainState{CID: cid, IsCamp: true}
	}
	if cid := first(match1(raw, `[?&]periodId=(\d+)`), match1(raw, `/financial/.*?periodId=(\d+)`)); cid != "" {
		return trainState{CID: cid, IsCamp: false}
	}
	return trainState{}
}

func followTrainURL(c *util.Client, h map[string]string, raw string) string {
	resp, err := c.Get(raw, h)
	if err != nil {
		return raw
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String()
	}
	return raw
}

func checkQianliaoLogin(c *util.Client, h map[string]string) error {
	var resp struct {
		State struct {
			Code *int `json:"code"`
		} `json:"state"`
	}
	payload := map[string]any{"transferData": map[string]any{}, "transferUrl": "/gate/user/getUserInfoById"}
	if err := postJSONInto(c, train_user_info_url, payload, h, &resp); err != nil {
		return fmt.Errorf("qianliao train login check: %w", err)
	}
	if resp.State.Code == nil || *resp.State.Code != 0 {
		code := "<missing>"
		if resp.State.Code != nil {
			code = fmt.Sprint(*resp.State.Code)
		}
		return fmt.Errorf("qianliao train login check failed: state.code=%s", code)
	}
	return nil
}

func fetchTrainCourseList(c *util.Client, h map[string]string) ([]trainCourse, error) {
	var out []trainCourse
	for page := 1; page < 10; page++ {
		var resp struct {
			Data struct {
				Courses []struct {
					CampID   any    `json:"campId"`
					PeriodID any    `json:"periodId"`
					Name     string `json:"name"`
				} `json:"courses"`
			} `json:"data"`
		}
		payload := map[string]any{
			"transferData": map[string]any{"page": map[string]any{"page": page, "size": 9999}},
			"transferUrl":  "/gate/course/myCourseList",
		}
		if err := postJSONInto(c, train_course_list_url, payload, h, &resp); err != nil {
			return out, err
		}
		if len(resp.Data.Courses) == 0 {
			break
		}
		for _, co := range resp.Data.Courses {
			campID := jstr(co.CampID)
			periodID := jstr(co.PeriodID)
			cid := first(campID, periodID)
			if cid == "" {
				continue
			}
			out = append(out, trainCourse{CourseID: cid, Title: co.Name, IsCamp: campID != ""})
		}
	}
	seen, dedup := map[string]bool{}, out[:0]
	for _, co := range out {
		key := fmt.Sprintf("%t:%s", co.IsCamp, co.CourseID)
		if !seen[key] {
			seen[key] = true
			dedup = append(dedup, co)
		}
	}
	return dedup, nil
}

func fetchTrainTitle(c *util.Client, h map[string]string, st trainState) string {
	if st.CID == "" {
		return ""
	}
	if st.IsCamp {
		var resp struct {
			Data struct {
				CampName string `json:"campName"`
			} `json:"data"`
		}
		payload := map[string]any{"transferData": map[string]any{"campId": st.CID}, "transferUrl": "/gate/learningCalendar/campData"}
		if postJSONInto(c, train_camp_url, payload, h, &resp) == nil {
			return sanitize(resp.Data.CampName)
		}
		return ""
	}
	body, err := c.GetString(fmt.Sprintf(train_period_url, url.QueryEscape(st.CID)), h)
	if err != nil {
		return ""
	}
	return sanitize(match1(body, `"name"\s*:\s*"(.*?)"`))
}

func fetchTrainItems(c *util.Client, h map[string]string, st trainState) ([]trainItem, error) {
	transferURL := "/gate/openCourse/playListOfCourse"
	transferData := map[string]any{"periodId": st.CID}
	if st.IsCamp {
		transferURL = "/gate/course/playListOfCourse"
		transferData = map[string]any{"campId": st.CID}
	}
	var resp struct {
		Data struct {
			Courses []struct {
				Name    string `json:"name"`
				Type    string `json:"type"`
				TopicID any    `json:"topicId"`
				RoomID  any    `json:"roomId"`
				PlayURL string `json:"playUrl"`
			} `json:"courses"`
		} `json:"data"`
	}
	payload := map[string]any{"transferData": transferData, "transferUrl": transferURL}
	if err := postJSONInto(c, fmt.Sprintf(train_info_url, transferURL), payload, h, &resp); err != nil {
		return nil, err
	}
	var out []trainItem
	for _, co := range resp.Data.Courses {
		if !isTrainMediaType(co.Type) {
			continue
		}
		topicID := jstr(co.TopicID)
		if topicID == "" {
			continue
		}
		out = append(out, trainItem{TopicID: topicID, RoomID: jstr(co.RoomID), Type: co.Type, Name: co.Name, PlayURL: co.PlayURL})
	}
	return out, nil
}

func isTrainMediaType(t string) bool {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "VIDEO", "AUDIO", "INTELLIGENT_LIVE", "LIVE":
		return true
	default:
		return false
	}
}

func resolveTrainItem(c *util.Client, h map[string]string, st trainState, item trainItem, index int) (*extractor.MediaInfo, error) {
	playURL, audioURL, err := resolveTrainMedia(c, h, st, item)
	if err != nil {
		return nil, err
	}
	playURL = first(playURL, item.PlayURL)
	if playURL == "" && audioURL == "" {
		return nil, fmt.Errorf("qianliao train: empty media URL for topicId %s", item.TopicID)
	}
	mediaURL := first(playURL, audioURL)
	stream := extractor.Stream{Quality: "best", URLs: []string{mediaURL}, Format: pickFormat(mediaURL), Headers: map[string]string{"Referer": trainReferer}}
	if playURL != "" && audioURL != "" {
		stream.AudioURL = audioURL
		stream.NeedMerge = stream.Format == "m3u8"
	}
	return &extractor.MediaInfo{
		Site:    "qlchat",
		Title:   sanitize(fmt.Sprintf("[%d]--%s", index, first(item.Name, item.TopicID))),
		Streams: map[string]extractor.Stream{"best": stream},
		Extra:   map[string]any{"topic_id": item.TopicID, "room_id": item.RoomID, "video_type": item.Type, "course_id": st.CID, "is_camp": st.IsCamp},
	}, nil
}

func resolveTrainMedia(c *util.Client, h map[string]string, st trainState, item trainItem) (string, string, error) {
	api := ""
	switch strings.ToUpper(strings.TrimSpace(item.Type)) {
	case "VIDEO":
		api = fmt.Sprintf(train_video_url, url.QueryEscape(item.TopicID), url.QueryEscape(st.CID))
	case "AUDIO":
		api = fmt.Sprintf(train_audio_url, url.QueryEscape(item.TopicID), url.QueryEscape(st.CID))
	case "INTELLIGENT_LIVE", "LIVE":
		if st.IsCamp {
			api = fmt.Sprintf(train_course_live_url, url.QueryEscape(item.TopicID), url.QueryEscape(st.CID), url.QueryEscape(item.RoomID))
		} else {
			api = fmt.Sprintf(train_live_url, url.QueryEscape(item.TopicID), url.QueryEscape(st.CID), url.QueryEscape(item.RoomID))
		}
	default:
		return "", "", fmt.Errorf("qianliao train: unsupported media type %s", item.Type)
	}
	body, err := c.GetString(api, h)
	if err != nil {
		return "", "", err
	}
	audioURL := trainCleanURL(match1(body, `"playUrl"\s*:\s*"(https?://[^"]+\.(?i:mp3|m4a|aac)[^"]*)"`))
	appID := match1(body, `"appId"\s*:\s*"(\d+)"`)
	fileID := match1(body, `"fileId"\s*:\s*"(\d+)"`)
	keySign := match1(body, `"keySign"\s*:\s*"(.+?)"`)
	if appID == "" || fileID == "" || keySign == "" {
		return "", audioURL, nil
	}
	playURL, err := resolveTrainQCloud(c, h, appID, fileID, keySign)
	if err != nil {
		return "", audioURL, err
	}
	return playURL, audioURL, nil
}

func resolveTrainQCloud(c *util.Client, h map[string]string, appID, fileID, keySign string) (string, error) {
	body, err := c.GetString(fmt.Sprintf(train_m3u8_url, appID, fileID, keySign), h)
	if err != nil {
		return "", err
	}
	playURL := trainCleanURL(match1(body, `"url"\s*:\s*"(http.+?)"`))
	if playURL == "" {
		return "", nil
	}
	if token := match1(body, `"drmToken"\s*:\s*"(.+?)"`); token != "" {
		parts := strings.Split(playURL, "/")
		if len(parts) > 0 {
			parts[len(parts)-1] = "voddrm.token." + token + "." + parts[len(parts)-1]
			playURL = strings.Join(parts, "/")
		}
	}
	if !strings.Contains(strings.ToLower(playURL), ".m3u8") {
		return playURL, nil
	}
	master, err := c.GetString(playURL, h)
	if err != nil {
		return playURL, nil
	}
	return first(selectTrainM3U8Variant(playURL, master), playURL), nil
}

func selectTrainM3U8Variant(masterURL, body string) string {
	var variants []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(strings.ToLower(line), ".m3u8") {
			continue
		}
		variants = append(variants, line)
	}
	if len(variants) == 0 {
		return ""
	}
	chosen := variants[len(variants)-1]
	if strings.HasPrefix(chosen, "http") {
		return trainCleanURL(chosen)
	}
	base := masterURL
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[:i+1]
	}
	return trainCleanURL(base + chosen)
}

func trainCleanURL(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	raw = strings.ReplaceAll(raw, `\u0026`, "&")
	return regexp.MustCompile(`\\+`).ReplaceAllString(raw, "")
}
