package bilibili

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

// API endpoints from decompiled Mooc/Courses/Bilibili/Bilibili_Course.pyc:
//
//	https://api.bilibili.com/pugv/pay/web/my/paid?ps=10&pn={page}
//	https://api.bilibili.com/pugv/view/web/season/v2?season_id={cid}
//	https://api.bilibili.com/pugv/view/web/season?ep_id={ep_id}
//	https://api.bilibili.com/pugv/player/web/playurl?fnval=16&fourk=1&ep_id={vid}
const (
	cheeseSeasonV2        = "https://api.bilibili.com/pugv/view/web/season/v2?season_id=%s"
	cheeseSeasonEP        = "https://api.bilibili.com/pugv/view/web/season?ep_id=%s"
	cheesePlayURL         = "https://api.bilibili.com/pugv/player/web/playurl?fnval=16&fourk=1&ep_id=%s"
	cheesePaidList        = "https://api.bilibili.com/pugv/pay/web/my/paid?ps=10&pn=%d"
	cheesePaidMaxPages    = 500
	cheesePaidListTitle   = "Bilibili Cheese Paid Courses"
	cheeseDownloadReferer = "https://www.bilibili.com/"
)

var cheesePatterns = []string{
	`bilibili\.com/cheese/play/(?:ss|ep)\d+`,
	`bilibili\.com/cheese(?:$|[/?#])`,
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
var cheeseHomeRe = regexp.MustCompile(`bilibili\.com/cheese(?:$|[/?#])`)

func (c *BilibiliCheese) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("bilibili cheese requires login cookies")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)
	h := cheeseHeaders()

	if m := cheeseEPRe.FindStringSubmatch(rawURL); m != nil {
		return extractCheeseSeason(client, h, fmt.Sprintf(cheeseSeasonEP, m[1]), "")
	}
	if m := cheeseSSRe.FindStringSubmatch(rawURL); m != nil {
		return extractCheeseSeason(client, h, fmt.Sprintf(cheeseSeasonV2, m[1]), "")
	}
	if cheeseHomeRe.MatchString(rawURL) {
		return extractCheesePaidList(client, h)
	}
	return nil, fmt.Errorf("cannot parse cheese URL: %s", rawURL)
}

func cheeseHeaders() map[string]string {
	return map[string]string{
		"Accept":  "application/json, text/plain, */*",
		"Referer": cheeseDownloadReferer,
	}
}

func extractCheeseSeason(client *util.Client, h map[string]string, seasonURL string, entryPrefix string) (*extractor.MediaInfo, error) {
	season, err := fetchCheeseSeason(client, h, seasonURL)
	if err != nil {
		return nil, err
	}
	entries, err := buildCheeseEntries(client, h, season.Title, entryPrefix, season.episodes())
	if err != nil {
		return nil, err
	}
	return &extractor.MediaInfo{
		Site:    "bilibili-cheese",
		Title:   util.SanitizeFilename(biliFirstNonEmpty(season.Title, entryPrefix, "bilibili_cheese")),
		Entries: entries,
	}, nil
}

func extractCheesePaidList(client *util.Client, h map[string]string) (*extractor.MediaInfo, error) {
	courses, err := fetchCheesePaidCourses(client, h)
	if err != nil {
		return nil, err
	}
	if len(courses) == 0 {
		return nil, fmt.Errorf("bilibili cheese paid list is empty")
	}

	var entries []*extractor.MediaInfo
	skippedCourses := 0
	var firstErr error
	for _, course := range courses {
		season, err := fetchCheeseSeason(client, h, fmt.Sprintf(cheeseSeasonV2, course.ID))
		if err != nil {
			skippedCourses++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		courseTitle := biliFirstNonEmpty(course.Title, season.Title, "course_"+course.ID)
		courseEntries, err := buildCheeseEntries(client, h, courseTitle, courseTitle, season.episodes())
		if err != nil {
			skippedCourses++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		entries = append(entries, courseEntries...)
	}
	if len(entries) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no playable paid cheese episodes: %w", firstErr)
		}
		return nil, fmt.Errorf("no playable paid cheese episodes")
	}

	return &extractor.MediaInfo{
		Site:    "bilibili-cheese",
		Title:   cheesePaidListTitle,
		Entries: entries,
		Extra: map[string]any{
			"course_count":    len(courses),
			"skipped_courses": skippedCourses,
		},
	}, nil
}

type cheesePaidCourse struct {
	ID    string
	Title string
}

func fetchCheesePaidCourses(client *util.Client, h map[string]string) ([]cheesePaidCourse, error) {
	var courses []cheesePaidCourse
	seen := map[string]bool{}
	totalHint := 0

	for page := 1; page <= cheesePaidMaxPages; page++ {
		body, err := client.GetString(fmt.Sprintf(cheesePaidList, page), h)
		if err != nil {
			return nil, fmt.Errorf("pugv paid list page %d fetch: %w", page, err)
		}
		var resp struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				Data []struct {
					ID       biliStringID `json:"id"`
					SeasonID biliStringID `json:"season_id"`
					Title    string       `json:"title"`
				} `json:"data"`
				List []struct {
					ID       biliStringID `json:"id"`
					SeasonID biliStringID `json:"season_id"`
					Title    string       `json:"title"`
				} `json:"list"`
				Page struct {
					Total int `json:"total"`
				} `json:"page"`
				Total int `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			return nil, fmt.Errorf("parse pugv paid list page %d: %w", page, err)
		}
		if resp.Code != 0 {
			return nil, fmt.Errorf("pugv paid list page %d returned code=%d message=%q", page, resp.Code, resp.Message)
		}
		if totalHint == 0 {
			totalHint = resp.Data.Page.Total
			if totalHint == 0 {
				totalHint = resp.Data.Total
			}
		}

		pageCourses := make([]cheesePaidCourse, 0, len(resp.Data.Data)+len(resp.Data.List))
		for _, item := range resp.Data.Data {
			id := biliFirstNonEmpty(item.ID.String(), item.SeasonID.String())
			if id != "" {
				pageCourses = append(pageCourses, cheesePaidCourse{ID: id, Title: strings.TrimSpace(item.Title)})
			}
		}
		for _, item := range resp.Data.List {
			id := biliFirstNonEmpty(item.ID.String(), item.SeasonID.String())
			if id != "" {
				pageCourses = append(pageCourses, cheesePaidCourse{ID: id, Title: strings.TrimSpace(item.Title)})
			}
		}
		if len(pageCourses) == 0 {
			break
		}
		for _, course := range pageCourses {
			if seen[course.ID] {
				continue
			}
			seen[course.ID] = true
			courses = append(courses, course)
		}
		if totalHint > 0 && len(courses) >= totalHint {
			break
		}
	}
	return courses, nil
}

type cheeseSeason struct {
	Title    string             `json:"title"`
	Episodes []cheeseRawEpisode `json:"episodes"`
	Sections []struct {
		Title    string             `json:"title"`
		Episodes []cheeseRawEpisode `json:"episodes"`
	} `json:"sections"`
}

type cheeseRawEpisode struct {
	ID        biliStringID `json:"id"`
	EPID      biliStringID `json:"ep_id"`
	EpisodeID biliStringID `json:"episode_id"`
	Title     string       `json:"title"`
	LongTitle string       `json:"long_title"`
	Duration  int          `json:"duration"`
}

type cheeseEpisode struct {
	ID           string
	Title        string
	SectionTitle string
	Duration     int
}

func fetchCheeseSeason(client *util.Client, h map[string]string, seasonURL string) (*cheeseSeason, error) {
	body, err := client.GetString(seasonURL, h)
	if err != nil {
		return nil, fmt.Errorf("pugv season fetch: %w", err)
	}
	var resp struct {
		Code    int          `json:"code"`
		Message string       `json:"message"`
		Data    cheeseSeason `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("parse pugv season: %w", err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("pugv season returned code=%d message=%q", resp.Code, resp.Message)
	}
	if len(resp.Data.episodes()) == 0 {
		return nil, fmt.Errorf("pugv season has no episodes (course locked?)")
	}
	return &resp.Data, nil
}

func (s cheeseSeason) episodes() []cheeseEpisode {
	var out []cheeseEpisode
	seen := map[string]bool{}
	appendEpisode := func(sectionTitle string, raw cheeseRawEpisode) {
		id := biliFirstNonEmpty(raw.ID.String(), raw.EPID.String(), raw.EpisodeID.String())
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, cheeseEpisode{
			ID:           id,
			Title:        biliFirstNonEmpty(raw.Title, raw.LongTitle, "episode_"+id),
			SectionTitle: strings.TrimSpace(sectionTitle),
			Duration:     raw.Duration,
		})
	}
	for _, ep := range s.Episodes {
		appendEpisode("", ep)
	}
	for _, section := range s.Sections {
		for _, ep := range section.Episodes {
			appendEpisode(section.Title, ep)
		}
	}
	return out
}

func buildCheeseEntries(client *util.Client, h map[string]string, courseTitle string, entryPrefix string, episodes []cheeseEpisode) ([]*extractor.MediaInfo, error) {
	if len(episodes) == 0 {
		return nil, fmt.Errorf("pugv season has no episodes")
	}
	var entries []*extractor.MediaInfo
	var firstErr error
	for i, ep := range episodes {
		streams, err := fetchCheesePlayStreams(client, h, ep.ID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		title := fmt.Sprintf("%02d %s", i+1, ep.Title)
		if ep.SectionTitle != "" {
			title = fmt.Sprintf("%02d %s -- %s", i+1, ep.SectionTitle, ep.Title)
		}
		if entryPrefix != "" {
			title = entryPrefix + " -- " + title
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:    "bilibili-cheese",
			Title:   util.SanitizeFilename(title),
			Streams: streams,
			Extra: map[string]any{
				"course_title": courseTitle,
				"episode_id":   ep.ID,
				"duration":     ep.Duration,
			},
		})
	}
	if len(entries) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no playable episodes: %w", firstErr)
		}
		return nil, fmt.Errorf("no playable episodes")
	}
	return entries, nil
}

type cheeseDashStream struct {
	ID         int    `json:"id"`
	BaseURL    string `json:"baseUrl"`
	BaseURLAlt string `json:"base_url"`
	Bandwidth  int    `json:"bandwidth"`
}

func (s cheeseDashStream) URL() string { return biliFirstNonEmpty(s.BaseURL, s.BaseURLAlt) }

func fetchCheesePlayStreams(client *util.Client, h map[string]string, epID string) (map[string]extractor.Stream, error) {
	body, err := client.GetString(fmt.Sprintf(cheesePlayURL, epID), h)
	if err != nil {
		return nil, fmt.Errorf("pugv playurl ep_id=%s fetch: %w", epID, err)
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			DRMType int `json:"drm_type"`
			Dash    struct {
				Video []cheeseDashStream `json:"video"`
				Audio []cheeseDashStream `json:"audio"`
			} `json:"dash"`
			DUrl []struct {
				URL  string `json:"url"`
				Size int64  `json:"size"`
			} `json:"durl"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("parse pugv playurl ep_id=%s: %w", epID, err)
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("pugv playurl ep_id=%s returned code=%d message=%q", epID, resp.Code, resp.Message)
	}
	if resp.Data.DRMType != 0 {
		return nil, fmt.Errorf("pugv playurl ep_id=%s is DRM-protected", epID)
	}

	streams := map[string]extractor.Stream{}
	if len(resp.Data.Dash.Video) > 0 {
		bestVideo := bestCheeseDash(resp.Data.Dash.Video)
		bestAudio := bestCheeseDash(resp.Data.Dash.Audio)
		if bestVideo.URL() != "" {
			audioURL := bestAudio.URL()
			streams["dash"] = extractor.Stream{
				Quality:   "best",
				URLs:      []string{bestVideo.URL()},
				Format:    "dash",
				NeedMerge: audioURL != "",
				AudioURL:  audioURL,
				Headers:   cheeseDownloadHeaders(),
			}
		}
	} else if len(resp.Data.DUrl) > 0 {
		for i, d := range resp.Data.DUrl {
			if d.URL == "" {
				continue
			}
			streams[fmt.Sprintf("default_%d", i+1)] = extractor.Stream{
				Quality: "default",
				URLs:    []string{d.URL},
				Format:  biliPickFormat(d.URL, "mp4"),
				Size:    d.Size,
				Headers: cheeseDownloadHeaders(),
			}
		}
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("pugv playurl ep_id=%s has no downloadable streams", epID)
	}
	return streams, nil
}

func bestCheeseDash(streams []cheeseDashStream) cheeseDashStream {
	var best cheeseDashStream
	for _, s := range streams {
		if s.URL() == "" {
			continue
		}
		if best.URL() == "" || s.Bandwidth > best.Bandwidth || (s.Bandwidth == best.Bandwidth && s.ID > best.ID) {
			best = s
		}
	}
	return best
}

func cheeseDownloadHeaders() map[string]string {
	return map[string]string{
		"Referer":    cheeseDownloadReferer,
		"User-Agent": util.RandomUA(),
	}
}
