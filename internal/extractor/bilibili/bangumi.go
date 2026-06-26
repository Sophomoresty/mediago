package bilibili

// Bangumi/番剧 extractor.
//
// Source: Course_Others._get_bangumi_list (line 3103) fetches
//   https://api.bilibili.com/pgc/view/web/ep/list?season_id={}
// to get the episode list, then downloads each episode via bbdown with
//   https://www.bilibili.com/bangumi/play/ep{ep_id}
//
// Endpoints (all from decompiled source, never fabricated):
//   - Page scrape for og:title and season_id extraction (regex on HTML)
//   - https://api.bilibili.com/pgc/view/web/ep/list?season_id={}
//   - https://api.bilibili.com/pgc/player/web/playurl?ep_id={}&fnval=4048&fourk=1&qn=127

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	// From source: https://api.bilibili.com/pgc/view/web/ep/list?season_id={}
	bangumiEPListURL = "https://api.bilibili.com/pgc/view/web/ep/list?season_id=%s"
	// Standard pgc playurl endpoint (same param style as x/player/playurl)
	bangumiPlayURL   = "https://api.bilibili.com/pgc/player/web/playurl?ep_id=%s&fnval=4048&fourk=1&qn=127"
	bangumiReferer   = "https://www.bilibili.com"
	bangumiSite      = "bilibili-bangumi"
)

var bangumiPatterns = []string{
	`bilibili\.com/bangumi/play/ss\d+`,
	`bilibili\.com/bangumi/play/ep\d+`,
}

func init() {
	extractor.Register(&BilibiliBangumi{}, extractor.SiteInfo{
		Name: "BilibiliBangumi",
		URL:  "bilibili.com/bangumi",
	})
}

type BilibiliBangumi struct{}

func (b *BilibiliBangumi) Patterns() []string { return bangumiPatterns }

var bangumiSSRe = regexp.MustCompile(`/bangumi/play/ss(\d+)`)
var bangumiEPRe = regexp.MustCompile(`/bangumi/play/ep(\d+)`)

func (b *BilibiliBangumi) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	client := util.NewClient()
	if opts != nil && opts.Cookies != nil {
		client.SetCookieJar(opts.Cookies)
	}
	headers := bangumiHeaders()

	// Extract season_id from ss URL directly
	if m := bangumiSSRe.FindStringSubmatch(rawURL); m != nil {
		return extractBangumiSeason(client, headers, m[1], rawURL)
	}

	// For ep URLs, we need to find the season_id.
	// Fetch the page HTML and look for the season_id redirect/meta.
	if m := bangumiEPRe.FindStringSubmatch(rawURL); m != nil {
		epID := m[1]
		seasonID, title := bangumiResolveEP(client, headers, rawURL)
		if seasonID != "" {
			return extractBangumiSeason(client, headers, seasonID, rawURL)
		}
		// Fallback: download just this single episode
		return extractSingleBangumiEP(client, headers, epID, title)
	}

	return nil, fmt.Errorf("cannot parse bangumi URL: %s", rawURL)
}

func bangumiHeaders() map[string]string {
	return map[string]string{
		"Referer":    bangumiReferer,
		"User-Agent": util.RandomUA(),
	}
}

// bangumiResolveEP fetches the bangumi page HTML and extracts season_id
// via the regex from source: www\.bilibili\.com/bangumi/play/ss(\d+)
// Also extracts the og:title for the season title.
func bangumiResolveEP(client *util.Client, headers map[string]string, pageURL string) (string, string) {
	body, err := client.GetString(pageURL, headers)
	if err != nil {
		return "", ""
	}

	// Source: re.search('property\\s*=\\s*"og:title"\\s*content\\s*=\\s*"(.*?)"', text)
	titleRe := regexp.MustCompile(`property\s*=\s*"og:title"\s*content\s*=\s*"(.*?)"`)
	var title string
	if m := titleRe.FindStringSubmatch(body); m != nil {
		title = m[1]
	}

	// Source: re.search('www\\.bilibili\\.com/bangumi/play/ss(\\d+)', text)
	ssRe := regexp.MustCompile(`www\.bilibili\.com/bangumi/play/ss(\d+)`)
	if m := ssRe.FindStringSubmatch(body); m != nil {
		return m[1], title
	}

	// Also try __INITIAL_STATE__ JSON which often has season_id
	ssIDRe := regexp.MustCompile(`"season_id"\s*:\s*(\d+)`)
	if m := ssIDRe.FindStringSubmatch(body); m != nil {
		return m[1], title
	}

	return "", title
}

func extractBangumiSeason(client *util.Client, headers map[string]string, seasonID string, rawURL string) (*extractor.MediaInfo, error) {
	episodes, seasonTitle, err := fetchBangumiEpisodes(client, headers, seasonID)
	if err != nil {
		return nil, err
	}

	if len(episodes) == 0 {
		return nil, fmt.Errorf("bangumi season %s has no episodes", seasonID)
	}

	// If no title from API, try fetching from page
	if seasonTitle == "" {
		_, seasonTitle = bangumiResolveEP(client, headers, rawURL)
	}
	if seasonTitle == "" {
		seasonTitle = "bangumi_ss" + seasonID
	}

	var entries []*extractor.MediaInfo
	var firstErr error
	for i, ep := range episodes {
		epIDStr := fmt.Sprintf("%d", ep.EPID)
		streams, err := fetchBangumiPlayStreams(client, headers, epIDStr)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		// Fetch subtitles using the episode's BVID+CID, same as regular videos.
		// Source: Bilibili_Base.download_sub is available to all subclasses.
		var subtitles []extractor.Subtitle
		if ep.BVID != "" && ep.CID != 0 {
			subtitles = fetchSubtitles(client, ep.BVID, ep.CID)
		}

		epTitle := biliFirstNonEmpty(ep.LongTitle, ep.Title, fmt.Sprintf("EP%d", i+1))
		entryTitle := fmt.Sprintf("[%02d] %s", i+1, epTitle)

		entries = append(entries, &extractor.MediaInfo{
			Site:      bangumiSite,
			Title:     util.SanitizeFilename(entryTitle),
			Streams:   streams,
			Subtitles: subtitles,
			Extra: map[string]any{
				"season_title": seasonTitle,
				"ep_id":        ep.EPID,
				"badge":        ep.Badge,
			},
		})
	}

	if len(entries) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no playable bangumi episodes: %w", firstErr)
		}
		return nil, fmt.Errorf("no playable bangumi episodes")
	}

	return &extractor.MediaInfo{
		Site:    bangumiSite,
		Title:   util.SanitizeFilename(seasonTitle),
		Entries: entries,
		Extra: map[string]any{
			"season_id":     seasonID,
			"episode_count": len(episodes),
		},
	}, nil
}

func extractSingleBangumiEP(client *util.Client, headers map[string]string, epID string, title string) (*extractor.MediaInfo, error) {
	streams, err := fetchBangumiPlayStreams(client, headers, epID)
	if err != nil {
		return nil, err
	}

	if title == "" {
		title = "bangumi_ep" + epID
	}

	// Subtitles are not fetched for the single-EP fallback path because
	// we lack the BVID+CID required by x/player/v2 (this path is only
	// reached when the season_id could not be resolved from the page HTML).
	return &extractor.MediaInfo{
		Site:    bangumiSite,
		Title:   util.SanitizeFilename(title),
		Streams: streams,
		Extra: map[string]any{
			"ep_id": epID,
		},
	}, nil
}

type bangumiEpisode struct {
	EPID      int64  `json:"ep_id"`
	AID       int64  `json:"aid"`
	BVID      string `json:"bvid"`
	CID       int64  `json:"cid"`
	Title     string `json:"title"`
	LongTitle string `json:"long_title"`
	Badge     string `json:"badge"`
}

// fetchBangumiEpisodes calls the pgc ep/list API.
// Source: _get_bangumi_list -> json.loads(text).get('result',{}).get('episodes',[])
func fetchBangumiEpisodes(client *util.Client, headers map[string]string, seasonID string) ([]bangumiEpisode, string, error) {
	apiURL := fmt.Sprintf(bangumiEPListURL, seasonID)
	body, err := client.GetString(apiURL, headers)
	if err != nil {
		return nil, "", fmt.Errorf("bangumi ep list fetch: %w", err)
	}

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Result  struct {
			Title    string            `json:"title"`
			Episodes []bangumiEpisode  `json:"episodes"`
		} `json:"result"`
	}

	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, "", fmt.Errorf("parse bangumi ep list: %w", err)
	}
	if resp.Code != 0 {
		return nil, "", fmt.Errorf("bangumi ep list returned code=%d message=%q", resp.Code, resp.Message)
	}

	return resp.Result.Episodes, resp.Result.Title, nil
}

// fetchBangumiPlayStreams calls the pgc playurl API for a single episode.
// Source: bbdown handles this internally; the pgc playurl API is the
// standard endpoint for bangumi stream resolution.
func fetchBangumiPlayStreams(client *util.Client, headers map[string]string, epID string) (map[string]extractor.Stream, error) {
	apiURL := fmt.Sprintf(bangumiPlayURL, epID)
	body, err := client.GetString(apiURL, headers)
	if err != nil {
		return nil, fmt.Errorf("bangumi playurl ep_id=%s fetch: %w", epID, err)
	}

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Result  struct {
			Dash struct {
				Video []bangumiDashStream `json:"video"`
				Audio []bangumiDashStream `json:"audio"`
			} `json:"dash"`
			DUrl []struct {
				URL  string `json:"url"`
				Size int64  `json:"size"`
			} `json:"durl"`
		} `json:"result"`
	}

	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("parse bangumi playurl ep_id=%s: %w", epID, err)
	}
	if resp.Code != 0 {
		// Common: code=-10403 means region-restricted or login required
		return nil, fmt.Errorf("bangumi playurl ep_id=%s returned code=%d message=%q (may require login or be region-restricted)", epID, resp.Code, resp.Message)
	}

	streams := map[string]extractor.Stream{}

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

	if len(resp.Result.Dash.Video) > 0 {
		var bestAudioURL string
		if len(resp.Result.Dash.Audio) > 0 {
			best := resp.Result.Dash.Audio[0]
			for _, a := range resp.Result.Dash.Audio[1:] {
				if a.url() != "" && a.Bandwidth > best.Bandwidth {
					best = a
				}
			}
			bestAudioURL = best.url()
		}

		for _, v := range resp.Result.Dash.Video {
			vURL := v.url()
			if vURL == "" {
				continue
			}
			q, ok := qualityMap[v.ID]
			if !ok {
				q = fmt.Sprintf("%dp", v.ID)
			}
			if _, exists := streams[q]; exists {
				continue
			}
			streams[q] = extractor.Stream{
				Quality:   q,
				URLs:      []string{vURL},
				Format:    "dash",
				NeedMerge: bestAudioURL != "",
				AudioURL:  bestAudioURL,
				Headers:   bangumiDownloadHeaders(),
			}
		}
	} else if len(resp.Result.DUrl) > 0 {
		for i, d := range resp.Result.DUrl {
			if d.URL == "" {
				continue
			}
			streams[fmt.Sprintf("default_%d", i+1)] = extractor.Stream{
				Quality: "default",
				URLs:    []string{d.URL},
				Format:  "mp4",
				Size:    d.Size,
				Headers: bangumiDownloadHeaders(),
			}
		}
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("bangumi playurl ep_id=%s has no downloadable streams (may require VIP)", epID)
	}

	return streams, nil
}

type bangumiDashStream struct {
	ID        int    `json:"id"`
	BaseURL   string `json:"baseUrl"`
	BaseUrl2  string `json:"base_url"`
	Bandwidth int    `json:"bandwidth"`
}

func (s bangumiDashStream) url() string {
	return biliFirstNonEmpty(s.BaseURL, s.BaseUrl2)
}

func bangumiDownloadHeaders() map[string]string {
	return map[string]string{
		"Referer":    bangumiReferer,
		"User-Agent": util.RandomUA(),
	}
}

// bangumiSeasonTitleFromPage extracts the og:title from the page HTML.
// Reuses the same approach as _get_bangumi_list which scrapes the page
// for property="og:title".
func bangumiSeasonTitleFromPage(body string) string {
	re := regexp.MustCompile(`property\s*=\s*"og:title"\s*content\s*=\s*"(.*?)"`)
	if m := re.FindStringSubmatch(body); m != nil {
		title := strings.TrimSpace(m[1])
		// Clean common suffixes like "_bilibili_哔哩哔哩"
		if idx := strings.Index(title, "_bilibili"); idx > 0 {
			title = title[:idx]
		}
		return title
	}
	return ""
}
