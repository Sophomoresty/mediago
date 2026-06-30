package plaso

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor/shared"
)

type plasoSTS struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	Region          string
	Bucket          string
	Host            string
	Endpoint        string
	Raw             any
}

type plistCand struct {
	url, duration, startMS string
	audio                  bool
}

func (s *plasoSession) fetchAliPlaySource(f fileItem) plasoSource {
	id := firstNonEmpty(f.ID, f.MyID, f.VideoID)
	if id == "" && firstNonEmpty(f.Location, f.LocationPath) == "" {
		return plasoSource{}
	}
	for _, data := range s.aliPlayRequestDataVariants(f) {
		if v, err := s.postJSON(s.eps.url(m3u8Path), data); err == nil {
			if src := s.sourceFromPlayInfo(v, "aliyun_play_info"); src.URL != "" {
				src.Extra = mergeExtra(src.Extra, map[string]any{"api": s.eps.url(m3u8Path), "request": data})
				return src
			}
		}
	}
	return s.fetchAliSTSPlaySource(f)
}

func (s *plasoSession) fetchAliSTSPlaySource(f fileItem) plasoSource {
	videoID := firstNonEmpty(f.VideoID, f.Vid)
	if videoID == "" {
		return plasoSource{}
	}
	payload := shared.AliyunPlayPayload{}
	for _, api := range []string{s.eps.url(stsPath), s.eps.url(stsPreviewPath)} {
		v, err := s.postJSON(api, s.playRequestData(f))
		if err != nil {
			continue
		}
		walk(v, func(m map[string]any) {
			if payload.AccessKeyID != "" {
				return
			}
			candidate := shared.AliyunPayloadFromMap(m, m)
			if candidate.AccessKeyID != "" && candidate.AccessKeySecret != "" {
				if candidate.Region == "" {
					candidate.Region = firstNonEmpty(firstText(m, "region", "regionId", "Region"), "cn-shanghai")
				}
				payload = candidate
			}
		})
		if payload.AccessKeyID != "" {
			break
		}
	}
	if payload.AccessKeyID == "" {
		return plasoSource{}
	}
	info, err := shared.AliyunResolvePlayInfo(s.client, payload, videoID, shared.AliyunPlayOptions{
		Referer:           s.eps.base,
		Origin:            s.eps.base,
		Quality:           s.quality,
		PreferDefinitions: aliyunPreferDefinitions(s.quality),
		Headers:           streamHeaders(s.headers),
		FetchM3U8:         true,
		RewriteM3U8Keys:   true,
	})
	if err != nil {
		return plasoSource{}
	}
	m3u8Text := info.M3U8Text
	if m3u8Text != "" {
		m3u8Text = absolutizeM3U8Text(m3u8Text, info.URL)
	}
	return plasoSource{URL: info.URL, Format: firstNonEmpty(info.Format, formatOf(info.URL, f.Type)), Quality: firstNonEmpty(info.Definition, "best"), SourceType: info.SourceType, NeedMerge: info.NeedMerge, Size: info.Size, M3U8Text: m3u8Text, Extra: map[string]any{"aliyun_vid": videoID, "aliyun_api": info.APIURL, "encrypt_type": info.EncryptType}}
}

func (s *plasoSession) fetchPolyvSource(f fileItem) plasoSource {
	vid := firstNonEmpty(f.Vid)
	signInfo := map[string]string{}
	if vid == "" && looksPolyvVID(f.VideoID) {
		vid = f.VideoID
	}
	if vid == "" && f.ID != "" {
		if v, err := s.postJSON(s.eps.url(polySignPath), s.playRequestData(f)); err == nil {
			vid = findFirst(v, "vid", "polyvVid", "polyv_vid", "videoId", "video_id")
			signInfo = plasoPolyvSignInfo(v)
		}
	}
	if vid == "" {
		return plasoSource{}
	}
	if sec, err := shared.PolyvResolveSecure(s.client, vid, s.headers); err == nil {
		if manifest, err := shared.PolyvPickBestManifest(sec); err == nil {
			src := plasoSource{URL: s.normalizeMediaURL(manifest, ""), Format: "m3u8", Quality: "best", SourceType: "polyv", NeedMerge: true, Extra: map[string]any{"polyv_vid": vid}}
			if text, err := s.client.GetString(src.URL, streamHeaders(s.headers)); err == nil && strings.HasPrefix(strings.TrimSpace(text), "#EXTM3U") {
				text = absolutizeM3U8Text(text, src.URL)
				if rewritten, err := shared.PolyvRewriteM3U8KeysWithOptions(s.client, text, shared.PolyvRewriteOptions{
					Token:       sec.PlayToken(),
					Referer:     s.eps.base,
					ManifestURL: src.URL,
					SeedConst:   sec.SeedConst(),
				}); err == nil {
					src.M3U8Text = rewritten
				}
			}
			return src
		}
	}
	polyReq := map[string]string{"vid": vid}
	for k, v := range signInfo {
		if v != "" {
			polyReq[k] = v
		}
	}
	if body, err := s.client.PostForm(polyVideoURL, polyReq, s.headers); err == nil {
		if m := mediaRe.FindString(body); m != "" {
			m = strings.ReplaceAll(m, `\/`, `/`)
			return plasoSource{URL: s.normalizeMediaURL(m, ""), Format: formatOf(m, f.Type), Quality: "best", SourceType: "polyv_video_info", NeedMerge: strings.Contains(strings.ToLower(m), ".m3u8"), Extra: map[string]any{"polyv_vid": vid}}
		}
	}
	req := map[string]string{"fileId": f.ID, "id": f.ID, "vid": vid}
	for k, v := range signInfo {
		if v != "" {
			req[k] = v
		}
	}
	if v, err := s.postJSON(s.eps.url(m3u8SignPath), req); err == nil {
		if src := s.sourceFromPlayInfo(v, "polyv_sign"); src.URL != "" {
			src.Extra = mergeExtra(src.Extra, map[string]any{"polyv_vid": vid, "api": s.eps.url(m3u8SignPath)})
			return src
		}
	}
	return plasoSource{}
}

func (s *plasoSession) fetchPlistSource(f fileItem) plasoSource {
	for _, raw := range []string{f.LocationPath, f.Location, f.URL} {
		plistURL := s.normalizeMediaURL(raw, "")
		if plistURL == "" || !strings.HasPrefix(plistURL, "http") || !isLikelyPlistURL(plistURL) {
			continue
		}
		text, err := s.client.GetString(plistURL, streamHeaders(s.headers))
		if err != nil {
			continue
		}
		var root any
		if err := json.Unmarshal([]byte(text), &root); err == nil {
			if src := s.pickPlistMedia(root, plistURL, f); src.URL != "" {
				src.Extra = mergeExtra(src.Extra, map[string]any{"plist_url": plistURL})
				return src
			}
		}
		if m := mediaRe.FindString(text); m != "" {
			m = s.normalizeMediaURL(m, plistURL)
			fmtv := formatOf(m, f.Type)
			return plasoSource{URL: m, Format: fmtv, SourceType: "plist_regex", NeedMerge: fmtv == "m3u8", Extra: map[string]any{"plist_url": plistURL}}
		}
	}
	if src := s.fetchSignedPlistSource(f); src.URL != "" {
		return src
	}
	return plasoSource{}
}

func (s *plasoSession) fetchSignedPlistSource(f fileItem) plasoSource {
	location := strings.Trim(strings.TrimSpace(f.Location), "/")
	if location == "" || strings.HasPrefix(strings.ToLower(location), "http") || strings.Contains(strings.ToLower(location), ".pdf") {
		return plasoSource{}
	}
	sts := s.fetchCourseSTS(f)
	if sts.AccessKeyID == "" || sts.AccessKeySecret == "" {
		return plasoSource{}
	}
	root := plasoPlistStorageRoot(f.LocationPath)
	objectKey := root + "/" + location + "/info.plist"
	plistURL := buildPlasoCourseSTSSignedURL(objectKey, sts, time.Now().Add(time.Hour))
	if plistURL == "" {
		return plasoSource{}
	}
	text, err := s.client.GetString(plistURL, streamHeaders(s.headers))
	if err != nil {
		return plasoSource{}
	}
	var rootJSON any
	if err := json.Unmarshal([]byte(text), &rootJSON); err != nil {
		return plasoSource{}
	}
	src := s.pickPlistMedia(rootJSON, plistURL, f)
	if src.URL != "" {
		src.Extra = mergeExtra(src.Extra, map[string]any{"plist_url": plistURL, "plist_object_key": objectKey, "plist_root": plasoPlistStorageRoot(f.LocationPath)})
	}
	return src
}

func (s *plasoSession) pickPlistMedia(root any, baseURL string, f fileItem) plasoSource {
	var cands []plistCand
	walk(root, func(m map[string]any) {
		if u, _ := pickPlayURL(m, s.quality); u != "" {
			cands = append(cands, plistCand{url: s.normalizeMediaURL(u, baseURL), audio: isAudioMap(m), duration: firstText(m, "duration"), startMS: firstText(m, "start_ms", "startMs")})
		}
		for _, entry := range asAnyList(m["media"]) {
			mm := asAnyMap(entry)
			if len(mm) == 0 {
				continue
			}
			u := firstText(mm, "m3u8Url", "m3u8URL", "url", "path", "location", "src")
			if u == "" {
				u, _ = pickPlayURL(mm, s.quality)
			}
			if u != "" {
				cands = append(cands, plistCand{url: s.normalizeMediaURL(u, baseURL), audio: isAudioMap(mm), duration: firstText(mm, "duration"), startMS: firstText(mm, "start_ms", "startMs")})
			}
		}
	})
	for _, entry := range s.plistTimelineCandidates(root, f) {
		cands = append(cands, entry)
	}
	var picked, audio plistCand
	var videoURLs []string
	for _, c := range cands {
		if c.url == "" {
			continue
		}
		if c.audio {
			if audio.url == "" {
				audio = c
			}
			continue
		}
		if formatOf(c.url, f.Type) == "m3u8" {
			videoURLs = append(videoURLs, c.url)
		}
		if picked.url == "" {
			picked = c
		}
	}
	if len(videoURLs) > 1 {
		if text := s.mergeM3U8TextList(videoURLs); text != "" {
			return plasoSource{URL: m3u8DataURL(text), Format: "m3u8", SourceType: "plist_joined", NeedMerge: true, M3U8Text: text, AudioURL: audio.url, Extra: map[string]any{"joined_count": len(videoURLs)}}
		}
	}
	if picked.url == "" {
		picked = audio
	}
	if picked.url == "" {
		return plasoSource{}
	}
	fmtv := formatOf(picked.url, f.Type)
	src := plasoSource{URL: picked.url, Format: fmtv, SourceType: "plist", NeedMerge: fmtv == "m3u8", AudioURL: audio.url, Extra: map[string]any{"duration": picked.duration, "start_ms": picked.startMS}}
	if fmtv == "m3u8" {
		if text, err := s.client.GetString(picked.url, streamHeaders(s.headers)); err == nil && strings.HasPrefix(strings.TrimSpace(text), "#EXTM3U") {
			src.M3U8Text = absolutizeM3U8Text(text, picked.url)
		}
	}
	return src
}

func (s *plasoSession) plistTimelineCandidates(root any, f fileItem) []plistCand {
	var out []plistCand
	var visit func(any)
	visit = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for _, key := range []string{"recordUrl", "m3u8Url", "m3u8URL", "url", "audioPath"} {
				if raw := firstText(t, key); strings.Contains(strings.ToLower(raw), ".m3u8") || strings.Contains(strings.ToLower(raw), ".mp4") || strings.Contains(strings.ToLower(raw), ".mp3") || strings.Contains(strings.ToLower(raw), ".m4a") || strings.Contains(strings.ToLower(raw), ".aac") {
					url := buildPlasoEventAudioURL(raw)
					if key != "audioPath" {
						url = buildPlistMediaURL(f.Location, raw, f.LocationPath)
					}
					out = append(out, plistCand{url: url, audio: key == "audioPath" || strings.HasPrefix(plistTrackName(raw), "a")})
				}
			}
			for _, x := range t {
				visit(x)
			}
		case []any:
			if c, ok := parsePlistMediaArray(t, f); ok {
				out = append(out, c)
			}
			for _, x := range t {
				visit(x)
			}
		}
	}
	visit(root)
	return out
}

func parsePlistMediaArray(v []any, f fileItem) (plistCand, bool) {
	var out plistCand
	if len(v) < 3 {
		return out, false
	}
	start := valueText(v[0])
	code := valueText(v[1])
	if code == "37" {
		for _, x := range v[2:] {
			if mediaPath := mediaPathFromAny(x); mediaPath != "" {
				if isAudioMediaPath(mediaPath) {
					out.url = buildPlasoEventAudioURL(mediaPath)
				} else {
					out.url = buildPlistMediaURL(f.Location, mediaPath, f.LocationPath)
				}
				out.startMS = start
				out.audio = isAudioMediaPath(mediaPath)
				return out, true
			}
		}
		return out, false
	}
	if len(v) >= 4 {
		mediaPath := valueText(v[3])
		if isMediaPath(mediaPath) || isAudioMediaPath(mediaPath) {
			if isAudioMediaPath(mediaPath) || code == "1" {
				out.url = buildPlasoEventAudioURL(mediaPath)
			} else {
				out.url = buildPlistMediaURL(f.Location, mediaPath, f.LocationPath)
			}
			out.startMS = start
			out.duration = valueText(v[2])
			out.audio = isAudioMediaPath(mediaPath) || code == "1"
			return out, true
		}
	}
	return out, false
}

func mediaPathFromAny(v any) string {
	switch t := v.(type) {
	case string:
		if isMediaPath(t) {
			return strings.TrimSpace(t)
		}
	case []any:
		for _, x := range t {
			if s := mediaPathFromAny(x); s != "" {
				return s
			}
		}
	case []string:
		for _, s := range t {
			if isMediaPath(s) {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func isMediaPath(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	return strings.Contains(l, ".m3u8") || strings.Contains(l, ".mp4") || strings.Contains(l, ".flv") ||
		strings.Contains(l, ".mov") || strings.Contains(l, ".m4v") || strings.Contains(l, ".ts") ||
		strings.Contains(l, ".mp3") || strings.Contains(l, ".m4a") || strings.Contains(l, ".aac") || strings.Contains(l, ".wav")
}

func isAudioMediaPath(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	return strings.Contains(l, ".mp3") || strings.Contains(l, ".m4a") || strings.Contains(l, ".aac") || strings.Contains(l, ".wav") || strings.HasPrefix(plistTrackName(s), "a")
}

func plasoPlistStorageRoot(locationPath string) string {
	lp := strings.ToLower(strings.Trim(strings.TrimSpace(locationPath), "/"))
	if lp == "nmini" || lp == "mini" {
		return "mini"
	}
	return "liveclass/plaso"
}

func buildPlistMediaURL(location, mediaPath, locationPath string) string {
	mediaPath = strings.TrimSpace(strings.ReplaceAll(mediaPath, `\/`, `/`))
	if mediaPath == "" {
		return ""
	}
	if strings.HasPrefix(mediaPath, "http://") || strings.HasPrefix(mediaPath, "https://") {
		return mediaPath
	}
	if strings.HasPrefix(mediaPath, "//") {
		return "https:" + mediaPath
	}
	return "https://filecdn.plaso.com/" + plasoPlistStorageRoot(locationPath) + "/" + strings.Trim(strings.TrimSpace(location), "/") + "/" + strings.TrimLeft(mediaPath, "/")
}

func plistTrackName(mediaPath string) string {
	p := strings.Trim(strings.TrimSpace(mediaPath), "/")
	if p == "" {
		return ""
	}
	if i := strings.Index(p, "/"); i >= 0 {
		return strings.ToLower(strings.TrimSpace(p[:i]))
	}
	return ""
}

func (s *plasoSession) mergeM3U8TextList(urls []string) string {
	var bodies []string
	maxTarget := 0
	maxVersion := 3
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		text := raw
		baseURL := ""
		if !strings.HasPrefix(strings.TrimSpace(raw), "#EXTM3U") {
			got, err := s.client.GetString(raw, streamHeaders(s.headers))
			if err != nil {
				continue
			}
			text = got
			baseURL = raw
		}
		if !strings.HasPrefix(strings.TrimSpace(text), "#EXTM3U") {
			continue
		}
		text = absolutizeM3U8Text(text, baseURL)
		if n := m3u8IntTag(text, "#EXT-X-TARGETDURATION:"); n > maxTarget {
			maxTarget = n
		}
		if n := m3u8IntTag(text, "#EXT-X-VERSION:"); n > maxVersion {
			maxVersion = n
		}
		bodies = append(bodies, text)
	}
	if len(bodies) == 0 {
		return ""
	}
	if maxTarget <= 0 {
		maxTarget = 16
	}
	var out []string
	out = append(out, "#EXTM3U", "#EXT-X-VERSION:"+strconv.Itoa(maxVersion), "#EXT-X-MEDIA-SEQUENCE:0", "#EXT-X-ALLOW-CACHE:YES", "#EXT-X-TARGETDURATION:"+strconv.Itoa(maxTarget))
	for i, body := range bodies {
		if i > 0 {
			out = append(out, "#EXT-X-DISCONTINUITY")
		}
		for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			upper := strings.ToUpper(line)
			if line == "" || upper == "#EXTM3U" || strings.HasPrefix(upper, "#EXT-X-VERSION:") ||
				strings.HasPrefix(upper, "#EXT-X-MEDIA-SEQUENCE:") || strings.HasPrefix(upper, "#EXT-X-ALLOW-CACHE:") ||
				strings.HasPrefix(upper, "#EXT-X-TARGETDURATION:") || strings.HasPrefix(upper, "#EXT-X-ENDLIST") {
				continue
			}
			out = append(out, line)
		}
	}
	out = append(out, "#EXT-X-ENDLIST")
	return strings.Join(out, "\n") + "\n"
}

func m3u8IntTag(text, prefix string) int {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), prefix) {
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
			return n
		}
	}
	return 0
}

func absolutizeM3U8Text(text, baseURL string) string {
	if baseURL == "" {
		return text
	}
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trim, "#EXT-X-KEY:") {
			out = append(out, absolutizeM3U8KeyLine(line, baseURL))
			continue
		}
		if strings.HasPrefix(trim, "#") {
			out = append(out, line)
			continue
		}
		out = append(out, resolveM3U8LikeURI(trim, baseURL))
	}
	return strings.Join(out, "\n")
}

func absolutizeM3U8KeyLine(line, baseURL string) string {
	uri := extractM3U8QuotedURI(line)
	if uri == "" || strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://") || strings.HasPrefix(uri, "data:") {
		return line
	}
	return strings.Replace(line, uri, resolveM3U8LikeURI(uri, baseURL), 1)
}

func extractM3U8QuotedURI(line string) string {
	idx := strings.Index(strings.ToUpper(line), "URI=")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[idx+4:])
	if strings.HasPrefix(rest, `"`) {
		rest = rest[1:]
		if end := strings.Index(rest, `"`); end >= 0 {
			return rest[:end]
		}
		return ""
	}
	if end := strings.Index(rest, ","); end >= 0 {
		return rest[:end]
	}
	return rest
}

func resolveM3U8LikeURI(raw, baseURL string) string {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "data:") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	base, err1 := url.Parse(baseURL)
	ref, err2 := url.Parse(raw)
	if err1 == nil && err2 == nil {
		return base.ResolveReference(ref).String()
	}
	return raw
}

func (s *plasoSession) buildDirectDocumentSource(f fileItem) plasoSource {
	if !isDocumentLike(f) {
		return plasoSource{}
	}
	raw := firstNonEmpty(f.URL, f.LocationPath, f.Location)
	if raw == "" {
		return plasoSource{}
	}
	if direct := s.directSource(f, raw); direct.URL != "" {
		direct.SourceType = "direct_file"
		return direct
	}
	raw = normalizePlasoStorageObjectKey(f.Location, firstNonEmpty(f.LocationPath, raw))
	sts := s.fetchCourseSTS(f)
	if sts.AccessKeyID == "" || sts.AccessKeySecret == "" {
		return plasoSource{}
	}
	signedURL := buildPlasoCourseSTSSignedURL(raw, sts, time.Now().Add(time.Hour))
	if signedURL == "" {
		return plasoSource{}
	}
	return plasoSource{URL: signedURL, Format: formatOf(raw, f.Type), SourceType: "oss_sts_file", Size: f.Size, Extra: map[string]any{"oss_bucket": sts.Bucket, "oss_host": sts.Host, "oss_region": sts.Region}}
}

func (s *plasoSession) fetchCourseSTS(f fileItem) plasoSTS {
	var out plasoSTS
	for _, data := range []map[string]string{s.playRequestData(f), map[string]string{"id": "liveclass"}} {
		for _, api := range []string{s.eps.url(stsPath), s.eps.url(stsPreviewPath)} {
			v, err := s.postJSON(api, data)
			if err != nil {
				continue
			}
			walk(v, func(m map[string]any) {
				if out.AccessKeyID != "" {
					return
				}
				ak := firstText(m, "AccessKeyId", "AccessKeyID", "accessKeyId", "accessKeyID", "access_key_id", "accessKey", "access_key", "accessId", "access_id", "OSSAccessKeyId", "id")
				sk := firstText(m, "AccessKeySecret", "accessKeySecret", "access_key_secret", "accessSecret", "access_secret", "secret")
				if ak == "" || sk == "" {
					return
				}
				out = plasoSTS{AccessKeyID: ak, AccessKeySecret: sk, SecurityToken: firstText(m, "SecurityToken", "securityToken", "security_token", "sts_token", "token"), Region: normalizeOSSRegion(firstText(m, "region", "Region", "regionId", "domain_region")), Bucket: firstText(m, "bucket", "bucketName", "Bucket"), Host: firstText(m, "host", "Host", "domain", "ossHost", "downloadHost", "accelerateDomain"), Endpoint: firstText(m, "endpoint", "Endpoint", "ossEndpoint"), Raw: m}
				out.Host = normalizeHost(out.Host)
				out.Endpoint = normalizeHost(out.Endpoint)
				if out.Region == "" {
					out.Region = normalizeOSSRegion(regionFromEndpoint(firstNonEmpty(out.Endpoint, out.Host)))
				}
				if out.Host == "" {
					out.Host = "file.plaso.com"
				}
				if out.Bucket == "" {
					out.Bucket = "file-plaso"
				}
				if out.Region == "" {
					out.Region = "cn-hangzhou"
				}
			})
			if out.AccessKeyID != "" {
				break
			}
		}
		if out.AccessKeyID != "" {
			break
		}
	}
	return out
}

func (s *plasoSession) sourceFromPlayInfo(v any, sourceType string) plasoSource {
	u, quality := pickPlayURL(v, s.quality)
	u = s.normalizeMediaURL(u, s.eps.base)
	if u == "" {
		return plasoSource{}
	}
	fmtv := formatOf(u, "video")
	src := plasoSource{URL: u, Format: fmtv, Quality: firstNonEmpty(quality, "best"), SourceType: sourceType, NeedMerge: fmtv == "m3u8", Size: parseSize(findFirstValue(v, "size", "Size", "fileSize"))}
	if fmtv == "m3u8" {
		if text, err := s.client.GetString(u, streamHeaders(s.headers)); err == nil && strings.HasPrefix(strings.TrimSpace(text), "#EXTM3U") {
			src.M3U8Text = absolutizeM3U8Text(text, u)
		}
	}
	return src
}

func (s *plasoSession) playRequestData(f fileItem) map[string]string {
	data := map[string]string{}
	for _, kv := range [][2]string{
		{"fileId", f.ID}, {"id", f.ID}, {"myid", f.MyID}, {"myId", f.MyID},
		{"location", f.Location}, {"locationPath", f.LocationPath},
		{"storageId", f.StorageID}, {"storage_id", f.StorageID},
		{"vid", f.Vid}, {"videoId", f.VideoID},
	} {
		if strings.TrimSpace(kv[1]) != "" {
			data[kv[0]] = strings.TrimSpace(kv[1])
		}
	}
	return data
}

func (s *plasoSession) normalizeMediaURL(raw, baseURL string) string {
	u := strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	u = strings.Trim(u, "\"'")
	if u == "" {
		return ""
	}
	if decoded, err := url.QueryUnescape(u); err == nil && strings.Contains(decoded, "://") {
		u = decoded
	}
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	if baseURL != "" {
		base, err1 := url.Parse(baseURL)
		ref, err2 := url.Parse(u)
		if err1 == nil && err2 == nil {
			return base.ResolveReference(ref).String()
		}
	}
	if strings.HasPrefix(u, "/") {
		return s.eps.base + u
	}
	return u
}

func (s *plasoSession) buildPlayerSource(f fileItem) plasoSource {
	if f.ID == "" || isDocumentLike(f) || (!isVideoLike(f) && firstNonEmpty(f.URL, f.Location, f.LocationPath, f.Vid, f.VideoID) != "") {
		return plasoSource{}
	}
	playerURL := plasoPlayerURL + url.QueryEscape(f.ID)
	return plasoSource{
		URL:        playerURL,
		Format:     "html",
		Quality:    "player",
		SourceType: "player_html",
		Extra: map[string]any{
			"player_url":           playerURL,
			"player_url_encrypted": plasoPlayerURLEncrypt(playerURL),
			"render_required":      true,
		},
	}
}

func buildPlasoCourseSTSSignedURL(raw string, sts plasoSTS, expiresAt time.Time) string {
	if firstNonEmpty(sts.Region, regionFromEndpoint(firstNonEmpty(sts.Endpoint, sts.Host))) != "" {
		if signed := buildPlasoCourseSTSV4SignedURL(raw, sts, expiresAt); signed != "" {
			return signed
		}
	}
	return buildPlasoCourseSTSV1SignedURL(raw, sts, expiresAt)
}

func buildPlasoCourseSTSV1SignedURL(raw string, sts plasoSTS, expiresAt time.Time) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if raw == "" {
		return ""
	}
	var u *url.URL
	var err error
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "//") {
		if strings.HasPrefix(raw, "//") {
			raw = "https:" + raw
		}
		u, err = url.Parse(raw)
		if err != nil {
			return ""
		}
	} else {
		object := strings.TrimLeft(raw, "/")
		host := firstNonEmpty(sts.Host, buildOSSHost(sts.Bucket, sts.Endpoint, sts.Region))
		if host == "" {
			return ""
		}
		u = &url.URL{Scheme: "https", Host: host, Path: "/" + object}
	}
	expires := strconv.FormatInt(expiresAt.Unix(), 10)
	bucket := firstNonEmpty(sts.Bucket, bucketFromHost(u.Host))
	canonical := u.EscapedPath()
	if bucket != "" && !strings.HasPrefix(canonical, "/"+bucket+"/") {
		canonical = "/" + bucket + canonical
	}
	if sts.SecurityToken != "" {
		canonical += "?security-token=" + url.QueryEscape(sts.SecurityToken)
	}
	toSign := "GET\n\n\n" + expires + "\n" + canonical
	mac := hmac.New(sha1.New, []byte(sts.AccessKeySecret))
	_, _ = mac.Write([]byte(toSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	q := u.Query()
	q.Set("OSSAccessKeyId", sts.AccessKeyID)
	q.Set("Expires", expires)
	q.Set("Signature", sig)
	if sts.SecurityToken != "" {
		q.Set("security-token", sts.SecurityToken)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func buildPlasoCourseSTSV4SignedURL(raw string, sts plasoSTS, expiresAt time.Time) string {
	u, ok := plasoOSSURL(raw, sts)
	if !ok {
		return ""
	}
	region := firstNonEmpty(sts.Region, regionFromEndpoint(firstNonEmpty(sts.Endpoint, u.Host)))
	if sts.AccessKeyID == "" || sts.AccessKeySecret == "" || region == "" {
		return ""
	}
	now := expiresAt.Add(-time.Hour).UTC()
	dateTime := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	scope := date + "/" + region + "/oss/aliyun_v4_request"
	q := u.Query()
	q.Set("x-oss-signature-version", "OSS4-HMAC-SHA256")
	q.Set("x-oss-date", dateTime)
	q.Set("x-oss-expires", "3600")
	q.Set("x-oss-credential", sts.AccessKeyID+"/"+scope)
	if sts.SecurityToken != "" {
		q.Set("x-oss-security-token", sts.SecurityToken)
	}
	canonicalQuery := q.Encode()
	canonicalHeaders := "host:" + strings.ToLower(u.Host) + "\n"
	canonicalRequest := strings.Join([]string{
		"GET",
		firstNonEmpty(u.EscapedPath(), "/"),
		canonicalQuery,
		canonicalHeaders,
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	hashed := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"OSS4-HMAC-SHA256",
		dateTime,
		scope,
		hex.EncodeToString(hashed[:]),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(ossV4SigningKey(sts.AccessKeySecret, date, region), []byte(stringToSign)))
	q.Set("x-oss-signature", signature)
	u.RawQuery = q.Encode()
	return u.String()
}

func plasoOSSURL(raw string, sts plasoSTS) (*url.URL, bool) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if raw == "" {
		return nil, false
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		return u, err == nil
	}
	host := firstNonEmpty(sts.Host, buildOSSHost(sts.Bucket, sts.Endpoint, sts.Region))
	if host == "" {
		return nil, false
	}
	return &url.URL{Scheme: "https", Host: host, Path: "/" + strings.TrimLeft(raw, "/")}, true
}

func ossV4SigningKey(secret, date, region string) []byte {
	kDate := hmacSHA256([]byte("aliyun_v4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte("oss"))
	return hmacSHA256(kService, []byte("aliyun_v4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}
