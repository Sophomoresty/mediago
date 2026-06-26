package haozaixian

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func (x *hzCtx) mediaFromLessons(lessons []hzLesson, courseMaterials []hzMaterial) (*extractor.MediaInfo, error) {
	entries := []*extractor.MediaInfo{}
	appendStream := func(name, u, kind string) {
		if u == "" {
			return
		}
		fmtv := extFormat(u)
		entry := &extractor.MediaInfo{
			Site:  "haozaixian",
			Title: cleanName(name),
			Extra: map[string]any{"kind": kind},
			Streams: map[string]extractor.Stream{"best": {
				Quality:   "best",
				URLs:      []string{u},
				Format:    fmtv,
				NeedMerge: strings.EqualFold(fmtv, "m3u8"),
				Headers:   map[string]string{"Referer": referer, "Cookie": x.cookie, "User-Agent": x.headers["User-Agent"]},
			}},
		}
		entries = append(entries, entry)
	}
	for _, lesson := range lessons {
		if lesson.Type == "ai_video" {
			roundID := lesson.RoundID
			if roundID == "" {
				roundID = x.getAIRoundID(lesson.LessonID)
			}
			if roundID != "" {
				if videos := x.getAIVideoURLs(lesson.LessonID, roundID); len(videos) > 0 {
					for i, v := range videos {
						name := fmt.Sprintf("[%d.1.%d]--%s", lesson.Seq, i+1, lesson.LessonName)
						kind := "ai_video"
						if i == 0 {
							name = fmt.Sprintf("[%d.1]--%s_主课内容", lesson.Seq, lesson.LessonName)
						} else {
							name = fmt.Sprintf("[%d.1.%d]--%s_题目解析", lesson.Seq, i, lesson.LessonName)
						}
						appendStream(name, v.URL, kind)
					}
					goto lessonExtras
				}
			}
		}
		if u := x.getVideoAddress(lesson.LessonID, lesson.LiveRoomID, lesson.LessonName); u != "" {
			appendStream(lesson.Name, u, "video")
		}
	lessonExtras:
		if teacherImgs, myImgs := x.getCourseEmphasisImages(lesson.LessonID, x.cid); len(teacherImgs) > 0 || len(myImgs) > 0 {
			for i, u := range teacherImgs {
				appendStream(fmt.Sprintf("(%d.1.2.%d)--%s", lesson.Seq, i+1, lesson.LessonName), u, "image")
			}
			for i, u := range myImgs {
				appendStream(fmt.Sprintf("(%d.1.3.%d)--%s", lesson.Seq, i+1, lesson.LessonName), u, "image")
			}
		}
		if imgs := x.getLessonLectureImages(lesson.LessonID, x.cid); len(imgs) > 0 {
			for i, u := range imgs {
				appendStream(fmt.Sprintf("(%d.1.1.%d)--%s", lesson.Seq, i+1, lesson.LessonName), u, "image")
			}
		}
		for _, m := range lesson.Materials {
			appendStream(m.Name, m.URL, m.Kind)
		}
	}
	for _, m := range courseMaterials {
		appendStream(m.Name, m.URL, m.Kind)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("haozaixian: empty media list")
	}
	return &extractor.MediaInfo{Site: "haozaixian", Title: firstNonEmpty(x.title, x.cid, "haozaixian"), Entries: entries, Extra: map[string]any{"course_id": x.cid, "course_type": x.courseType, "n_course_type": x.nCourseType}}, nil
}

func (x *hzCtx) getVideoAddress(lessonID, liveRoomID, lessonName string) string {
	candidates := []struct {
		url   string
		pairs []kv
	}{}
	if x.courseType == SPECIAL_COURSE_TYPE {
		approuter := fmt.Sprintf("https://c4-jx-stable.zuoyebang.com/static/hy/ai-room/enter-4c97df9b-hycache.html?courseId=%s&lessonId=%s&liveRoomId=%s&title=%s&funcId=0&customId=0&ai=1&na__zyb_source__=%s", x.cid, lessonID, liveRoomID, url.PathEscape(lessonName), x.naSource)
		candidates = append(candidates, struct {
			url   string
			pairs []kv
		}{ai_video_url, []kv{{"na__zyb_source__", x.naSource}, {"approuter", approuter}, {"liveRoomId", liveRoomID}, {"courseId", x.cid}, {"lessonId", lessonID}, {"lcsversion", "7"}, {"product", "fudao"}, {"protoVersion", "1"}, {"ram", "32"}, {"sdk", "20"}, {"os", "win"}}})
		candidates = append(candidates, struct {
			url   string
			pairs []kv
		}{special_video_url, []kv{{"appId", "winhaoke"}, {"isPlayback", "1"}, {"approuter", fmt.Sprintf("https://c4-jx-stable.zuoyebang.com/static/hy/mix-room-live/enter-bd6ae220-hycache.html?courseId=%s&lessonId=%s&liveRoomId=%s&isPlayback=1&liveStage=1&na__zyb_source__=%s", x.cid, lessonID, liveRoomID, x.naSource)}, {"courseId", x.cid}, {"lessonId", lessonID}}})
	} else {
		candidates = append(candidates, struct {
			url   string
			pairs []kv
		}{system_video_url, []kv{{"appId", "winhaoke"}, {"isPlayback", "1"}, {"approuter", fmt.Sprintf("https://c3-jx-stable.zuoyebang.com/static/hy/mix-room-live/enter-ad9a44fc-hycache.html?courseId=%s&lessonId=%s&liveRoomId=%s&isPlayback=1&liveStage=1&na__zyb_source__=%s", x.cid, lessonID, liveRoomID, x.naSource)}, {"courseId", x.cid}, {"lessonId", lessonID}}})
	}
	for _, cand := range candidates {
		root, err := x.requestJSON(queryURL(cand.url, cand.pairs...), nil)
		if err != nil || str(root["errNo"]) != "0" {
			continue
		}
		data := asMap(root["data"])
		if vi := asMap(data["videoInfo"]); len(vi) > 0 {
			if urls := x.pickVideoInfoURLs(listAsAny(vi["videoAddress"])); len(urls) > 0 {
				for _, u := range urls {
					if x.isPlainM3U8URL(u) || !strings.Contains(strings.ToLower(u), ".m3u8") {
						return u
					}
				}
			}
		}
		preloading := asMap(data["preloading"])
		if mix := asMap(preloading["mixRoomVideoInfo"]); len(mix) > 0 {
			if urls := x.pickMultiClarityUrls(mix); len(urls) > 0 {
				for _, u := range urls {
					if x.isPlainM3U8URL(u) || !strings.Contains(strings.ToLower(u), ".m3u8") {
						return u
					}
				}
			}
			if u := firstNonEmpty(str(mix["videoAddress"]), str(mix["lbpVideoAddress"])); u != "" {
				return u
			}
		}
		if lbk := asMap(preloading["lbk"]); len(lbk) > 0 {
			if u := str(lbk["lbpVideoAddress"]); u != "" {
				return u
			}
		}
	}
	return ""
}
