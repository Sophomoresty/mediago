package bilibili

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

// API endpoints from decompiled Mooc/Courses/Bilibili/Bilibili_Course.pyc:
//   https://api.bilibili.com/pugv/pay/web/my/paid?ps=10&pn={page}
//   https://api.bilibili.com/pugv/view/web/season/v2?season_id={cid}
//   https://api.bilibili.com/pugv/view/web/season?ep_id={ep_id}
//   https://api.bilibili.com/pugv/player/web/playurl?fnval=16&fourk=1&ep_id={vid}
const (
	cheeseSeasonV2 = "https://api.bilibili.com/pugv/view/web/season/v2?season_id=%s"
	cheeseSeasonEP = "https://api.bilibili.com/pugv/view/web/season?ep_id=%s"
	cheesePlayURL  = "https://api.bilibili.com/pugv/player/web/playurl?fnval=16&fourk=1&ep_id=%s"
	cheesePaidList = "https://api.bilibili.com/pugv/pay/web/my/paid?ps=10&pn=%d"
)

var cheesePatterns = []string{
	`bilibili\.com/cheese/play/(?:ss|ep)\d+`,
}

func init() {
	extractor.Register(&BilibiliCheese{}, extractor.SiteInfo{
		Name:     "BilibiliCheese",
		URL:      "bilibili.com/cheese",
		NeedAuth: true,
	})
}

// BilibiliCheese is a separate extractor for Bilibili课堂 (paid courses).
// It registers a distinct URL pattern from the regular video extractor and
// follows the pugv (Pay-User-Generated Video) API chain.
type BilibiliCheese struct{}

func (c *BilibiliCheese) Patterns() []string { return cheesePatterns }

var cheeseEPRe = regexp.MustCompile(`/cheese/play/ep(\d+)`)
var cheeseSSRe = regexp.MustCompile(`/cheese/play/ss(\d+)`)

func (c *BilibiliCheese) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("bilibili cheese requires login cookies")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)
	h := map[string]string{"Referer": "https://www.bilibili.com/"}

	var seasonURL string
	if m := cheeseEPRe.FindStringSubmatch(rawURL); m != nil {
		seasonURL = fmt.Sprintf(cheeseSeasonEP, m[1])
	} else if m := cheeseSSRe.FindStringSubmatch(rawURL); m != nil {
		seasonURL = fmt.Sprintf(cheeseSeasonV2, m[1])
	} else {
		return nil, fmt.Errorf("cannot parse cheese ep/ss id from URL: %s", rawURL)
	}

	body, err := client.GetString(seasonURL, h)
	if err != nil {
		return nil, fmt.Errorf("pugv season fetch: %w", err)
	}
	var season struct {
		Code int `json:"code"`
		Data struct {
			Title    string `json:"title"`
			Episodes []struct {
				EpisodeID int    `json:"id"`
				Title     string `json:"title"`
				Duration  int    `json:"duration"`
			} `json:"episodes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &season); err != nil {
		return nil, fmt.Errorf("parse pugv season: %w", err)
	}
	if season.Code != 0 {
		return nil, fmt.Errorf("pugv season returned code=%d", season.Code)
	}
	if len(season.Data.Episodes) == 0 {
		return nil, fmt.Errorf("pugv season has no episodes (course locked?)")
	}

	var entries []*extractor.MediaInfo
	for i, ep := range season.Data.Episodes {
		playBody, err := client.GetString(fmt.Sprintf(cheesePlayURL, fmt.Sprint(ep.EpisodeID)), h)
		if err != nil {
			continue
		}
		var play struct {
			Code int `json:"code"`
			Data struct {
				Dash struct {
					Video []struct {
						BaseURL  string `json:"baseUrl"`
						BandWdth int    `json:"bandwidth"`
					} `json:"video"`
					Audio []struct {
						BaseURL string `json:"baseUrl"`
					} `json:"audio"`
				} `json:"dash"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(playBody), &play); err != nil || play.Code != 0 || len(play.Data.Dash.Video) == 0 {
			continue
		}
		bestVid := play.Data.Dash.Video[0]
		var audioURL string
		if len(play.Data.Dash.Audio) > 0 {
			audioURL = play.Data.Dash.Audio[0].BaseURL
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:  "bilibili-cheese",
			Title: fmt.Sprintf("%02d %s", i+1, ep.Title),
			Streams: map[string]extractor.Stream{
				"dash": {
					Quality:  "best",
					URLs:     []string{bestVid.BaseURL},
					AudioURL: audioURL,
					Format:   "dash",
					Headers:  h,
				},
			},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no playable episodes (course not purchased?)")
	}

	return &extractor.MediaInfo{
		Site:    "bilibili-cheese",
		Title:   season.Data.Title,
		Entries: entries,
	}, nil
}
