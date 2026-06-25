package bilibili

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`bilibili\.com/video/[Bb][Vv]\w+`,
	`bilibili\.com/video/av\d+`,
	`b23\.tv/\w+`,
	// Source: Course_Others.prepare normalizes festival URLs that contain bvid param
	`bilibili\.com/festival/.*bvid=\w+`,
	// Source: Course_Others._get_bili_bvid_list handles space collection/series URLs
	`space\.bilibili\.com/\d+/channel/(?:collection|series)detail`,
	`space\.bilibili\.com/\d+/lists`,
	// Source: Course_Others.prepare normalizes mobile space URLs
	`m\.bilibili\.com/space/\d+`,
	// Source: Course_Others.download handles bilibili.com/list/ media list URLs
	`bilibili\.com/list/\w+`,
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

// Compiled regexes for URL normalization and collection/series extraction.
// All patterns sourced from Course_Others.prepare and _get_bili_bvid_list.
var (
	festivalBVIDRe = regexp.MustCompile(`bilibili\.com/festival/.*?bvid=(\w+)`)
	mobileSpaceRe  = regexp.MustCompile(`m\.bilibili\.com/space/(\d+)`)
	spaceListsRe1  = regexp.MustCompile(`space\.bilibili\.com/(\d+)/lists\?.*?sid=(\d+)`)
	spaceListsRe2  = regexp.MustCompile(`space\.bilibili\.com/(\d+)/lists/(\d+)`)

	// Source: _get_bili_bvid_list matches: //space.bilibili.com/{mid}/channel/collectiondetail?sid={sid}
	collectionRe = regexp.MustCompile(`space\.bilibili\.com/(\d+)/channel/collectiondetail\?sid=(\d+)`)
	// Source: _get_bili_bvid_list matches: //space.bilibili.com/{mid}/channel/seriesdetail?sid={sid}
	seriesRe = regexp.MustCompile(`space\.bilibili\.com/(\d+)/channel/seriesdetail\?sid=(\d+)`)
	// Source: _get_bili_bvid_list matches: bilibili.com/list/{up_id}
	listRe = regexp.MustCompile(`bilibili\.com/list/(\w+)`)
)

func (b *Bilibili) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	rawURL = resolveShortURL(rawURL)
	rawURL = normalizeURL(rawURL)

	client := util.NewClient()
	if opts != nil && opts.Cookies != nil {
		client.SetCookieJar(opts.Cookies)
	}
	headers := biliHeaders()

	// Try collection/series/list URL patterns first.
	// Source: Course_Others.download tries _get_bili_bvid_list before single-video flow.
	if m := collectionRe.FindStringSubmatch(rawURL); m != nil {
		return extractCollection(client, headers, m[1], m[2])
	}
	if m := seriesRe.FindStringSubmatch(rawURL); m != nil {
		return extractSeries(client, headers, m[1], m[2])
	}
	if m := listRe.FindStringSubmatch(rawURL); m != nil {
		// Check if this is a list URL that redirected to a collection/series
		return extractMediaList(client, headers, rawURL, m[1])
	}

	// Regular single-video flow
	bvid := extractBVID(rawURL)
	aid := extractAID(rawURL)

	if bvid == "" && aid == "" {
		return nil, fmt.Errorf("cannot extract video ID from URL: %s", rawURL)
	}

	info, err := getVideoInfo(client, bvid, aid)
	if err != nil {
		return nil, err
	}

	if bvid == "" {
		bvid = info.bvid
	}

	// Multi-P: if there is more than one page, produce one entry per page.
	// Source: Course_Others._get_bili_video_p_num iterates P1..Pn with
	//   url?p={i} and downloads each independently via bbdown_download_file.
	if len(info.pages) > 1 {
		return extractMultiP(client, bvid, info)
	}

	// Single-page video
	if len(info.pages) == 0 {
		return nil, fmt.Errorf("video has no pages (cid unavailable)")
	}
	cid := info.pages[0].Cid

	streams, err := getPlayURL(client, bvid, cid)
	if err != nil {
		return nil, err
	}

	subtitles := fetchSubtitles(client, bvid, cid)

	return &extractor.MediaInfo{
		Site:      "bilibili",
		Title:     info.title,
		Artist:    info.author,
		Streams:   streams,
		Subtitles: subtitles,
	}, nil
}

// normalizeURL applies URL transformations from Course_Others.prepare.
// Source: festival bvid extraction, mobile->desktop, lists->collectiondetail.
func normalizeURL(rawURL string) string {
	// Source: re.search('https?://.*?bilibili\.com/festival/.*?bvid=(\w+)', url)
	if m := festivalBVIDRe.FindStringSubmatch(rawURL); m != nil {
		return "https://www.bilibili.com/video/" + m[1]
	}
	// Source: url.replace('m.bilibili.com/space', 'space.bilibili.com')
	if m := mobileSpaceRe.FindStringSubmatch(rawURL); m != nil {
		return "https://space.bilibili.com/" + m[1]
	}
	// Source: re.search('https?://space\.bilibili\.com/(\d+)/lists.*?sid=(\d+)', url)
	if m := spaceListsRe1.FindStringSubmatch(rawURL); m != nil {
		return fmt.Sprintf("https://space.bilibili.com/%s/channel/collectiondetail?sid=%s", m[1], m[2])
	}
	// Source: re.search('https?://space\.bilibili\.com/(\d+)/lists/(\d+)', url)
	if m := spaceListsRe2.FindStringSubmatch(rawURL); m != nil {
		return fmt.Sprintf("https://space.bilibili.com/%s/channel/collectiondetail?sid=%s", m[1], m[2])
	}
	return rawURL
}

func biliHeaders() map[string]string {
	return map[string]string{
		"Referer":    "https://www.bilibili.com",
		"User-Agent": util.RandomUA(),
	}
}

// extractMultiP builds a playlist-style MediaInfo with one entry per page.
// Mirrors Course_Others.download which splits url?p=1..p=N and downloads
// each page independently.
func extractMultiP(client *util.Client, bvid string, info *videoInfo) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	var firstErr error

	for i, page := range info.pages {
		streams, err := getPlayURL(client, bvid, page.Cid)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		subtitles := fetchSubtitles(client, bvid, page.Cid)

		partTitle := page.Part
		if strings.TrimSpace(partTitle) == "" {
			partTitle = fmt.Sprintf("P%d", i+1)
		}
		entryTitle := fmt.Sprintf("[P%d] %s", i+1, partTitle)

		entries = append(entries, &extractor.MediaInfo{
			Site:      "bilibili",
			Title:     util.SanitizeFilename(entryTitle),
			Artist:    info.author,
			Streams:   streams,
			Subtitles: subtitles,
			Extra: map[string]any{
				"page_index": i + 1,
				"cid":        page.Cid,
			},
		})
	}

	if len(entries) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no playable pages: %w", firstErr)
		}
		return nil, fmt.Errorf("no playable pages")
	}

	return &extractor.MediaInfo{
		Site:    "bilibili",
		Title:   info.title,
		Artist:  info.author,
		Entries: entries,
		Extra: map[string]any{
			"page_count": len(info.pages),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Collection (seasons_archives_list)
// Source: _get_bili_bvid_list branch 1
//   API: https://api.bilibili.com/x/polymer/web-space/seasons_archives_list
//         ?mid={mid}&season_id={season_id}&sort_reverse=false&page_num={n}&page_size=99
//   Extracts: data.meta.name (title), data.archives[].bvid (video list)
// ---------------------------------------------------------------------------

func extractCollection(client *util.Client, headers map[string]string, mid, seasonID string) (*extractor.MediaInfo, error) {
	var title string
	var bvids []string

	for page := 1; page <= 99; page++ {
		apiURL := fmt.Sprintf(
			"https://api.bilibili.com/x/polymer/web-space/seasons_archives_list?mid=%s&season_id=%s&sort_reverse=false&page_num=%d&page_size=99",
			mid, seasonID, page,
		)
		body, err := client.GetString(apiURL, headers)
		if err != nil {
			if page == 1 {
				return nil, fmt.Errorf("collection fetch: %w", err)
			}
			break
		}

		var resp struct {
			Code int `json:"code"`
			Data struct {
				Meta struct {
					Name string `json:"name"`
				} `json:"meta"`
				Archives []struct {
					BVID string `json:"bvid"`
				} `json:"archives"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			break
		}
		if resp.Code != 0 {
			if page == 1 {
				return nil, fmt.Errorf("collection API error code=%d", resp.Code)
			}
			break
		}
		if title == "" && resp.Data.Meta.Name != "" {
			title = resp.Data.Meta.Name
		}
		if len(resp.Data.Archives) == 0 {
			break
		}
		for _, a := range resp.Data.Archives {
			if a.BVID != "" {
				bvids = append(bvids, a.BVID)
			}
		}
	}

	if len(bvids) == 0 {
		return nil, fmt.Errorf("collection has no videos")
	}
	if title == "" {
		title = "bilibili_collection_" + seasonID
	}

	return buildBVIDPlaylist(client, headers, title, bvids)
}

// ---------------------------------------------------------------------------
// Series (x/series/archives)
// Source: _get_bili_bvid_list branch 2
//   Title: https://api.bilibili.com/x/series/series?series_id={sid}
//          regex "name"\s*:\s*"(.*?)" on response text
//   List:  https://api.bilibili.com/x/series/archives
//          ?mid={mid}&series_id={sid}&only_normal=true&sort=desc&pn={n}&ps=99
//   Extracts: data.archives[].bvid
// ---------------------------------------------------------------------------

func extractSeries(client *util.Client, headers map[string]string, mid, seriesID string) (*extractor.MediaInfo, error) {
	// Fetch series title
	title := ""
	titleURL := fmt.Sprintf("https://api.bilibili.com/x/series/series?series_id=%s", seriesID)
	if body, err := client.GetString(titleURL, headers); err == nil {
		// Source: re.search('"name"\s*:\s*"(.*?)"', text)
		nameRe := regexp.MustCompile(`"name"\s*:\s*"(.*?)"`)
		if m := nameRe.FindStringSubmatch(body); m != nil {
			title = m[1]
		}
	}

	var bvids []string
	for page := 1; page <= 99; page++ {
		apiURL := fmt.Sprintf(
			"https://api.bilibili.com/x/series/archives?mid=%s&series_id=%s&only_normal=true&sort=desc&pn=%d&ps=99",
			mid, seriesID, page,
		)
		body, err := client.GetString(apiURL, headers)
		if err != nil {
			if page == 1 {
				return nil, fmt.Errorf("series fetch: %w", err)
			}
			break
		}

		var resp struct {
			Code int `json:"code"`
			Data struct {
				Archives []struct {
					BVID string `json:"bvid"`
				} `json:"archives"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			break
		}
		if resp.Code != 0 {
			if page == 1 {
				return nil, fmt.Errorf("series API error code=%d", resp.Code)
			}
			break
		}
		if len(resp.Data.Archives) == 0 {
			break
		}
		for _, a := range resp.Data.Archives {
			if a.BVID != "" {
				bvids = append(bvids, a.BVID)
			}
		}
	}

	if len(bvids) == 0 {
		return nil, fmt.Errorf("series has no videos")
	}
	if title == "" {
		title = "bilibili_series_" + seriesID
	}

	return buildBVIDPlaylist(client, headers, title, bvids)
}

// ---------------------------------------------------------------------------
// Media list (x/v2/medialist/resource/list)
// Source: _get_bili_bvid_list branch 3
//   API: https://api.bilibili.com/x/v2/medialist/resource/list
//        ?mobi_app=web&type=1&biz_id={up_id}&oid={oid}&otype=2
//        &ps=99&direction=false&desc=false&sort_field=1&tid=0&with_current=false
//   Extracts: data.media_list[].bv_id, data.upper.id (fallback to page HTML scraping)
// ---------------------------------------------------------------------------

func extractMediaList(client *util.Client, headers map[string]string, rawURL, upID string) (*extractor.MediaInfo, error) {
	// First, check if the page HTML redirects to a collection/series
	// Source: Course_Others.download checks page HTML for collection/series redirects
	body, err := client.GetString(rawURL, headers)
	if err == nil {
		// Source: re.search('//space.bilibili.com/(\d+)/channel/collectiondetail\?sid=(\d+)', text)
		if m := collectionRe.FindStringSubmatch(body); m != nil {
			return extractCollection(client, headers, m[1], m[2])
		}
		// Source: re.search('//space.bilibili.com/(\d+)/lists\?sid=(\d+)', text)
		if m := spaceListsRe1.FindStringSubmatch(body); m != nil {
			return extractCollection(client, headers, m[1], m[2])
		}
		// Source: re.search('//space.bilibili.com/(\d+)/lists/(\d+)', text)
		if m := spaceListsRe2.FindStringSubmatch(body); m != nil {
			return extractCollection(client, headers, m[1], m[2])
		}
		// Source: re.search('//www.bilibili.com/list/(\d+)/\?sid=(\d+)', url)
		listSIDRe := regexp.MustCompile(`//www\.bilibili\.com/list/(\d+)/\?sid=(\d+)`)
		if m := listSIDRe.FindStringSubmatch(rawURL); m != nil {
			return extractCollection(client, headers, m[1], m[2])
		}

		// Fallback: scrape BVIDs from page HTML
		// Source: re.findall('"index"\s*:\s*\d+.*?"bvid"\s*:\s*"(\w+)".*?"title"\s*:\s*".*?"', text)
		bvidPageRe := regexp.MustCompile(`"index"\s*:\s*\d+.*?"bvid"\s*:\s*"(\w+)".*?"title"\s*:\s*".*?"`)
		matches := bvidPageRe.FindAllStringSubmatch(body, -1)
		if len(matches) > 0 {
			// Source: re.search('name\s*=\s*"author"\s*content\s*=\s*"(.*?)"', text)
			authorRe := regexp.MustCompile(`name\s*=\s*"author"\s*content\s*=\s*"(.*?)"`)
			title := "bilibili_list_" + upID
			if m := authorRe.FindStringSubmatch(body); m != nil {
				title = m[1]
			}

			var bvids []string
			for _, m := range matches {
				bvids = append(bvids, m[1])
			}
			return buildBVIDPlaylist(client, headers, title, bvids)
		}
	}

	// If we couldn't find a collection/series redirect, try the medialist API
	// Source: API uses up_id as biz_id, needs an oid which is typically 0 for first call
	apiURL := fmt.Sprintf(
		"https://api.bilibili.com/x/v2/medialist/resource/list?mobi_app=web&type=1&biz_id=%s&oid=0&otype=2&ps=99&direction=false&desc=false&sort_field=1&tid=0&with_current=false",
		upID,
	)
	apiBody, err := client.GetString(apiURL, headers)
	if err != nil {
		return nil, fmt.Errorf("medialist fetch: %w", err)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			MediaList []struct {
				BvID string `json:"bv_id"`
			} `json:"media_list"`
			Upper struct {
				Name string `json:"name"`
			} `json:"upper"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(apiBody), &resp); err != nil {
		return nil, fmt.Errorf("parse medialist: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("medialist API error code=%d", resp.Code)
	}

	var bvids []string
	for _, item := range resp.Data.MediaList {
		if item.BvID != "" {
			bvids = append(bvids, item.BvID)
		}
	}
	if len(bvids) == 0 {
		return nil, fmt.Errorf("media list has no videos")
	}

	title := biliFirstNonEmpty(resp.Data.Upper.Name, "bilibili_list_"+upID)
	return buildBVIDPlaylist(client, headers, title, bvids)
}

// buildBVIDPlaylist fetches each BVID independently and builds a playlist.
// Source: Course_Others.download iterates bvids calling bbdown_download_file
// for each: 'https://www.bilibili.com/video/{}'.format(bvid)
func buildBVIDPlaylist(client *util.Client, headers map[string]string, title string, bvids []string) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	var firstErr error

	for idx, bvid := range bvids {
		info, err := getVideoInfo(client, bvid, "")
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		resolvedBVID := bvid
		if info.bvid != "" {
			resolvedBVID = info.bvid
		}

		if len(info.pages) > 1 {
			// Multi-P video within collection: expand all pages
			multiP, err := extractMultiP(client, resolvedBVID, info)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			// Prefix multi-P entries with the collection index
			for _, entry := range multiP.Entries {
				entry.Title = util.SanitizeFilename(fmt.Sprintf("[%02d] %s", idx+1, entry.Title))
				entries = append(entries, entry)
			}
		} else if len(info.pages) > 0 {
			cid := info.pages[0].Cid
			streams, err := getPlayURL(client, resolvedBVID, cid)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}

			subtitles := fetchSubtitles(client, resolvedBVID, cid)
			entryTitle := fmt.Sprintf("[%02d] %s", idx+1, info.title)

			entries = append(entries, &extractor.MediaInfo{
				Site:      "bilibili",
				Title:     util.SanitizeFilename(entryTitle),
				Artist:    info.author,
				Streams:   streams,
				Subtitles: subtitles,
				Extra: map[string]any{
					"bvid": resolvedBVID,
				},
			})
		}
	}

	if len(entries) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no playable videos in playlist: %w", firstErr)
		}
		return nil, fmt.Errorf("no playable videos in playlist")
	}

	return &extractor.MediaInfo{
		Site:    "bilibili",
		Title:   util.SanitizeFilename(title),
		Entries: entries,
		Extra: map[string]any{
			"video_count": len(bvids),
		},
	}, nil
}

type videoPage struct {
	Cid  int64  `json:"cid"`
	Part string `json:"part"`
}

type videoInfo struct {
	bvid   string
	title  string
	author string
	pages  []videoPage
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
			BVid  string      `json:"bvid"`
			Title string      `json:"title"`
			Owner struct {
				Name string `json:"name"`
			} `json:"owner"`
			Pages []videoPage `json:"pages"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse video info: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("bilibili API error: %s (code %d)", resp.Message, resp.Code)
	}

	return &videoInfo{
		bvid:   resp.Data.BVid,
		title:  resp.Data.Title,
		author: resp.Data.Owner.Name,
		pages:  resp.Data.Pages,
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

// fetchSubtitles retrieves subtitle tracks from the x/player/v2 API.
// Source: Bilibili_Base.download_sub provides the download helper; the
// player v2 API supplies subtitle metadata including subtitle_url for
// each language track.
func fetchSubtitles(client *util.Client, bvid string, cid int64) []extractor.Subtitle {
	apiURL := fmt.Sprintf(
		"https://api.bilibili.com/x/player/v2?bvid=%s&cid=%d",
		bvid, cid,
	)
	headers := map[string]string{
		"Referer": "https://www.bilibili.com",
	}

	body, err := client.GetString(apiURL, headers)
	if err != nil {
		return nil
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Subtitle struct {
				Subtitles []struct {
					LanDoc      string `json:"lan_doc"`
					Lan         string `json:"lan"`
					SubtitleURL string `json:"subtitle_url"`
				} `json:"subtitles"`
			} `json:"subtitle"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil
	}
	if resp.Code != 0 {
		return nil
	}

	var subs []extractor.Subtitle
	for _, s := range resp.Data.Subtitle.Subtitles {
		subURL := s.SubtitleURL
		if subURL == "" {
			continue
		}
		// bilibili returns protocol-relative URLs
		if strings.HasPrefix(subURL, "//") {
			subURL = "https:" + subURL
		}
		lang := biliFirstNonEmpty(s.LanDoc, s.Lan, "unknown")
		subs = append(subs, extractor.Subtitle{
			Language: lang,
			URL:      subURL,
			Format:   "json", // bilibili subtitle format is JSON (bcc)
		})
	}
	return subs
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
