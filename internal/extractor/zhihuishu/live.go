// live.go implements the Zhihuishu_Live sub-brand extractor.
//
// Source: decompiled Mooc/Courses/Zhihuishu/Zhihuishu_Live.pyc
//
// URL pattern from Mooc_Config courses_re:
//
//	Zhihuishu_Live: https?://.*?zhihuishu\.com/live.*?liveId=(?P<cid>\d+)
//
// Endpoints from Zhihuishu_Live class attributes:
//
//	url_course = "https://im.zhihuishu.com/livehome/getCourseInfo?liveId={}"
//	url_info   = "https://im.zhihuishu.com/livehome/getVideosByLiveId?liveId={}"
//
// Video URL resolution reuses the base getVideoURL (initVideo + changeVideoLine).
package zhihuishu

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	urlLiveCourse = "https://im.zhihuishu.com/livehome/getCourseInfo?liveId=%s"
	urlLiveInfo   = "https://im.zhihuishu.com/livehome/getVideosByLiveId?liveId=%s"
)

var liveIDRe = regexp.MustCompile(`(?i)liveId=(\d+)`)

func extractLiveID(u string) string {
	if m := liveIDRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	return ""
}

func isLiveURL(u string) bool {
	return strings.Contains(u, "zhihuishu.com/live") && extractLiveID(u) != ""
}

// extractLive implements the Zhihuishu_Live flow:
// _get_cid -> _get_title -> _get_infos -> _download
func extractLive(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	liveID := extractLiveID(rawURL)
	if liveID == "" {
		return nil, fmt.Errorf("cannot parse zhihuishu live URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := zhihuishuHeaders("https://www.zhihuishu.com")

	// _get_title: getCourseInfo -> speakerUserName, liveName
	title := "zhihuishu_live_" + liveID
	liveName := ""
	body, err := c.GetString(fmt.Sprintf(urlLiveCourse, liveID), h)
	if err == nil {
		speaker := match1(body, `"speakerUserName"\s*:\s*"(.+?)"`)
		ln := match1(body, `"liveName"\s*:\s*"(.+?)"`)
		if ln != "" {
			liveName = sanitize(ln)
		}
		speaker = sanitize(speaker)
		switch {
		case speaker != "" && liveName != "":
			title = speaker + "_" + liveName
		case speaker != "":
			title = speaker
		case liveName != "":
			title = liveName
		}
	}

	// _get_infos: getVideosByLiveId -> list of video entries
	body, err = c.GetString(fmt.Sprintf(urlLiveInfo, liveID), h)
	if err != nil {
		return nil, fmt.Errorf("getVideosByLiveId: %w", err)
	}

	var videoList []struct {
		ID   json.Number `json:"id"`
		Name string      `json:"name"`
		Sort json.Number `json:"sort"`
	}
	if err := json.Unmarshal([]byte(body), &videoList); err != nil {
		return nil, fmt.Errorf("parse live video list: %w", err)
	}

	// Filter out entries without id, sort by (sort, id) as in source
	var filtered []struct {
		ID   string
		Name string
		Sort int64
	}
	for _, v := range videoList {
		id := v.ID.String()
		if id == "" || id == "0" {
			continue
		}
		s, _ := v.Sort.Int64()
		idNum, _ := v.ID.Int64()
		filtered = append(filtered, struct {
			ID   string
			Name string
			Sort int64
		}{ID: id, Name: v.Name, Sort: s*1000000 + idNum})
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Sort < filtered[j].Sort
	})

	// Resolve video URLs and build entries
	usedNames := make(map[string]bool)
	var entries []*extractor.MediaInfo
	for _, v := range filtered {
		videoURL, err := getVideoURL(c, strings.TrimSpace(v.ID), h)
		if err != nil || videoURL == "" {
			continue
		}
		vName := buildLiveVideoName(v.Name, liveName, usedNames)
		entries = append(entries, &extractor.MediaInfo{
			Site:  "zhihuishu",
			Title: vName,
			Streams: map[string]extractor.Stream{
				"default": {
					Quality: "best",
					URLs:    []string{videoURL},
					Format:  pickFormat(videoURL),
					Headers: h,
				},
			},
		})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("zhihuishu live %s returned no playable videos", liveID)
	}

	// Single video: return flat MediaInfo
	if len(entries) == 1 {
		entries[0].Title = title
		return entries[0], nil
	}

	return &extractor.MediaInfo{
		Site:    "zhihuishu",
		Title:   title,
		Entries: entries,
		Extra: map[string]any{
			"live_id":            liveID,
			"discovered_entries": len(entries),
		},
	}, nil
}

// buildLiveVideoName mirrors Zhihuishu_Live._build_live_video_name.
func buildLiveVideoName(videoName, liveName string, used map[string]bool) string {
	// Clean name: strip file extension
	name := sanitize(strings.TrimSpace(videoName))
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}

	var base string
	if liveName != "" {
		if name == "" || name == liveName || strings.HasPrefix(name, liveName+"-") {
			base = liveName
		} else {
			base = liveName + "-" + name
		}
	} else if name != "" {
		base = name
	} else {
		base = "直播回放"
	}

	result := base
	counter := 2
	for used[result] {
		result = fmt.Sprintf("%s_%d", base, counter)
		counter++
	}
	used[result] = true
	return result
}
