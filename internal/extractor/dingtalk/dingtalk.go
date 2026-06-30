// Package dingtalk implements an extractor for n.dingtalk.com / h5.dingtalk.com
// live replay shares, alidocs.dingtalk.com document previews, and
// shanji.dingtalk.com AI transcribe replays.
//
// This extractor implements the full LWP (Lightweight Protocol) WebSocket
// client, ported from the decompiled Dingtalk_Live_Client.pyc source. LWP is
// a JSON-over-WebSocket RPC protocol connecting to wss://webalfa-cm3.dingtalk.com/long.
//
// Supported URL types:
//   - Live replay:       n.dingtalk.com/dingding/live-room/index.html?roomId=X&liveUuid=Y
//   - Public live share: h5.dingtalk.com/group-live-share/index.htm?encCid=X&liveUuid=Y
//   - AI transcribe:     shanji.dingtalk.com/app/transcribes/<uuid>
//   - Document preview:  alidocs.dingtalk.com/... (REST, no LWP needed)
package dingtalk

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

var patterns = []string{
	`(?:[\w-]+\.)*dingtalk\.com/(?:dingding/live-room|group-live-share|nt/api|app/transcribes)`,
	`alidocs\.dingtalk\.com/`,
	`shanji\.dingtalk\.com/`,
}

const (
	alidocsPresetURL = "https://alidocs.dingtalk.com/nt/api/docs/preset"
)

func init() {
	extractor.Register(&DingTalk{}, extractor.SiteInfo{
		Name:     "DingTalk",
		URL:      "dingtalk.com",
		NeedAuth: true,
	})
}

type DingTalk struct{}

func (d *DingTalk) Patterns() []string { return patterns }

func (d *DingTalk) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("dingtalk requires login cookies (use --cookies or --cookies-from-browser)")
	}

	if urls := extractDingtalkURLsFromText(rawURL); len(urls) > 0 {
		if len(urls) == 1 {
			rawURL = urls[0]
		} else {
			entries := make([]*extractor.MediaInfo, 0, len(urls))
			for _, u := range urls {
				entry, err := d.Extract(u, opts)
				if err != nil {
					continue
				}
				entries = append(entries, entry)
			}
			if len(entries) == 0 {
				return nil, fmt.Errorf("dingtalk batch text contains URLs but none resolved")
			}
			return &extractor.MediaInfo{
				Site:    "dingtalk",
				Title:   "dingtalk_batch",
				Entries: entries,
				Extra:   map[string]any{"url_count": len(urls), "charge_beans": dingtalkChargeBeans(len(entries))},
			}, nil
		}
	}

	// Alidocs notable/record links contain sheet row metadata and may embed
	// media directly in document payloads or via CSpace file metas.
	if meta := extractNotableRecordMeta(rawURL); meta.Valid() {
		return extractNotableRecord(opts, meta)
	}

	// Alidocs video/file preview (LWP CSpace flow).  The source first fetches
	// the page because some share URLs only expose space/file ids in HTML or
	// redirected query strings.
	if meta := hydratePreviewDentryMeta(opts, extractPreviewDentryMeta(rawURL)); meta.SpaceID != "" && meta.FileID != "" {
		return previewDentry(opts, meta)
	}

	// Legacy document preset/download preview (REST, no LWP)
	if dentryKey := extractDentryKey(rawURL); dentryKey != "" {
		return previewDoc(opts, dentryKey)
	}

	// AI transcribe
	if minutesUUID := extractTranscribeUUID(rawURL); minutesUUID != "" {
		return extractAITranscribe(opts, minutesUUID)
	}

	// Live replay or public share
	roomID, encCid, liveUUID, pcCode := extractLiveIDs(rawURL)
	if liveUUID == "" || (roomID == "" && encCid == "") {
		return nil, fmt.Errorf("cannot parse dingtalk URL — expected live-room, group-live-share, or transcribe format: %s", rawURL)
	}

	cookie := cookieString(opts)

	if encCid != "" {
		return extractPublicLiveShare(cookie, encCid, liveUUID, pcCode)
	}
	return extractLiveReplay(cookie, roomID, liveUUID)
}

// ---------------------------------------------------------------------------
// Live replay extraction
// ---------------------------------------------------------------------------

func extractLiveReplay(cookie, roomID, liveUUID string) (*extractor.MediaInfo, error) {
	result, err := resolveLiveReplay(cookie, roomID, liveUUID)
	if err != nil {
		return nil, fmt.Errorf("dingtalk live replay resolution failed: %w", err)
	}
	return buildMediaInfo(result)
}

func extractPublicLiveShare(cookie, encCid, liveUUID, pcCode string) (*extractor.MediaInfo, error) {
	result, err := resolvePublicLiveShare(cookie, encCid, liveUUID, pcCode)
	if err != nil {
		return nil, fmt.Errorf("dingtalk public live share resolution failed: %w", err)
	}
	return buildMediaInfo(result)
}

func extractAITranscribe(opts *extractor.ExtractOpts, minutesUUID string) (*extractor.MediaInfo, error) {
	cookie := cookieString(opts)
	result, err := resolveAITranscribe(cookie, minutesUUID)
	if err != nil {
		return nil, fmt.Errorf("dingtalk AI transcribe resolution failed: %w", err)
	}
	return buildMediaInfo(result)
}

func buildMediaInfo(result *liveReplayResult) (*extractor.MediaInfo, error) {
	if len(result.PlaybackURLs) == 0 {
		return nil, fmt.Errorf("no playback URLs resolved")
	}

	title := result.Title
	if title == "" {
		title = "dingtalk_" + coalesce(result.RoomID, result.LiveUUID)
	}

	// Choose the best URL
	bestURL := choosePreferredMediaURL(result.PlaybackURLs)
	if bestURL == "" {
		bestURL = result.PlaybackURLs[0]
	}

	// Determine format
	format := "mp4"
	if strings.Contains(strings.ToLower(bestURL), ".m3u8") {
		format = "m3u8"
	}
	streamURLs := append([]string(nil), result.PlaybackURLs...)
	streamExtra := map[string]any{}
	if result.M3U8Content != "" {
		streamURLs = []string{dingtalkM3U8DataURL(result.M3U8Content)}
		format = "m3u8"
		streamExtra["source_urls"] = append([]string(nil), result.PlaybackURLs...)
		streamExtra["source_type"] = "m3u8_text"
	}

	streams := map[string]extractor.Stream{
		"default": {
			Quality:   "best",
			URLs:      streamURLs,
			Format:    format,
			NeedMerge: format == "m3u8",
			Headers: map[string]string{
				"Referer":    referer,
				"User-Agent": pcUA,
			},
			Extra: streamExtra,
		},
	}

	// If we have resolved M3U8 content, include it as an extra
	extra := map[string]any{}
	if result.M3U8Content != "" {
		extra["m3u8_content"] = result.M3U8Content
		extra["m3u8_text"] = result.M3U8Content
	}
	if result.PlaybackToken != "" {
		extra["playback_token"] = result.PlaybackToken
	}
	if types := dingtalkSourceTypes(append(streamURLs, result.PlaybackURLs...)); len(types) > 0 {
		extra["source_types"] = types
	}
	extra["charge_beans"] = dingtalkChargeBeans(1)
	for k, v := range result.Extra {
		extra[k] = v
	}

	info := &extractor.MediaInfo{
		Site:    "dingtalk",
		Title:   title,
		Streams: streams,
	}
	if len(extra) > 0 {
		info.Extra = extra
	}
	return info, nil
}

// ---------------------------------------------------------------------------
// Document preview (REST endpoint, no LWP needed)
// ---------------------------------------------------------------------------

func previewDoc(opts *extractor.ExtractOpts, dentryKey string) (*extractor.MediaInfo, error) {
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)

	body, err := c.PostForm(alidocsPresetURL, map[string]string{"dentryKey": dentryKey}, map[string]string{
		"Referer": "https://alidocs.dingtalk.com/",
	})
	if err != nil {
		return nil, fmt.Errorf("alidocs preset: %w", err)
	}
	var preset struct {
		Data struct {
			Name        string `json:"name"`
			DownloadURL string `json:"downloadUrl"`
			PreviewURL  string `json:"previewUrl"`
			MimeType    string `json:"mimeType"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &preset); err != nil {
		return nil, fmt.Errorf("parse preset: %w", err)
	}
	docURL := preset.Data.DownloadURL
	if docURL == "" {
		docURL = preset.Data.PreviewURL
	}
	if docURL == "" {
		return nil, fmt.Errorf("alidocs preset returned no downloadUrl/previewUrl")
	}
	title := preset.Data.Name
	if title == "" {
		title = "dingtalk_doc_" + dentryKey
	}
	return &extractor.MediaInfo{
		Site:  "dingtalk",
		Title: title,
		Streams: map[string]extractor.Stream{
			"default": {
				Quality: "best",
				URLs:    []string{docURL},
				Format:  "binary",
				Headers: map[string]string{"Referer": "https://alidocs.dingtalk.com/"},
			},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// URL parsing
// ---------------------------------------------------------------------------

var (
	liveRoomRe      = regexp.MustCompile(`live-room/[^?]*\?(?:[^&]*&)*?roomId=([^&]+)`)
	groupShareRe    = regexp.MustCompile(`group-live-share/[^?]*\?(?:[^&]*&)*?encCid=([^&]+)`)
	liveUUIDRe      = regexp.MustCompile(`(?:liveUuid|uuid)=([^&]+)`)
	pcCodeRe        = regexp.MustCompile(`pcCode=([^&]+)`)
	dentryKeyRe     = regexp.MustCompile(`(?:dentryKey|dentryUuid)=([^&\s]+)`)
	transcribeURIRe = regexp.MustCompile(`/transcribes/([\w-]+)`)
)

func extractLiveIDs(u string) (roomID, encCid, liveUUID, pcCode string) {
	if m := liveRoomRe.FindStringSubmatch(u); len(m) > 1 {
		roomID = m[1]
	}
	if m := groupShareRe.FindStringSubmatch(u); len(m) > 1 {
		encCid = m[1]
	}
	if m := liveUUIDRe.FindStringSubmatch(u); len(m) > 1 {
		liveUUID = m[1]
	}
	if m := pcCodeRe.FindStringSubmatch(u); len(m) > 1 {
		pcCode = m[1]
	}
	return
}

func extractDentryKey(u string) string {
	if m := dentryKeyRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractTranscribeUUID(u string) string {
	if m := transcribeURIRe.FindStringSubmatch(u); len(m) > 1 {
		return m[1]
	}
	return ""
}

func coalesce(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

func cookieString(opts *extractor.ExtractOpts) string {
	if opts == nil || opts.Cookies == nil {
		return ""
	}
	// Build a cookie header from the DingTalk hosts used by live replay,
	// shanji minutes, alidocs, and the LWP WebSocket registration.
	origins := []string{
		"https://www.dingtalk.com/",
		"https://n.dingtalk.com/",
		"https://h5.dingtalk.com/",
		"https://live.dingtalk.com/",
		"https://shanji.dingtalk.com/",
		"https://alidocs.dingtalk.com/",
		"https://webalfa-cm3.dingtalk.com/",
	}
	seen := map[string]bool{}
	var parts []string
	for _, origin := range origins {
		parsedURL, err := url.Parse(origin)
		if err != nil || parsedURL == nil {
			continue
		}
		for _, c := range opts.Cookies.Cookies(parsedURL) {
			if c.Name == "" || seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}
