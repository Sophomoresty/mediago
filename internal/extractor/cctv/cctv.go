package cctv

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	neturl "net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`tv\.cctv\.com/.+\.shtml`,
	`cctv\.com/.+/VIDE\w+\.shtml`,
	`cctv\.com/.+/index\.shtml`,
}

func init() {
	extractor.Register(&CCTV{}, extractor.SiteInfo{
		Name: "CCTV",
		URL:  "tv.cctv.com",
	})
}

type CCTV struct{}

func (c *CCTV) Patterns() []string { return patterns }

func (c *CCTV) Extract(url string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	client := util.NewClient()
	headers := cctvHeaders()

	body, err := client.GetString(url, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch CCTV page: %w", err)
	}

	guid := extractGUID(body)
	if guid == "" {
		return nil, fmt.Errorf("cannot find video GUID in page")
	}

	title := extractTitle(body)
	if title == "" {
		title = "cctv_video"
	}

	apiURL := fmt.Sprintf("https://vdn.apps.cntv.cn/api/getHttpVideoInfo.do?pid=%s", guid)
	apiBody, err := client.GetString(apiURL, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video info: %w", err)
	}

	var info map[string]interface{}
	if err := json.Unmarshal([]byte(apiBody), &info); err != nil {
		return nil, fmt.Errorf("failed to parse CCTV API response: %w", err)
	}
	if status := stringFromAny(info["status"]); status != "" && status != "001" {
		return nil, fmt.Errorf("CCTV API returned status=%s", status)
	}

	if apiTitle := firstNonEmpty(stringFromAny(info["title"]), stringFromAny(info["tag"])); apiTitle != "" {
		title = apiTitle
	}

	streams := make(map[string]extractor.Stream)

	if selected := selectBestHLSURL(client, info, headers); selected != nil && selected.URL != "" {
		streams["hls"] = extractor.Stream{
			Quality:   selected.Quality(),
			URLs:      []string{selected.URL},
			Format:    "m3u8",
			NeedMerge: true,
			Headers:   cloneHeaders(headers),
		}
	}

	if videoURL := stringFromAny(info["video_url"]); videoURL != "" {
		streams["mp4"] = extractor.Stream{
			Quality: "default",
			URLs:    []string{videoURL},
			Format:  "mp4",
			Headers: cloneHeaders(headers),
		}
	}

	if len(streams) == 0 {
		return nil, fmt.Errorf("no streams found for CCTV video")
	}

	return &extractor.MediaInfo{
		Site:    "cctv",
		Title:   title,
		Streams: streams,
	}, nil
}

const (
	cctvReferer   = "https://www.cctv.cn/"
	cctvOrigin    = "https://www.cctv.cn"
	cctvUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

type hlsCandidate struct {
	Priority      int
	Bandwidth     int
	URL           string
	SourceKey     string
	EncryptedType string
	Supported     bool
}

type hlsProbe struct {
	URL       string
	Width     int
	Height    int
	Area      int
	Bandwidth int
	MasterURL string
}

type hlsSelection struct {
	URL           string
	Width         int
	Height        int
	Area          int
	Bandwidth     int
	Priority      int
	SourceKey     string
	EncryptedType string
}

func (s hlsSelection) Quality() string {
	if s.Height > 0 {
		return fmt.Sprintf("%dp", s.Height)
	}
	if s.Bandwidth > 0 {
		return fmt.Sprintf("%dk", s.Bandwidth/1000)
	}
	return "best"
}

func cctvHeaders() map[string]string {
	return map[string]string{
		"cookie":     "",
		"Accept":     "*/*",
		"Origin":     cctvOrigin,
		"Referer":    cctvReferer,
		"User-Agent": cctvUserAgent,
	}
}

func cloneHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func selectBestHLSURL(client *util.Client, info map[string]interface{}, headers map[string]string) *hlsSelection {
	type ranked struct {
		area      int
		bandwidth int
		priority  int
		selection *hlsSelection
	}

	var rankedItems []ranked
	seen := make(map[string]bool)

	for _, cand := range candidateHLSURLs(info) {
		if !cand.Supported {
			continue
		}
		probe := inspectHLSURL(client, cand.URL, headers, cand.Bandwidth, 0)
		if probe == nil {
			continue
		}
		finalURL := firstNonEmpty(probe.URL, cand.URL)
		if finalURL == "" || seen[finalURL] {
			continue
		}
		seen[finalURL] = true

		bandwidth := maxInt(probe.Bandwidth, cand.Bandwidth)
		sel := &hlsSelection{
			URL:           finalURL,
			Width:         probe.Width,
			Height:        probe.Height,
			Area:          probe.Area,
			Bandwidth:     bandwidth,
			Priority:      cand.Priority,
			SourceKey:     cand.SourceKey,
			EncryptedType: cand.EncryptedType,
		}
		rankedItems = append(rankedItems, ranked{
			area:      sel.Area,
			bandwidth: sel.Bandwidth,
			priority:  sel.Priority,
			selection: sel,
		})
	}

	sort.SliceStable(rankedItems, func(i, j int) bool {
		if rankedItems[i].area != rankedItems[j].area {
			return rankedItems[i].area > rankedItems[j].area
		}
		if rankedItems[i].bandwidth != rankedItems[j].bandwidth {
			return rankedItems[i].bandwidth > rankedItems[j].bandwidth
		}
		return rankedItems[i].priority > rankedItems[j].priority
	})
	if len(rankedItems) == 0 {
		return nil
	}
	return rankedItems[0].selection
}

func candidateHLSURLs(info map[string]interface{}) []hlsCandidate {
	var candidates []hlsCandidate
	addCandidate := func(priority, bandwidth int, rawURL, sourceKey, encryptedType string, supported bool) {
		u := normalizeCCTVURL(rawURL, "")
		if u == "" {
			return
		}
		candidates = append(candidates, hlsCandidate{
			Priority:      priority,
			Bandwidth:     bandwidth,
			URL:           u,
			SourceKey:     sourceKey,
			EncryptedType: encryptedType,
			Supported:     supported,
		})
		for _, variant := range plainHLSVariantURLs(u) {
			candidates = append(candidates, hlsCandidate{
				Priority:      priority - 1,
				Bandwidth:     variant.Bandwidth,
				URL:           variant.URL,
				SourceKey:     sourceKey,
				EncryptedType: encryptedType,
				Supported:     supported,
			})
		}
	}

	if manifest := manifestMap(info["manifest"]); manifest != nil {
		orderedKeys := []string{"hls_h5e_url", "hls_enc_url", "hls_url", "hls_enc2_url", "hls_audio_url"}
		for i, key := range orderedKeys {
			encryptedType := ""
			supported := true
			switch key {
			case "hls_h5e_url":
				encryptedType = "cctv_h5e"
			case "hls_enc_url", "hls_enc2_url":
				encryptedType = "cctv_enc"
				supported = false
			}
			addCandidate(100-i*5, 0, stringFromAny(manifest[key]), key, encryptedType, supported)
		}

		var otherKeys []string
		for key := range manifest {
			if containsString(orderedKeys, key) {
				continue
			}
			if strings.Contains(strings.ToLower(key), "hls") {
				otherKeys = append(otherKeys, key)
			}
		}
		sort.Strings(otherKeys)
		for _, key := range otherKeys {
			addCandidate(40, 0, stringFromAny(manifest[key]), key, "", true)
		}
	}

	addCandidate(60, 0, stringFromAny(info["hls_url"]), "hls_url", "", true)

	if video := mapFromAny(info["video"]); video != nil {
		addCandidate(30, 0, stringFromAny(video["url"]), "video.url", "", true)
		for _, key := range []string{"chapters4", "chapters3", "chapters2", "chapters"} {
			for _, chapter := range listMaps(video[key]) {
				addCandidate(20, 0, stringFromAny(chapter["url"]), key, "", true)
			}
		}
	}

	deduped := make([]hlsCandidate, 0, len(candidates))
	seen := make(map[string]bool)
	for _, cand := range candidates {
		u := normalizeCCTVURL(cand.URL, "")
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		cand.URL = u
		deduped = append(deduped, cand)
	}
	return deduped
}

func inspectHLSURL(client *util.Client, rawURL string, headers map[string]string, declaredBandwidth, depth int) *hlsProbe {
	if client == nil {
		client = util.NewClient()
	}
	videoURL := normalizeCCTVURL(rawURL, "")
	if videoURL == "" || depth > 2 {
		return nil
	}

	resp, err := client.Get(videoURL, headers)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	text := string(body)
	if !strings.HasPrefix(strings.TrimLeft(text, "\ufeff \t\r\n"), "#EXTM3U") {
		return nil
	}

	if !strings.Contains(text, "#EXT-X-STREAM-INF") {
		if !hlsPlaylistHasSegments(text) {
			return nil
		}
		return &hlsProbe{URL: videoURL, Bandwidth: declaredBandwidth}
	}

	for _, variant := range hlsVariantURLs(text, videoURL) {
		nextBandwidth := variant.Bandwidth
		if nextBandwidth == 0 {
			nextBandwidth = declaredBandwidth
		}
		probe := inspectHLSURL(client, variant.URL, headers, nextBandwidth, depth+1)
		if probe == nil {
			continue
		}
		probe.Width = maxInt(probe.Width, variant.Width)
		probe.Height = maxInt(probe.Height, variant.Height)
		probe.Area = maxInt(probe.Area, variant.Area)
		probe.Bandwidth = maxInt(probe.Bandwidth, nextBandwidth)
		probe.MasterURL = videoURL
		return probe
	}
	return nil
}

func hlsPlaylistHasSegments(text string) bool {
	seenExtInf := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF") {
			seenExtInf = true
			continue
		}
		if seenExtInf && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

type hlsVariant struct {
	Width     int
	Height    int
	Area      int
	Bandwidth int
	URL       string
}

func hlsVariantURLs(masterText, masterURL string) []hlsVariant {
	var variants []hlsVariant
	var pending *hlsVariant

	for _, line := range strings.Split(masterText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			bw := firstRegexInt(`BANDWIDTH=(\d+)`, line)
			width, height := resolutionFromLine(line)
			pending = &hlsVariant{
				Width:     width,
				Height:    height,
				Area:      width * height,
				Bandwidth: bw,
			}
			continue
		}
		if pending == nil || strings.HasPrefix(line, "#") {
			continue
		}
		pending.URL = normalizeCCTVURL(line, masterURL)
		if pending.URL != "" {
			variants = append(variants, *pending)
		}
		pending = nil
	}

	sort.SliceStable(variants, func(i, j int) bool {
		if variants[i].Area != variants[j].Area {
			return variants[i].Area > variants[j].Area
		}
		return variants[i].Bandwidth > variants[j].Bandwidth
	})
	return variants
}

func resolutionFromLine(line string) (int, int) {
	m := regexp.MustCompile(`RESOLUTION=(\d+)x(\d+)`).FindStringSubmatch(line)
	if len(m) != 3 {
		return 0, 0
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	return w, h
}

type plainHLSVariant struct {
	Bandwidth int
	URL       string
}

var plainHLSPathRe = regexp.MustCompile(`(?i)^(.*?/asp/(?:[^/]+/)*hls/)(?:main|\d+)/(.+)/(?:main|\d+)\.m3u8$`)

func plainHLSVariantURLs(masterURL string) []plainHLSVariant {
	parsed, err := neturl.Parse(strings.TrimSpace(masterURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path == "" {
		return nil
	}
	m := plainHLSPathRe.FindStringSubmatch(parsed.Path)
	if len(m) != 3 {
		return nil
	}

	out := make([]plainHLSVariant, 0, 5)
	for _, bandwidth := range []int{4000, 2000, 1200, 850, 450} {
		u := *parsed
		u.Path = fmt.Sprintf("%s%d/%s/%d.m3u8", m[1], bandwidth, strings.Trim(m[2], "/"), bandwidth)
		u.RawQuery = ""
		u.Fragment = ""
		out = append(out, plainHLSVariant{Bandwidth: bandwidth, URL: u.String()})
	}
	return out
}

func normalizeCCTVURL(raw, base string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	u, err := neturl.Parse(raw)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	if base == "" {
		return raw
	}
	baseURL, err := neturl.Parse(base)
	if err != nil {
		return raw
	}
	return baseURL.ResolveReference(u).String()
}

func manifestMap(raw interface{}) map[string]interface{} {
	switch v := raw.(type) {
	case map[string]interface{}:
		return v
	case string:
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(v), &out); err == nil {
			return out
		}
	}
	return nil
}

func mapFromAny(raw interface{}) map[string]interface{} {
	if m, ok := raw.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func listMaps(raw interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	list, ok := raw.([]interface{})
	if !ok {
		return out
	}
	for _, item := range list {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func stringFromAny(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstRegexInt(pattern, text string) int {
	m := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	v, _ := strconv.Atoi(m[1])
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func containsString(values []string, needle string) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

var guidPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bvar\s+guid\s*=\s*["']([0-9a-fA-F]{32})["']`),
	regexp.MustCompile(`\bguid\s*[:=]\s*["']([0-9a-fA-F]{32})["']`),
	regexp.MustCompile(`\bvideoCenterId\s*[:=]\s*["']([0-9a-fA-F]{32})["']`),
	regexp.MustCompile(`\bpid\s*[:=]\s*["']([0-9a-fA-F]{32})["']`),
}

func extractGUID(html string) string {
	for _, re := range guidPatterns {
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

var titlePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)<meta\s+property=["']og:title["']\s+content=["']([^"']+)["']`),
	regexp.MustCompile(`(?i)<meta\s+name=["']title["']\s+content=["']([^"']+)["']`),
	regexp.MustCompile(`(?is)<div\s+class=["']tit["']\s*>(.*?)</div>`),
	regexp.MustCompile(`(?i)<title>(.*?)</title>`),
}

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
	whitespaceRe = regexp.MustCompile(`\s+`)
	titleCleanRe = regexp.MustCompile(`(?i)[_-].*?(?:cctv\.com|央视网).*$`)
)

func cleanText(text string) string {
	text = html.UnescapeString(text)
	text = htmlTagRe.ReplaceAllString(text, "")
	text = whitespaceRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func extractTitle(body string) string {
	for _, re := range titlePatterns {
		if m := re.FindStringSubmatch(body); len(m) > 1 {
			title := cleanText(m[1])
			if title == "" {
				continue
			}
			title = titleCleanRe.ReplaceAllString(title, "")
			title = strings.TrimSpace(title)
			if title != "" {
				return title
			}
		}
	}
	return ""
}
