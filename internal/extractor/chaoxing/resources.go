package chaoxing

import (
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type chaoxingResource struct {
	Title    string
	Kind     string
	ObjectID string
	Mid      string
	LiveID   string
	JobID    string
	UUID     string
	RawURL   string
	Ext      string
}

func collectChaoxingResources(cardTexts []string, fallbackTitle string) []chaoxingResource {
	seen := map[string]bool{}
	var out []chaoxingResource
	add := func(res chaoxingResource) {
		res.Title = firstNonEmpty(cleanText(res.Title), cleanText(fallbackTitle), "chaoxing_resource")
		res.Ext = normalizeExt(res.Ext, res.Title, res.RawURL)
		if res.Kind == "" {
			res.Kind = inferResourceKind(res)
		}
		key := strings.Join([]string{res.Kind, res.ObjectID, res.LiveID, res.UUID, res.RawURL, res.Title}, "|")
		if key == "||||" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, res)
	}
	for _, text := range cardTexts {
		for _, obj := range jsObjectsAfter(text, "mArg") {
			var payload any
			if json.Unmarshal([]byte(obj), &payload) == nil {
				for _, m := range attachmentMaps(payload) {
					if res, ok := resourceFromMap(m); ok {
						add(res)
					}
				}
			}
		}
		for _, obj := range dataJSONObjects(text) {
			var payload any
			if json.Unmarshal([]byte(obj), &payload) == nil {
				for _, m := range attachmentMaps(payload) {
					if res, ok := resourceFromMap(m); ok {
						add(res)
					}
				}
			}
		}
		if htmlRes := htmlImageResource(text, fallbackTitle); htmlRes.RawURL != "" {
			add(htmlRes)
		}
	}
	return out
}

func (x *chaoxingContext) resolveResource(res chaoxingResource) *extractor.MediaInfo {
	if res.Kind == "live" {
		if entry := x.resolveLiveResource(res); entry != nil {
			return entry
		}
	}
	if res.Kind == "audio" && res.UUID != "" {
		if u := x.fetchAudioURL(res.UUID); u != "" {
			return streamEntry(res.Title, u, "mp3", x.headers, map[string]any{"kind": "audio", "uuid": res.UUID})
		}
	}
	if res.ObjectID != "" {
		if entry, err := x.resolveObjectResource(res); err == nil {
			return entry
		}
	}
	if res.UUID != "" {
		if u := x.fetchMeetReviewURL(res.UUID); u != "" {
			return streamEntry(res.Title, u, mediaFormat(u, res.Ext), x.headers, map[string]any{"kind": firstNonEmpty(res.Kind, "review"), "uuid": res.UUID})
		}
	}
	if isHTTPURL(res.RawURL) {
		return streamEntry(res.Title, res.RawURL, mediaFormat(res.RawURL, res.Ext), x.headers, map[string]any{"kind": firstNonEmpty(res.Kind, "file")})
	}
	return nil
}

func (x *chaoxingContext) resolveObjectResource(res chaoxingResource) (*extractor.MediaInfo, error) {
	if res.ObjectID == "" {
		return nil, fmt.Errorf("chaoxing: empty object id")
	}
	body, err := x.getString(x.abs("/ananas/status/" + url.PathEscape(res.ObjectID)))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ananas status: %w", err)
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) != nil {
		payload = map[string]any{}
	}
	title := firstNonEmpty(firstFieldString(payload, "filename", "name", "title"), res.Title, "chaoxing_"+res.ObjectID)
	kind := firstNonEmpty(res.Kind, inferResourceKind(res))
	streams := map[string]extractor.Stream{}
	seenURL := map[string]bool{}
	for _, k := range []string{"download", "httphd", "http", "httpmd", "hls", "m3u8", "url"} {
		for _, u := range fieldStrings(payload, k) {
			u = normalizeURL(u)
			if !isHTTPURL(u) || seenURL[u] {
				continue
			}
			seenURL[u] = true
			format := mediaFormat(u, res.Ext)
			quality := k
			if k == "m3u8" {
				quality = "hls"
			}
			if kind == "file" && k != "download" && len(streams) > 0 {
				continue
			}
			streams[quality] = extractor.Stream{Quality: quality, URLs: []string{u}, Format: format, NeedMerge: format == "m3u8", Headers: x.headers}
		}
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("no streams found (resource may be restricted)")
	}
	extra := map[string]any{"kind": kind, "object_id": res.ObjectID}
	subs := x.subtitles(res.Mid)
	return &extractor.MediaInfo{Site: "chaoxing", Title: util.SanitizeFilename(title), Streams: streams, Subtitles: subs, Extra: compactExtra(extra)}, nil
}

func (x *chaoxingContext) resolveLiveResource(res chaoxingResource) *extractor.MediaInfo {
	values := url.Values{}
	values.Set("jobid", res.JobID)
	values.Set("courseid", "")
	values.Set("knowledgeid", "")
	values.Set("clazzid", "")
	values.Set("userid", "")
	values.Set("liveid", res.LiveID)
	body, err := x.getString(x.abs("/ananas/live/liveinfo") + "?" + values.Encode())
	if err == nil {
		var payload any
		if json.Unmarshal([]byte(body), &payload) == nil {
			if u := firstURLMatching(payload, isPlayableURL); u != "" {
				return streamEntry(res.Title, u, mediaFormat(u, "mp4"), x.headers, map[string]any{"kind": "live", "live_id": res.LiveID, "job_id": res.JobID})
			}
		}
	}
	if u := x.fetchMeetReviewURL(firstNonEmpty(res.UUID, res.JobID)); u != "" {
		return streamEntry(res.Title, u, mediaFormat(u, "mp4"), x.headers, map[string]any{"kind": "live", "live_id": res.LiveID, "job_id": res.JobID})
	}
	return nil
}

func (x *chaoxingContext) fetchMeetReviewURL(uuid string) string {
	if uuid == "" {
		return ""
	}
	body, err := x.getString(fmt.Sprintf(meetReviewURL, url.QueryEscape(uuid)))
	if err != nil {
		return ""
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) != nil {
		return ""
	}
	objectID := firstFieldString(payload, "objectId", "objectid")
	if objectID == "" {
		return ""
	}
	body, err = x.getString(fmt.Sprintf(yunFileURL, url.QueryEscape(objectID)))
	if err != nil {
		return ""
	}
	if json.Unmarshal([]byte(body), &payload) != nil {
		return ""
	}
	return firstURLMatching(payload, isHTTPURL)
}

func (x *chaoxingContext) fetchAudioURL(uuid string) string {
	body, err := x.getString(fmt.Sprintf(audioListURL, url.QueryEscape(uuid)))
	if err != nil {
		return ""
	}
	if u := regexpFirst(body, `"http"\s*:\s*"(https?://.*?\.mp3.*?)"`); u != "" {
		return normalizeURL(u)
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) != nil {
		return ""
	}
	if u := firstURLMatching(payload, func(s string) bool { return strings.Contains(strings.ToLower(s), ".mp3") && isHTTPURL(s) }); u != "" {
		return u
	}
	pageID := firstFieldString(payload, "id", "pageId")
	objectID := firstFieldString(payload, "objectId2", "objectId", "objectid")
	if objectID == "" {
		objectID = regexpFirst(body, `"objectId2"\s*:\s*"(\w+?)"`)
	}
	if pageID == "" || objectID == "" {
		return ""
	}
	body, err = x.getString(fmt.Sprintf(audioUpdateURL, url.QueryEscape(pageID), url.QueryEscape(objectID)))
	if err != nil {
		return ""
	}
	if u := regexpFirst(body, `"http"\s*:\s*"(https?://.*?\.mp3.*?)"`); u != "" {
		return normalizeURL(u)
	}
	if json.Unmarshal([]byte(body), &payload) == nil {
		return firstURLMatching(payload, func(s string) bool { return strings.Contains(strings.ToLower(s), ".mp3") && isHTTPURL(s) })
	}
	return ""
}

func (x *chaoxingContext) subtitles(mid string) []extractor.Subtitle {
	if mid == "" {
		return nil
	}
	values := url.Values{}
	values.Set("mid", mid)
	body, err := x.getString(x.abs("/richvideo/subtitle") + "?" + values.Encode())
	if err != nil {
		return nil
	}
	var payload any
	if json.Unmarshal([]byte(body), &payload) != nil {
		return nil
	}
	if u := firstURLMatching(payload, isHTTPURL); u != "" {
		return []extractor.Subtitle{{Language: "default", URL: u, Format: mediaFormat(u, "srt")}}
	}
	return nil
}

func resourceFromMap(m map[string]any) (chaoxingResource, bool) {
	res := chaoxingResource{
		Title:    firstFieldString(m, "name", "title", "filename", "fileName"),
		ObjectID: firstFieldString(m, "objectid", "objectId", "oid"),
		Mid:      firstFieldString(m, "mid"),
		LiveID:   firstFieldString(m, "liveId", "liveid"),
		JobID:    firstFieldString(m, "jobid", "jobId"),
		UUID:     firstFieldString(m, "uuid"),
		RawURL:   normalizeURL(firstFieldString(m, "url", "downloadUrl", "downloadURL", "statusUrl", "loadurl", "fileUrl")),
		Ext:      firstFieldString(m, "type", "suffix", "ext", "fileType"),
	}
	if res.UUID == "" && res.RawURL != "" {
		res.UUID = regexpFirst(res.RawURL, `appswh\.chaoxing\.com/.*?/view/([\w-]+)`)
	}
	res.Kind = inferResourceKind(res)
	if res.ObjectID == "" && res.LiveID == "" && res.UUID == "" && res.RawURL == "" {
		return chaoxingResource{}, false
	}
	return res, true
}

func inferResourceKind(res chaoxingResource) string {
	if res.LiveID != "" {
		return "live"
	}
	ext := strings.TrimPrefix(strings.ToLower(firstNonEmpty(res.Ext, fileExt(res.Title), fileExt(res.RawURL))), ".")
	if res.UUID != "" || strings.Contains(strings.ToLower(res.RawURL), "appswh.chaoxing.com") || ext == "mp3" || ext == "m4a" {
		return "audio"
	}
	if isDocumentExt(ext) || (res.ObjectID != "" && ext != "" && ext != "mp4" && ext != "m3u8" && ext != "flv") {
		return "file"
	}
	if res.ObjectID != "" || isPlayableURL(res.RawURL) {
		return "video"
	}
	if isHTTPURL(res.RawURL) {
		return "file"
	}
	return "resource"
}

func attachmentMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch vv := x.(type) {
		case map[string]any:
			if arr, ok := vv["attachments"].([]any); ok {
				for _, item := range arr {
					if im, ok := item.(map[string]any); ok {
						if prop, ok := im["property"].(map[string]any); ok {
							out = append(out, prop)
						} else {
							out = append(out, im)
						}
					}
				}
			}
			if _, ok := resourceFromMap(vv); ok {
				out = append(out, vv)
			}
			for _, child := range vv {
				walk(child)
			}
		case []any:
			for _, child := range vv {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func dataJSONObjects(text string) []string {
	var out []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?is)data\s*=\s*'({[\s\S]*?})'`),
		regexp.MustCompile(`(?is)data\s*=\s*"({[\s\S]*?})"`),
	} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			out = append(out, htmlpkg.UnescapeString(m[1]))
		}
	}
	return out
}

func htmlImageResource(text, fallbackTitle string) chaoxingResource {
	if !strings.Contains(strings.ToLower(text), "ans-cc") {
		return chaoxingResource{}
	}
	for _, m := range regexp.MustCompile(`(?is)<img[^>]+src=["']([^"']+\.(?:jpg|jpeg|png|webp|gif)(?:\?[^"']*)?)["']`).FindAllStringSubmatch(text, -1) {
		if u := normalizeURL(htmlpkg.UnescapeString(m[1])); isHTTPURL(u) {
			return chaoxingResource{Title: firstNonEmpty(fallbackTitle, "图文"), Kind: "file", RawURL: u, Ext: fileExt(u)}
		}
	}
	return chaoxingResource{}
}

func streamEntry(title, rawURL, format string, headers map[string]string, extra map[string]any) *extractor.MediaInfo {
	if !isHTTPURL(rawURL) {
		return nil
	}
	format = firstNonEmpty(format, mediaFormat(rawURL, "mp4"))
	return &extractor.MediaInfo{
		Site:  "chaoxing",
		Title: util.SanitizeFilename(firstNonEmpty(title, "chaoxing_resource")),
		Streams: map[string]extractor.Stream{"default": {
			Quality:   "default",
			URLs:      []string{rawURL},
			Format:    format,
			NeedMerge: format == "m3u8",
			Headers:   headers,
		}},
		Extra: compactExtra(extra),
	}
}
