package bilibili

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`bilibili\.com/video/[Bb][Vv]\w+`,
	`bilibili\.com/video/av\d+`,
	`b23\.tv/\w+`,
}

func init() {
	extractor.Register(&Bilibili{}, extractor.SiteInfo{
		Name: "Bilibili",
		URL:  "bilibili.com",
	})
}

type Bilibili struct{}

func (b *Bilibili) Patterns() []string {
	return patterns
}

func (b *Bilibili) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	url = resolveShortURL(url)

	bvid := extractBVID(url)
	aid := extractAID(url)

	if bvid == "" && aid == "" {
		return nil, fmt.Errorf("cannot extract video ID from URL: %s", url)
	}

	client := util.NewClient()
	if opts != nil && opts.Cookies != nil {
		client.SetCookieJar(opts.Cookies)
	}

	info, err := getVideoInfo(client, bvid, aid)
	if err != nil {
		return nil, err
	}

	cid := info.cid
	if bvid == "" {
		bvid = info.bvid
	}

	streams, err := getPlayURL(client, bvid, cid)
	if err != nil {
		return nil, err
	}

	return &extractor.MediaInfo{
		Site:    "bilibili",
		Title:   info.title,
		Artist:  info.author,
		Streams: streams,
	}, nil
}

type videoInfo struct {
	bvid   string
	title  string
	author string
	cid    int64
}

func getVideoInfo(client *util.Client, bvid string, aid string) (*videoInfo, error) {
	apiURL := "https://api.bilibili.com/x/web-interface/view?"
	if bvid != "" {
		apiURL += "bvid=" + bvid
	} else {
		apiURL += "aid=" + aid
	}

	headers := map[string]string{
		"Referer": "https://www.bilibili.com",
	}

	body, err := client.GetString(apiURL, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to get video info: %w", err)
	}

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			BVid  string `json:"bvid"`
			Title string `json:"title"`
			Owner struct {
				Name string `json:"name"`
			} `json:"owner"`
			Pages []struct {
				Cid  int64  `json:"cid"`
				Part string `json:"part"`
			} `json:"pages"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse video info: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("bilibili API error: %s (code %d)", resp.Message, resp.Code)
	}

	var cid int64
	if len(resp.Data.Pages) > 0 {
		cid = resp.Data.Pages[0].Cid
	}

	return &videoInfo{
		bvid:   resp.Data.BVid,
		title:  resp.Data.Title,
		author: resp.Data.Owner.Name,
		cid:    cid,
	}, nil
}

func getPlayURL(client *util.Client, bvid string, cid int64) (map[string]extractor.Stream, error) {
	apiURL := fmt.Sprintf(
		"https://api.bilibili.com/x/player/playurl?bvid=%s&cid=%d&fnval=4048&fourk=1&qn=127",
		bvid, cid,
	)

	headers := map[string]string{
		"Referer": "https://www.bilibili.com",
	}

	body, err := client.GetString(apiURL, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to get play URL: %w", err)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Dash struct {
				Video []dashStream `json:"video"`
				Audio []dashStream `json:"audio"`
			} `json:"dash"`
			DUrl []struct {
				URL  string `json:"url"`
				Size int64  `json:"size"`
			} `json:"durl"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse play URL: %w", err)
	}

	streams := make(map[string]extractor.Stream)

	if len(resp.Data.Dash.Video) > 0 {
		qualityMap := map[int]string{
			127: "8K",
			126: "Dolby Vision",
			125: "HDR",
			120: "4K",
			116: "1080p60",
			112: "1080p+",
			80:  "1080p",
			74:  "720p60",
			64:  "720p",
			32:  "480p",
			16:  "360p",
		}

		var bestAudioURL string
		if len(resp.Data.Dash.Audio) > 0 {
			bestAudioURL = resp.Data.Dash.Audio[0].BaseURL
		}

		for _, v := range resp.Data.Dash.Video {
			q, ok := qualityMap[v.ID]
			if !ok {
				q = fmt.Sprintf("%dp", v.ID)
			}
			key := q
			if _, exists := streams[key]; exists {
				continue
			}
			streams[key] = extractor.Stream{
				Quality:   q,
				URLs:      []string{v.BaseURL},
				Format:    "dash",
				NeedMerge: true,
				AudioURL:  bestAudioURL,
				Headers: map[string]string{
					"Referer":    "https://www.bilibili.com",
					"User-Agent": util.RandomUA(),
				},
			}
		}
	} else if len(resp.Data.DUrl) > 0 {
		for i, d := range resp.Data.DUrl {
			streams[fmt.Sprintf("default_%d", i)] = extractor.Stream{
				Quality: "default",
				URLs:    []string{d.URL},
				Format:  "mp4",
				Size:    d.Size,
				Headers: map[string]string{
					"Referer":    "https://www.bilibili.com",
					"User-Agent": util.RandomUA(),
				},
			}
		}
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("no streams found (video may require login)")
	}

	return streams, nil
}

type dashStream struct {
	ID        int    `json:"id"`
	BaseURL   string `json:"baseUrl"`
	Bandwidth int    `json:"bandwidth"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

func extractBVID(url string) string {
	re := regexp.MustCompile(`[Bb][Vv](\w+)`)
	m := re.FindStringSubmatch(url)
	if len(m) > 0 {
		return m[0]
	}
	return ""
}

func extractAID(url string) string {
	re := regexp.MustCompile(`av(\d+)`)
	m := re.FindStringSubmatch(url)
	if len(m) > 1 {
		if _, err := strconv.Atoi(m[1]); err == nil {
			return m[1]
		}
	}
	return ""
}

func resolveShortURL(url string) string {
	if matched, _ := regexp.MatchString(`b23\.tv`, url); matched {
		client := util.NewClient()
		resp, err := client.Get(url, nil)
		if err == nil && resp != nil {
			finalURL := resp.Request.URL.String()
			resp.Body.Close()
			return finalURL
		}
	}
	return url
}
