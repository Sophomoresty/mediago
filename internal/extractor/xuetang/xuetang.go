// Package xuetang implements an extractor for next.xuetangx.com courses.
//
// API chain ported from decompiled Mooc/Courses/Xuetang/Xuetang_Course.pyc:
//  1. /api/v1/lms/learn/product/info?cid=&sign=    → classroom_name (course title)
//  2. /api/v1/lms/learn/course/chapter?cid=&sign=  → section/leaf tree (chapter list)
//  3. /api/v1/lms/learn/leaf_info/{cid}/{leaf_id}/?sign={sign} → content_info.media.ccid
//  4. /api/v1/lms/service/playurl/{ccid}/?appid=10000 → data.sources.quality10/quality20 (mp4 URLs)
//
// Sign + cid are pulled out of the URL ("/course/SIGN/CID" or "/learn[/space]/SIGN/.../CID").
// Supports xuetangx.com, cmgemooc.com, gradsmartedu.cn.
package xuetang

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`(?:[\w-]+\.)*(?:xuetangx\.com|cmgemooc\.com|gradsmartedu\.cn)/.*?(?:course|learn(?:/space)?)/`,
}

// URL forms ported from Mooc_Config.courses_re['Xuetang_Course']:
//
//	/course/{sign}/{cid}
//	/learn[/space]/{sign}/.../{cid}
var (
	urlCourseRe = regexp.MustCompile(`https?://([^/]+)/.*?course/([^/]+)/(\d+)`)
	urlLearnRe  = regexp.MustCompile(`https?://([^/]+)/.*?learn(?:/space)?/([^/]+)/.*?/(\d+)`)
)

func init() {
	extractor.Register(&Xuetang{}, extractor.SiteInfo{
		Name:     "Xuetang",
		URL:      "next.xuetangx.com",
		NeedAuth: true,
	})
}

type Xuetang struct{}

func (x *Xuetang) Patterns() []string { return patterns }

func (x *Xuetang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xuetang requires login cookies (use --cookies or --cookies-from-browser)")
	}

	host, sign, cid := parseURL(rawURL)
	if cid == "" || sign == "" {
		return nil, fmt.Errorf("cannot parse xuetang URL: %s", rawURL)
	}
	base := "https://" + host

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := map[string]string{
		"Referer":  base + "/",
		"X-Client": "web",
		"xtbz":     "cloud",
	}

	titleBody, _ := c.GetString(fmt.Sprintf("%s/api/v1/lms/learn/product/info?cid=%s&sign=%s", base, cid, sign), h)
	title := matchGroup1(titleBody, `"classroom_name"\s*:\s*"([^"]+)"`)
	if title == "" {
		title = "xuetang_" + cid
	}

	chapBody, err := c.GetString(fmt.Sprintf("%s/api/v1/lms/learn/course/chapter?cid=%s&sign=%s", base, cid, sign), h)
	if err != nil {
		return nil, fmt.Errorf("course/chapter: %w", err)
	}
	var chapResp struct {
		Data struct {
			ContentData []struct {
				Name           string `json:"name"`
				SectionLeafLst []struct {
					Name     string `json:"name"`
					LeafType int    `json:"leaf_type"`
					ID       any    `json:"id"`
					LeafList []struct {
						Name     string `json:"name"`
						LeafType int    `json:"leaf_type"`
						ID       any    `json:"id"`
					} `json:"leaf_list"`
				} `json:"section_leaf_list"`
			} `json:"content_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(chapBody), &chapResp); err != nil {
		return nil, fmt.Errorf("parse chapter: %w", err)
	}

	var entries []*extractor.MediaInfo
	for ci, ch := range chapResp.Data.ContentData {
		for si, sec := range ch.SectionLeafLst {
			leafs := sec.LeafList
			if len(leafs) == 0 && sec.LeafType == 0 {
				leafs = append(leafs, struct {
					Name     string `json:"name"`
					LeafType int    `json:"leaf_type"`
					ID       any    `json:"id"`
				}{Name: sec.Name, LeafType: 0, ID: sec.ID})
			}
			for li, leaf := range leafs {
				if leaf.LeafType != 0 {
					continue
				}
				videoURL := getVideoURL(c, base, h, sign, cid, fmt.Sprint(leaf.ID))
				if videoURL == "" {
					continue
				}
				name := fmt.Sprintf("%02d.%02d.%02d %s", ci+1, si+1, li+1, sanitize(leaf.Name))
				entries = append(entries, &extractor.MediaInfo{
					Site:  "xuetang",
					Title: name,
					Streams: map[string]extractor.Stream{
						"default": {
							Quality: "best",
							URLs:    []string{videoURL},
							Format:  pickFormat(videoURL),
							Headers: map[string]string{"Referer": base + "/"},
						},
					},
				})
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no playable videos found (course locked or no purchase?)")
	}

	return &extractor.MediaInfo{
		Site:    "xuetang",
		Title:   title,
		Entries: entries,
	}, nil
}

// getVideoURL implements _get_signature → _get_video_url:
//
//	leaf_info/{cid}/{leaf}/?sign={sign} → data.content_info.media.ccid
//	service/playurl/{ccid}/?appid=10000 → data.sources.quality10/20 (mp4 URLs)
func getVideoURL(c *util.Client, base string, h map[string]string, sign, cid, leafID string) string {
	leafURL := fmt.Sprintf("%s/api/v1/lms/learn/leaf_info/%s/%s/?sign=%s", base, cid, leafID, sign)
	body, err := c.GetString(leafURL, h)
	if err != nil {
		return ""
	}
	var leaf struct {
		Data struct {
			ContentInfo struct {
				Media struct {
					CCID            any    `json:"ccid"`
					LivePlaybackURL string `json:"live_palyback_url"`
				} `json:"media"`
			} `json:"content_info"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &leaf) != nil {
		return ""
	}
	if leaf.Data.ContentInfo.Media.LivePlaybackURL != "" {
		return leaf.Data.ContentInfo.Media.LivePlaybackURL
	}
	ccid := fmt.Sprint(leaf.Data.ContentInfo.Media.CCID)
	if ccid == "" || ccid == "<nil>" || ccid == "0" {
		return ""
	}
	playURL := fmt.Sprintf("%s/api/v1/lms/service/playurl/%s/?appid=10000", base, url.PathEscape(ccid))
	playBody, err := c.GetString(playURL, h)
	if err != nil {
		return ""
	}
	var pr struct {
		Data struct {
			Sources struct {
				Q10 []string `json:"quality10"`
				Q20 []string `json:"quality20"`
			} `json:"sources"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(playBody), &pr) != nil {
		return ""
	}
	if len(pr.Data.Sources.Q20) > 0 {
		return pr.Data.Sources.Q20[0]
	}
	if len(pr.Data.Sources.Q10) > 0 {
		return pr.Data.Sources.Q10[0]
	}
	return ""
}

func parseURL(u string) (host, sign, cid string) {
	if m := urlCourseRe.FindStringSubmatch(u); m != nil {
		return m[1], m[2], m[3]
	}
	if m := urlLearnRe.FindStringSubmatch(u); m != nil {
		return m[1], m[2], m[3]
	}
	return "", "", ""
}

func matchGroup1(s, pat string) string {
	if m := regexp.MustCompile(pat).FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

var sanitizeRe = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)

func sanitize(s string) string { return sanitizeRe.ReplaceAllString(strings.TrimSpace(s), "_") }

func pickFormat(u string) string {
	if strings.Contains(u, ".m3u8") {
		return "m3u8"
	}
	return "mp4"
}
