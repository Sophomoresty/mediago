package koolearn

// study.go holds the shared study.koolearn.com plumbing used by the three
// sub-brand flows (Koolearn_Course, Koolearn_Chuguo, Koolearn_Tiny). The
// per-brand course-tree walks live in course.go / chuguo.go / tiny.go; they all
// funnel leaf lessons through the same video-resolution pipeline reconstructed
// from Mooc/Courses/Koolearn/Koolearn_Course (_get_m3u8_text -> _get_m3u8_url ->
// _xdf_m3u8_text) plus the live-replay branches.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

// study.koolearn.com URL templates. Source: Koolearn_Course/Chuguo/Tiny class
// attributes. %s placeholders preserve the source's positional order.
const (
	// Koolearn_Course
	urlCourse        = "https://study.koolearn.com/%s/course/%s/%s"                                                                          // ctype, cid, order
	urlCourseLive    = "https://study.koolearn.com/common/live/data/%s?orderNo=%s&id=-1&level=1"                                             // cid, order
	urlCourseLiveSub = "https://study.koolearn.com/common/live/data/%s?orderNo=%s&id=%s&level=2"                                             // cid, order, node_id
	urlCourseCat     = "https://study.koolearn.com/%s/course_kc_data/%s/%s?pathId=%s&nodeId=-1&level=1"                                      // ctype, cid, order, path_id
	urlCourseLesson  = "https://study.koolearn.com/%s/course_kc_data/%s/%s?pathId=%s&nodeId=%s&level=%d&learningSubjectId=%s"                // ctype, cid, order, path_id, inner_node_id, level, node_id
	urlChapter       = "https://study.koolearn.com/tongyong/diagnostic/lesson/chapter?orderNo=%s&productId=%s&saId=%s"                       // order, cid, path_id
	urlUnit          = "https://study.koolearn.com/tongyong/diagnostic/lesson/unit?lsId=%s&orderNo=%s&productId=%s&saId=%s"                  // ls_id, order, cid, path_id
	urlNode          = "https://study.koolearn.com/tongyong/diagnostic/lesson/node?unitId=%s&lsId=%s&orderNo=%s&productId=%s&saId=%s"        // unit_id, ls_id, order, cid, path_id
	urlGetVideoURL   = "https://media-vod.roombox.xdf.cn/v1/play/getVideoUrl"                                                                // POST
	urlNewVideoInfo  = "https://study.koolearn.com/common/learning/getNewVideoInfo?courseId=%s&nodeId=%s&urlPart=%s&productId=%s&orderNo=%s" // user_cid, video_id, ctype, cid, order
	urlLivePlayback  = "https://api.roombox.xdf.cn/api/client/module/info/playback?classroomId=%s"                                           // live_id
	urlVipInfo       = "https://vip.koolearn.com/api/classlist/list?lessonType=2&type=end&productId=%s&orderNo=%s"                           // cid, order
	urlVipSign       = "https://vip.koolearn.com/api/live/url/student/review?classId=%s"                                                     // live_id
	urlVipVideo      = "https://vip.koolearn.com/api/live/replayVideo/%s/%s?signature=%s"                                                    // consumer_type, live_id, sign

	// Koolearn_Chuguo
	urlChuguoSubject  = "https://study.koolearn.com/chuguo/lesson/%s?orderNo=%s&currentModuleId=%s"                                                                                 // cid, order, mod_id
	urlChuguoCourse   = "https://study.koolearn.com/chuguo/index/course-module/%s/%s"                                                                                               // order, cid
	urlChuguoCat      = "https://study.koolearn.com/chuguo/lesson/nodes?productId=%s&moduleId=%s&learningSubjectId=%s&courseId=%s&level=1"                                          // cid, mod_id, sub_id, user_cid
	urlChuguoLesson   = "https://study.koolearn.com/chuguo/lesson/nodes?productId=%s&moduleId=%s&learningSubjectId=%s&courseId=%s&nodeId=%s&level=%d&isPushed=%s"                   // cid, mod_id, sub_id, user_cid, node_id, level, is_push
	urlChuguoNewVideo = "https://study.koolearn.com/common/learning/getNewVideoInfo?courseId=%s&nodeId=%s&urlPart=%s&productId=%s&orderNo=%s&isRecommend=%d"                       // user_cid, video_id, ctype, cid, order, is_recommend
	urlChuguoLive     = "https://study.koolearn.com/chuguo/live/data/%s/%s"                                                                                                         // cid, order
	urlChuguoLiveSub  = "https://study.koolearn.com/chuguo/live/data/%s/%s?id=%s"                                                                                                    // cid, order, node_id
	urlChuguoIndex    = "https://study.koolearn.com/chuguo/index/%s?orderNo=%s"                                                                                                      // cid, order
	urlChuguoNewIndex = "https://study.koolearn.com/chuguo/index/new/index?productId=%s&orderNo=%s"                                                                                  // cid, order

	// Koolearn_Tiny
	urlTinyTitle  = "https://study.koolearn.com/chuguo/small-class/index/top-msg?orderNo=%s&productId=%s"                                                       // order, cid
	urlTinyCourse = "https://study.koolearn.com/chuguo/small-class/index/course-module?orderNo=%s&productId=%s"                                                 // order, cid
	urlTinyCat    = "https://study.koolearn.com/chuguo/small-class/lesson/nodes?itemId=%s&orderNo=%s&productId=%s&level=1&type=%s"                              // mod_id, order, cid, mod_type
	urlTinyLesson = "https://study.koolearn.com/chuguo/small-class/lesson/nodes?productId=%s&orderNo=%s&itemId=%s&type=%s&learningSubjectId=%s&level=%d"        // cid, order, node_id, mod_type, mod_id, level
	urlTinyLive   = "https://vip.koolearn.com/api/live/replayVideo/%s/%s?signature=%s"                                                                          // lid1, lid2, sign

	urlUserFind = "https://study.koolearn.com/common/find/user"
)

// maxTreeDepth caps the course-tree recursion. Source descends levels 2->3->4,
// so depth 4 is the deepest a leaf can appear.
const maxTreeDepth = 4

// studyNode is one normalized lesson-tree node carried through the per-brand
// walks. A node is a leaf (playable) when leaf is true; otherwise it is a
// branch that needs another _get_lesson_list call to enumerate children.
type studyNode struct {
	nodeID   string
	name     string
	leaf     bool
	nodeType int
	isLive   bool
	jumpURL  string
	userCID  string // Chuguo/Tiny per-node userCourseId
	isPushed bool   // Chuguo isPushed propagation
}

// studyContext holds the resolved cid/order/ctype/userCID/userID for one
// course, shared by the tree walk and the video resolver. It mirrors the
// Koolearn_Course instance state (self.cid / self.order_no / self._ctype /
// self.user_cid / self.user_id).
type studyContext struct {
	c        *util.Client
	header   map[string]string
	cid      string
	order    string
	ctype    string
	userCID  string
	userID   string
	paramURL string // Koolearn_Course self._param_url (?ct=&courseId=)
}

func studyHeaders(jar http.CookieJar) map[string]string {
	return map[string]string{"Referer": urlHome}
}

// studyLogined checks the i.koolearn.com/logininfo status marker (shared with
// koolearn.go's koolearnLogined). Source: Koolearn_Base._check_cookie.
func studyLogined(c *util.Client) bool {
	return koolearnLogined(c)
}

// getJSON fetches url and unmarshals into v. A JSONDecodeError in the source is
// treated as empty/no-data, so a parse failure returns (false, nil) here rather
// than an error — callers then see an empty result, matching the source.
func (sc *studyContext) getJSON(rawURL string, v any) (bool, error) {
	body, err := sc.c.GetString(rawURL, sc.header)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(body), v); err != nil {
		return false, nil
	}
	return true, nil
}

func (sc *studyContext) getString(rawURL string) (string, error) {
	return sc.c.GetString(rawURL, sc.header)
}

// resolveLeafStream turns a leaf studyNode into a playable Stream. It mirrors
// Koolearn_Course._download_video_info dispatch: live replay via jump_url, then
// the getNewVideoInfo VOD path (direct mediaUrl, else roombox getVideoUrl).
// ctypeOverride and isRecommend let Chuguo pass urlPart=chuguo&isRecommend=1.
func (sc *studyContext) resolveLeafStream(node studyNode, ctypeOverride string, isRecommend int) (*extractor.Stream, error) {
	if node.isLive && node.jumpURL != "" {
		liveURL, err := sc.resolveLiveURL(node.jumpURL)
		if err != nil {
			return nil, err
		}
		if liveURL == "" {
			return nil, nil
		}
		return &extractor.Stream{Quality: "best", URLs: []string{liveURL}, Format: mediaExt(liveURL), Headers: map[string]string{"Referer": urlRoomReferer}}, nil
	}

	userCID := node.userCID
	if userCID == "" {
		userCID = sc.userCID
	}
	ctype := ctypeOverride
	if ctype == "" {
		ctype = sc.ctype
	}
	mediaURL, rvideoID, roomHdr, err := sc.fetchNewVideoInfo(node.nodeID, userCID, ctype, isRecommend)
	if err != nil {
		return nil, err
	}
	if mediaURL != "" {
		// Direct HLS playlist. The per-segment AES key is decrypted at download
		// time (Koolearn JS decrypt_koolearn_key / koolearn_key_decode keyed by
		// user_id); the extractor only surfaces the playlist URL + the user_id
		// needed for that step.
		return &extractor.Stream{
			Quality: "best",
			URLs:    []string{mediaURL},
			Format:  "m3u8",
			Headers: map[string]string{"Referer": urlHome},
		}, nil
	}
	if rvideoID != "" && len(roomHdr) > 0 {
		roomURL, err := sc.fetchRoomboxVideoURL(rvideoID, roomHdr)
		if err != nil {
			return nil, err
		}
		if roomURL != "" {
			return &extractor.Stream{Quality: "best", URLs: []string{roomURL}, Format: "m3u8", Headers: map[string]string{"Referer": urlRoomReferer}}, nil
		}
	}
	return nil, nil
}

// newVideoInfoResp models the getNewVideoInfo data block.
// Source: _get_m3u8_text reads data.mediaUrl/mediaUrlH5 and
// data.roomboxInfo.rvideoId/rheaderJson.
type newVideoInfoResp struct {
	Data struct {
		MediaURL    string `json:"mediaUrl"`
		MediaURLH5  string `json:"mediaUrlH5"`
		RoomboxInfo struct {
			RvideoID    string `json:"rvideoId"`
			RheaderJSON string `json:"rheaderJson"`
		} `json:"roomboxInfo"`
	} `json:"data"`
}

// fetchNewVideoInfo calls getNewVideoInfo and returns the direct mediaUrl (if
// any), the roombox rvideoId, and the roombox auth headers parsed from
// rheaderJson. isRecommend < 0 selects the Course template (no isRecommend
// param); >= 0 selects the Chuguo template.
func (sc *studyContext) fetchNewVideoInfo(videoID, userCID, ctype string, isRecommend int) (string, string, map[string]string, error) {
	var infoURL string
	if isRecommend >= 0 {
		infoURL = fmt.Sprintf(urlChuguoNewVideo, url.QueryEscape(userCID), url.QueryEscape(videoID), url.QueryEscape(ctype), url.QueryEscape(sc.cid), url.QueryEscape(sc.order), isRecommend)
	} else {
		infoURL = fmt.Sprintf(urlNewVideoInfo, url.QueryEscape(userCID), url.QueryEscape(videoID), url.QueryEscape(ctype), url.QueryEscape(sc.cid), url.QueryEscape(sc.order))
	}
	var resp newVideoInfoResp
	ok, err := sc.getJSON(infoURL, &resp)
	if err != nil {
		return "", "", nil, fmt.Errorf("koolearn getNewVideoInfo: %w", err)
	}
	if !ok {
		return "", "", nil, nil
	}
	mediaURL := firstNonEmpty(resp.Data.MediaURL, resp.Data.MediaURLH5)
	var roomHdr map[string]string
	if resp.Data.RoomboxInfo.RheaderJSON != "" {
		_ = json.Unmarshal([]byte(resp.Data.RoomboxInfo.RheaderJSON), &roomHdr)
	}
	return mediaURL, resp.Data.RoomboxInfo.RvideoID, roomHdr, nil
}

// fetchRoomboxVideoURL POSTs to media-vod getVideoUrl with the roombox auth
// headers and returns url_infos[0].url. Source: Koolearn_Course._get_m3u8_url.
func (sc *studyContext) fetchRoomboxVideoURL(rvideoID string, roomHdr map[string]string) (string, error) {
	payload := map[string]any{
		"definition":       "SD,LD,FD,HD,2K,4K,OD,SQ,HQ,AUTO",
		"enc_type":         0,
		"stream_type":      "",
		"url_type":         0,
		"video_id":         rvideoID,
		"exclude_enc_type": []int{0},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	headers := map[string]string{"Content-Type": "application/json", "Referer": urlRoomReferer}
	for _, k := range []string{"X-Roombox-App-Id", "X-Roombox-Authmode", "X-Roombox-Nonce", "X-Roombox-Signature", "X-Roombox-Timestamp"} {
		if v, ok := roomHdr[k]; ok {
			headers[k] = v
		}
	}
	resp, err := sc.c.Post(urlGetVideoURL, bytes.NewReader(body), headers)
	if err != nil {
		return "", fmt.Errorf("koolearn getVideoUrl: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("koolearn getVideoUrl: HTTP %d", resp.StatusCode)
	}
	var out struct {
		URLInfos []struct {
			URL string `json:"url"`
		} `json:"url_infos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", nil
	}
	if len(out.URLInfos) > 0 {
		return out.URLInfos[0].URL, nil
	}
	return "", nil
}

var (
	reLiveEntryRel = regexp.MustCompile(`(//study\.koolearn\.com/live/entry.*)`)
	reLiveEntryAbs = regexp.MustCompile(`(https://study\.koolearn\.com/live/entry.*)`)
	reLiveCID      = regexp.MustCompile(`cid=(\d+)`)
	reLiveToken    = regexp.MustCompile(`token=([\w.\-_]+)`)
)

// resolveLiveURL follows a lesson jump_url to a roombox live-playback mp4.
// Source: Koolearn_Course._get_live_url.
func (sc *studyContext) resolveLiveURL(jumpURL string) (string, error) {
	entry := ""
	if m := reLiveEntryRel.FindStringSubmatch(jumpURL); len(m) > 1 {
		entry = "https:" + m[1]
	} else {
		full := jumpURL
		if strings.HasPrefix(full, "/") {
			full = urlStudyHome + full
		} else if !strings.HasPrefix(full, "http") {
			full = urlStudyHome + "/" + full
		}
		finalURL, err := sc.finalURL(full)
		if err != nil {
			return "", err
		}
		if dec, derr := url.QueryUnescape(finalURL); derr == nil {
			finalURL = dec
		}
		if m := reLiveEntryAbs.FindStringSubmatch(finalURL); len(m) > 1 {
			entry = m[1]
		}
	}
	if entry == "" {
		return "", nil
	}
	playURL, err := sc.finalURL(entry)
	if err != nil {
		return "", err
	}
	mCID := reLiveCID.FindStringSubmatch(playURL)
	mTok := reLiveToken.FindStringSubmatch(playURL)
	if len(mCID) < 2 || len(mTok) < 2 {
		return "", nil
	}
	headers := map[string]string{"Referer": urlHome, "Token": mTok[1]}
	body, err := sc.c.GetString(fmt.Sprintf(urlLivePlayback, url.QueryEscape(mCID[1])), headers)
	if err != nil {
		return "", fmt.Errorf("koolearn live playback: %w", err)
	}
	var out struct {
		Data struct {
			VideoURL any `json:"videoUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return "", nil
	}
	return firstURL(out.Data.VideoURL), nil
}

// finalURL issues a GET and returns the post-redirect URL. Source uses
// request_get_raw(...).url to follow 302s for live-entry resolution.
func (sc *studyContext) finalURL(rawURL string) (string, error) {
	resp, err := sc.c.Get(rawURL, sc.header)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String(), nil
	}
	return rawURL, nil
}

// resolveVipReplay resolves a VIP live-replay (consumer_type/live_id) to an mp4.
// Source: Koolearn_Course._get_vip_video_url.
func (sc *studyContext) resolveVipReplay(consumerType, liveID string) (string, error) {
	signBody, err := sc.c.GetString(fmt.Sprintf(urlVipSign, url.QueryEscape(liveID)), sc.header)
	if err != nil {
		return "", fmt.Errorf("koolearn vip sign: %w", err)
	}
	mSign := regexp.MustCompile(`signature=(\w+)`).FindStringSubmatch(signBody)
	if len(mSign) < 2 {
		return "", nil
	}
	videoBody, err := sc.c.GetString(fmt.Sprintf(urlVipVideo, url.QueryEscape(consumerType), url.QueryEscape(liveID), url.QueryEscape(mSign[1])), sc.header)
	if err != nil {
		return "", fmt.Errorf("koolearn vip video: %w", err)
	}
	mURL := regexp.MustCompile(`"videoUrl"\s*:\s*"(http.*?)"`).FindStringSubmatch(videoBody)
	if len(mURL) < 2 {
		return "", nil
	}
	return mURL[1], nil
}

// leafEntry builds a standalone MediaInfo for one resolved leaf lesson, with the
// hierarchy index encoded in the title prefix ("[i.j.k]--name"), matching the
// source's inx_tup naming.
func leafEntry(title string, stream *extractor.Stream, extra map[string]any) *extractor.MediaInfo {
	return &extractor.MediaInfo{
		Site:    "koolearn",
		Title:   title,
		Streams: map[string]extractor.Stream{"best": *stream},
		Extra:   extra,
	}
}

func indexPrefix(idx []int, counter int) string {
	parts := make([]string, 0, len(idx)+1)
	for _, n := range idx {
		parts = append(parts, fmt.Sprintf("%d", n))
	}
	parts = append(parts, fmt.Sprintf("%d", counter))
	return "[" + strings.Join(parts, ".") + "]--"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
