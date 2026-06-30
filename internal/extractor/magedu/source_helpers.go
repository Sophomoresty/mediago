package magedu

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

var (
	mageduHTTPURLRe  = regexp.MustCompile(`https?://[^\s"'<>]+|//[^\s"'<>]+`)
	mageduPolyvVIDRe = regexp.MustCompile(`(?i)^[0-9a-f]{32}(?:_[a-z0-9])?$`)
)

type mageduFileCandidate struct {
	URL, Title, Format string
	Size               int64
}

func mageduDirectVideoEntry(sess *mageduSession, item mageduItem) *extractor.MediaInfo {
	format := firstText(item.FileFmt, mediaExt(item.FileURL))
	return &extractor.MediaInfo{
		Site:  "magedu",
		Title: item.Title,
		Streams: map[string]extractor.Stream{"default": {
			Quality:   "default",
			URLs:      []string{item.FileURL},
			Format:    format,
			Size:      item.Size,
			NeedMerge: format == "m3u8",
			Headers:   sess.Headers,
		}},
		Extra: map[string]any{"source_type": "direct", "section_id": item.SectionID, "video_storage_id": item.StorageID},
	}
}

func mageduPolyvPDXEntry(c *util.Client, item mageduItem, polyvVID, pdxURL, token string, sec *shared.PolyvSecure, headers map[string]string) (*extractor.MediaInfo, error) {
	info, err := shared.PolyvResolvePDX(c, shared.PolyvPDXOptions{
		VideoID:       item.VideoID,
		PDXURL:        pdxURL,
		PlaySafeToken: token,
		Headers:       headers,
		Secure:        sec,
	})
	if err != nil {
		return nil, fmt.Errorf("magedu polyv pdx %s: %w", polyvVID, err)
	}
	extra := map[string]any{
		"video_id":         item.VideoID,
		"polyv_vid":        polyvVID,
		"section_id":       item.SectionID,
		"video_storage_id": item.StorageID,
		"playSafeToken":    token,
		"token":            info.PlaySafeToken,
		"source_type":      "polyv_pdx",
		"m3u8_meta":        info.M3U8Meta(),
	}
	for k, v := range info.ExtraMap() {
		extra[k] = v
	}
	return &extractor.MediaInfo{
		Site:  "magedu",
		Title: item.Title,
		Streams: map[string]extractor.Stream{"default": {
			Quality:   "default",
			URLs:      []string{mageduM3U8DataURL(info.M3U8Text)},
			Format:    "m3u8",
			Size:      item.Size,
			NeedMerge: true,
			Headers:   headers,
			Extra: map[string]any{
				"cryptor":   "polyv_pdx",
				"m3u8_meta": info.M3U8Meta(),
			},
		}},
		Extra: extra,
	}, nil
}

func mageduSectionMaterials(c *util.Client, sess *mageduSession, sec map[string]any, prefix []int) []mageduItem {
	sectionID := firstText(sec["id"], sec["sectionId"])
	baseTitle := mageduSectionTitle(sec, "资料")
	seen := map[string]bool{}
	if firstText(sec["sectionType"]) == "2" {
		if u := normalizeURL(firstText(sec["content"])); u != "" {
			seen[u] = true
		}
	}

	materialData := map[string]any{}
	loadedMaterialData := false
	if intOf(sec["materialCount"]) > 0 && sectionID != "" && c != nil && sess != nil {
		if resp, err := mageduGetJSON(c, mageduAPIURL(urlMaterial, urlKEAPIBase), map[string]string{"sectionId": sectionID}, sess.Headers); err == nil {
			data := mageduData(resp)
			if m := mapAny(data); len(m) > 0 {
				materialData = m
				loadedMaterialData = true
			} else if rs := records(data); len(rs) > 0 {
				materialData = map[string]any{"classOther": rs}
				loadedMaterialData = true
			}
		}
	}
	if len(materialData) == 0 {
		materialData = sec
	}

	fields := []struct {
		keys  []string
		label string
	}{
		{[]string{"classNotes", "class_notes", "courseware", "coursewareUrl", "courseware_url", "notes"}, "课件"},
		{[]string{"afterClass", "after_class", "afterClassUrl", "after_class_url", "homework"}, "课后资料"},
		{[]string{"classOther", "class_other", "other", "otherUrl", "other_url", "attachments", "files", "materials"}, "其它资料"},
	}

	var out []mageduItem
	fieldIndex := 0
	for _, field := range fields {
		value := firstMaterialValue(materialData, field.keys...)
		if value == nil && loadedMaterialData {
			value = firstMaterialValue(sec, field.keys...)
		}
		cands := mageduFileCandidates(value)
		if len(cands) == 0 {
			continue
		}
		fieldIndex++
		for i, cand := range cands {
			u := normalizeURL(cand.URL)
			if !mageduLooksLikeDownloadURL(u) || seen[u] {
				continue
			}
			seen[u] = true
			name := firstText(cand.Title, fmt.Sprintf("%s-%s", baseTitle, field.label))
			p := append(append([]int{}, prefix...), fieldIndex)
			if len(cands) > 1 {
				p = append(p, i+1)
			}
			out = append(out, mageduItem{Kind: "file", Title: indexedFileTitle(p, name), FileURL: u, FileFmt: firstText(cand.Format, mediaExt(u)), Size: cand.Size})
		}
	}
	return out
}

func firstMaterialValue(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && firstText(v) != "" {
			return v
		}
	}
	return nil
}

func mageduFileCandidates(v any) []mageduFileCandidate {
	var out []mageduFileCandidate
	seen := map[string]bool{}
	var walk func(any, string)
	walk = func(v any, inheritedTitle string) {
		switch x := v.(type) {
		case nil:
			return
		case string:
			for _, raw := range mageduURLsFromText(x) {
				u := normalizeURL(raw)
				if u == "" || seen[u] {
					continue
				}
				seen[u] = true
				out = append(out, mageduFileCandidate{URL: u, Title: inheritedTitle, Format: mediaExt(u)})
			}
		case []any:
			for _, it := range x {
				walk(it, inheritedTitle)
			}
		case map[string]any:
			title := firstText(x["title"], x["name"], x["fileName"], x["filename"], x["file_name"], x["materialName"], x["attachmentName"], inheritedTitle)
			format := firstText(x["fileFmt"], x["fileFormat"], x["fileType"], x["type"], x["suffix"], x["ext"])
			for _, key := range []string{"url", "fileUrl", "fileURL", "file_url", "downloadUrl", "download_url", "path", "href", "src", "content", "attachmentUrl", "attachment_url", "resourceUrl", "resource_url"} {
				for _, raw := range mageduURLsFromText(firstText(x[key])) {
					u := normalizeURL(raw)
					if u == "" || seen[u] {
						continue
					}
					seen[u] = true
					out = append(out, mageduFileCandidate{URL: u, Title: title, Format: firstText(format, mediaExt(u)), Size: int64(numOf(firstText(x["size"], x["fileSize"], x["file_size"])))})
				}
			}
			for key, child := range x {
				if key == "url" || key == "fileUrl" || key == "fileURL" || key == "file_url" || key == "downloadUrl" || key == "download_url" || key == "path" || key == "href" || key == "src" {
					continue
				}
				walk(child, title)
			}
		}
	}
	walk(v, "")
	return out
}

func mageduURLsFromText(text string) []string {
	text = strings.TrimSpace(strings.Trim(text, "\"'"))
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) == nil {
			return candidatesToURLs(mageduFileCandidates(decoded))
		}
	}
	matches := mageduHTTPURLRe.FindAllString(text, -1)
	if len(matches) > 0 {
		for i := range matches {
			matches[i] = strings.TrimRight(matches[i], `,.;)]}`)
		}
		return matches
	}
	if strings.HasPrefix(text, "/") && mageduLooksLikeDownloadURL(text) {
		return []string{text}
	}
	return nil
}

func candidatesToURLs(cands []mageduFileCandidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.URL)
	}
	return out
}

func mageduSectionTitle(sec map[string]any, fallback string) string {
	title := firstText(sec["title"], sec["name"], sec["sectionName"], fallback)
	title = regexp.MustCompile(`(?i)\.(?:mp4|m3u8)$`).ReplaceAllString(title, "")
	return firstText(title, fallback)
}

func mageduLooksLikeDownloadURL(raw string) bool {
	u := strings.TrimSpace(raw)
	if u == "" {
		return false
	}
	lu := strings.ToLower(u)
	if strings.HasPrefix(lu, "http://") || strings.HasPrefix(lu, "https://") || strings.HasPrefix(u, "//") {
		return true
	}
	if strings.HasPrefix(u, "/") {
		return strings.Contains(lu, ".pdf") || strings.Contains(lu, ".ppt") || strings.Contains(lu, ".doc") || strings.Contains(lu, ".xls") || strings.Contains(lu, ".zip") || strings.Contains(lu, ".rar") || strings.Contains(lu, ".mp4") || strings.Contains(lu, ".m3u8") || strings.Contains(lu, ".mp3")
	}
	return strings.HasPrefix(lu, "data:application/")
}

func mageduLooksLikeVideoURL(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	return mageduLooksLikeDownloadURL(raw) && (strings.Contains(u, ".m3u8") || strings.Contains(u, ".mp4") || strings.Contains(u, ".flv") || strings.Contains(u, ".mov"))
}

func mageduLooksLikePolyvID(raw string) bool {
	v := strings.TrimSpace(raw)
	if v == "" || strings.Contains(v, "://") || strings.Contains(v, "/") || strings.Contains(v, "?") {
		return false
	}
	if mageduPolyvVIDRe.MatchString(v) {
		return true
	}
	if len(v) < 8 || len(v) > 80 {
		return false
	}
	for _, r := range v {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func mageduFormatPolyvVID(videoID string) string {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return ""
	}
	if strings.Contains(videoID, "_") {
		return strings.Split(videoID, "_")[0] + "_" + videoID[:1]
	}
	return videoID + "_" + videoID[:1]
}

func mageduNormalizePolyvManifest(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" || strings.HasPrefix(strings.ToLower(u), "http://") || strings.HasPrefix(strings.ToLower(u), "https://") || strings.HasPrefix(strings.ToLower(u), "data:") || strings.HasPrefix(u, "#EXTM3U") {
		return u
	}
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	if strings.HasPrefix(u, "/") {
		return strings.TrimRight(sharedPolyvHLSBase(), "/") + u
	}
	return strings.TrimRight(sharedPolyvHLSBase(), "/") + "/" + u
}

func sharedPolyvHLSBase() string { return "https://hls.videocc.net" }

func mageduM3U8DataURL(text string) string {
	return "data:application/vnd.apple.mpegurl;base64," + base64.StdEncoding.EncodeToString([]byte(text))
}

func mageduAbsolutizeM3U8(text, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil || base == nil || base.Scheme == "" || base.Host == "" {
		return text
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#EXT-X-KEY") {
			if uri := extractMageduM3U8URI(trimmed); uri != "" {
				lines[i] = strings.Replace(line, uri, mageduResolveM3U8URL(base, uri), 1)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines[i] = strings.Replace(line, trimmed, mageduResolveM3U8URL(base, trimmed), 1)
	}
	return strings.Join(lines, "\n")
}

func extractMageduM3U8URI(line string) string {
	idx := strings.Index(line, `URI="`)
	if idx < 0 {
		return ""
	}
	rest := line[idx+5:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func mageduResolveM3U8URL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") || strings.HasPrefix(strings.ToLower(raw), "data:") || strings.HasPrefix(raw, "0x") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return base.Scheme + ":" + raw
	}
	ref, err := url.Parse(raw)
	if err != nil || ref == nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func mageduStreamFormat(u string) string {
	if strings.Contains(strings.ToLower(u), ".m3u8") || strings.HasPrefix(strings.ToLower(u), "data:application/vnd.apple.mpegurl") {
		return "m3u8"
	}
	return mediaExt(u)
}
