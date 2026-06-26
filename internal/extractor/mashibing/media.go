package mashibing

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

func mashibingHeaders(cookie string) map[string]string {
	headers := map[string]string{"client": "pc", "platform_type": "1", "accept": "application/json, text/plain, */*", "origin": urlReferer, "referer": urlReferer, "cookie": cookie, "User-Agent": mashibingUA}
	if token := mashibingCookieValue(cookie, "token"); token != "" {
		var obj map[string]any
		decoded, _ := url.QueryUnescape(token)
		if json.Unmarshal([]byte(decoded), &obj) == nil {
			access := mashibingFirstText(obj["token"])
			prefix := mashibingFirstText(obj["tokenPrefix"], obj["tokenHead"], "Bearer ")
			if prefix != "" && !strings.HasSuffix(prefix, " ") {
				prefix += " "
			}
			key := mashibingFirstText(obj["tokenHeaderKey"], "Authorization")
			if access != "" {
				headers[key] = prefix + access
			}
			refresh, refreshKey := mashibingFirstText(obj["refreshToken"]), mashibingFirstText(obj["refreshTokenHeaderKey"])
			if refresh != "" && refreshKey != "" {
				headers[refreshKey] = prefix + refresh
			}
		}
	}
	return headers
}

func mashibingCookieString(jar http.CookieJar) string {
	seen, parts := map[string]bool{}, []string{}
	for _, raw := range []string{"https://www.mashibing.com/", "https://mashibing.com/", "https://gateway.mashibing.com/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			if ck.Value != "" && !seen[ck.Name] {
				seen[ck.Name] = true
				parts = append(parts, ck.Name+"="+ck.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}

func mashibingCookieValue(cookie, name string) string {
	for _, p := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 && strings.EqualFold(kv[0], name) {
			return kv[1]
		}
	}
	return ""
}

func mashibingParseCourseID(raw string) string {
	if m := mashibingIDRe.FindStringSubmatch(raw); len(m) > 0 {
		return mashibingFirstText(m[1], m[2])
	}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		return mashibingFirstText(q.Get("courseNo"), q.Get("courseId"), q.Get("cid"), q.Get("id"))
	}
	return ""
}

func mashibingPickCourse(courses []mashibingCourse, cid string) mashibingCourse {
	for _, c := range courses {
		if cid != "" && c.ID == cid {
			return c
		}
	}
	return mashibingCourse{}
}

func mashibingBuildItems(c *util.Client, sess *mashibingSession, cid string, detail map[string]any) []mashibingItem {
	items := []mashibingItem{}
	items = append(items, mashibingExtractSectionSources(detail, cid, "1.1", 1)...)
	for chIdx, chapter := range mashibingRecords(detail["chapterList"]) {
		chapterNo := chIdx + 1
		videoNo, fileNo := 0, 0
		sections := mashibingRecords(chapter["sectionList"])
		sort.SliceStable(sections, func(i, j int) bool {
			return mashibingInt(sections[i]["sectionNo"]) < mashibingInt(sections[j]["sectionNo"])
		})
		for _, section := range sections {
			vid := mashibingFirstText(section["ployvVideoId"], section["polyvVideoId"], section["videoId"])
			if vid != "" {
				videoNo++
				title := mashibingFirstText(section["sectionName"], section["title"], vid)
				items = append(items, mashibingItem{Kind: "video", Name: mashibingCleanName(fmt.Sprintf("[%d.%d]--%s", chapterNo, videoNo, title)), VideoID: vid, Size: int64(mashibingNum(section["downloadVideoSize"], section["size"])), Raw: section})
			}
			files := mashibingExtractSectionSources(section, cid, strconv.Itoa(chapterNo), fileNo+1)
			fileNo += len(files)
			items = append(items, files...)
		}
	}
	if docInfo := mashibingFetchDocumentInfo(c, sess, cid); len(docInfo) > 0 {
		items = append(items, mashibingDocumentItems(cid, docInfo)...)
	}
	return items
}

func mashibingExtractSectionSources(section map[string]any, cid, prefix string, startIndex int) []mashibingItem {
	if len(section) == 0 {
		return nil
	}
	keys := []string{"dataUrl", "gitUrl", "netdiskUrl", "fynoteUrl", "downloadUrl", "fileUrl", "attachmentUrl"}
	seen := map[string]bool{}
	items := []mashibingItem{}
	idx := startIndex - 1
	baseTitle := mashibingFirstText(section["sectionName"], section["courseName"], section["title"], section["name"], "资料")
	for _, k := range keys {
		u := mashibingFirstText(section[k])
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		idx++
		fmtName := mashibingFileExt(u)
		name := mashibingCleanName(fmt.Sprintf("(%s.%d)--%s", prefix, idx, baseTitle))
		if fmtName != "" && fmtName != "attach" && !strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(fmtName)) {
			name = strings.TrimRight(name, ".") + "." + fmtName
		}
		items = append(items, mashibingItem{Kind: "file", Name: name, FileURL: mashibingNormalizeMediaURL(u), FileFmt: fmtName, CourseID: cid, Raw: section})
	}
	return items
}

func mashibingFetchDocumentInfo(c *util.Client, sess *mashibingSession, cid string) map[string]any {
	for page := 1; page < 100; page++ {
		resp, err := mashibingGetJSON(c, urlSourceList, map[string]string{"page": strconv.Itoa(page), "size": "10"}, sess.Headers)
		if err != nil {
			break
		}
		data := mashibingMap(resp["data"])
		for _, rec := range mashibingRecords(data["records"]) {
			if mashibingFirstText(rec["courseId"]) == cid {
				return rec
			}
		}
		pages := mashibingInt(data["pages"])
		if pages > 0 && page >= pages {
			break
		}
	}
	return map[string]any{}
}

func mashibingDocumentItems(cid string, info map[string]any) []mashibingItem {
	items := []mashibingItem{}
	for chIdx, chapter := range mashibingRecords(info["chapterList"]) {
		prefix := strconv.Itoa(chIdx + 1)
		for _, section := range mashibingRecords(chapter["sectionList"]) {
			sectionID := mashibingFirstText(section["sectionId"], section["sectionNo"], section["id"])
			title := mashibingFirstText(section["sectionName"], section["title"], section["name"], "资料")
			if sectionID != "" {
				items = append(items, mashibingItem{Kind: "document", Name: mashibingCleanName(fmt.Sprintf("(%s.1)--%s", prefix, title)), CourseID: cid, SectionID: sectionID, Raw: section})
			}
			items = append(items, mashibingExtractSectionSources(section, cid, prefix, len(items)+1)...)
		}
	}
	return items
}

func mashibingPolyvInfo(c *util.Client, videoID string, headers map[string]string) map[string]any {
	body, err := c.GetString(fmt.Sprintf(urlPolyvJSONTmpl, url.PathEscape(videoID)), headers)
	if err != nil {
		return map[string]any{}
	}
	var raw map[string]any
	if json.Unmarshal([]byte(body), &raw) != nil {
		return map[string]any{}
	}
	if bodyHex := mashibingFirstText(raw["body"]); bodyHex != "" {
		if decoded := mashibingPolyvDecode(videoID, bodyHex); len(decoded) > 0 {
			return decoded
		}
		if decoded := mashibingPolyvDecode(mashibingFormatPolyvVID(videoID), bodyHex); len(decoded) > 0 {
			return decoded
		}
	}
	if data := mashibingMap(raw["data"]); len(data) > 0 {
		return data
	}
	return raw
}

func mashibingPolyvDecode(videoID, bodyHex string) map[string]any {
	cipherText, err := hex.DecodeString(strings.TrimSpace(bodyHex))
	if err != nil || len(cipherText) == 0 || len(cipherText)%aes.BlockSize != 0 {
		return map[string]any{}
	}
	sum := md5.Sum([]byte(videoID))
	digest := hex.EncodeToString(sum[:])
	block, err := aes.NewCipher([]byte(digest[:16]))
	if err != nil {
		return map[string]any{}
	}
	plain := make([]byte, len(cipherText))
	cipher.NewCBCDecrypter(block, []byte(digest[16:32])).CryptBlocks(plain, cipherText)
	if pad := int(plain[len(plain)-1]); pad > 0 && pad <= aes.BlockSize && pad <= len(plain) {
		plain = plain[:len(plain)-pad]
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(plain)))
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(plain)))
	}
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(decoded, &out) != nil {
		return map[string]any{}
	}
	return out
}

func mashibingFormatPolyvVID(videoID string) string {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return ""
	}
	if strings.Contains(videoID, "_") {
		return strings.Split(videoID, "_")[0] + "_" + videoID[:1]
	}
	return videoID + "_" + videoID[:1]
}

func mashibingSelectPolyvURL(info map[string]any, mode int) string {
	qualityOrder := map[int][]int{1: {0, 1, 2}, 2: {1, 2, 0}, 3: {2, 0, 1}}
	order := qualityOrder[mode]
	if len(order) == 0 {
		order = []int{0, 1, 2}
	}
	for _, key := range []string{"hls", "hls2", "hls_backup"} {
		arr := mashibingStringList(info[key])
		if len(arr) == 0 {
			continue
		}
		for _, idx := range append(order, 0) {
			if idx >= 0 && idx < len(arr) && arr[idx] != "" {
				return arr[idx]
			}
		}
		if arr[len(arr)-1] != "" {
			return arr[len(arr)-1]
		}
	}
	return mashibingFirstText(info["hlsIndex"], info["videolink"])
}

func mashibingBuildPolyvPDXURL(videoURL string, info map[string]any) string {
	if s := mashibingFirstText(info["pdx"], info["pdxUrl"]); s != "" {
		return s
	}
	u, err := url.Parse(strings.TrimSpace(videoURL))
	if err != nil || u.Path == "" {
		return ""
	}
	lower := strings.ToLower(u.Path)
	if strings.HasSuffix(lower, ".pdx") {
		return videoURL
	}
	if strings.HasSuffix(lower, ".m3u8") {
		u.Path = regexp.MustCompile(`(?i)\.m3u8$`).ReplaceAllString(u.Path, ".pdx")
		return u.String()
	}
	return ""
}

func mashibingPlaySafeToken(c *util.Client, sess *mashibingSession, videoID string) string {
	resp, err := mashibingPostJSON(c, urlPlaySafe, map[string]any{"videoId": videoID}, sess.Headers)
	if err != nil {
		return ""
	}
	data := resp["data"]
	if s := mashibingFirstText(data); s != "" && s != "map[]" {
		if _, ok := data.(map[string]any); !ok {
			return s
		}
	}
	m := mashibingMap(data)
	return mashibingFirstText(m["playSafe"], m["token"], m["playSafeToken"], resp["playSafe"], resp["token"], resp["playSafeToken"])
}

func mashibingPolyvHeaders(sess *mashibingSession) map[string]string {
	return map[string]string{"Accept": "application/json, text/plain, */*", "Origin": urlReferer, "Referer": urlReferer, "User-Agent": mashibingFirstText(sess.Headers["User-Agent"], mashibingUA)}
}

func mashibingNormalizeMediaURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "data:") || strings.HasPrefix(s, "#EXTM3U") {
		return s
	}
	if strings.HasPrefix(s, "//") {
		return "https:" + s
	}
	if strings.HasPrefix(s, "/") {
		return shared.PolyvHLSPlayBase + s
	}
	return shared.PolyvHLSPlayBase + "/" + s
}

func mashibingStreamFormat(u string) string {
	lower := strings.ToLower(u)
	switch {
	case strings.HasPrefix(strings.TrimSpace(u), "#extm3u") || strings.Contains(lower, ".m3u8"):
		return "m3u8"
	case strings.Contains(lower, ".pdx"):
		return "pdx"
	case strings.Contains(lower, ".mp4"):
		return "mp4"
	default:
		return "m3u8"
	}
}

func mashibingFileExt(u string) string {
	if parsed, err := url.Parse(u); err == nil {
		if ext := strings.TrimPrefix(strings.ToLower(path.Ext(parsed.Path)), "."); ext != "" && len(ext) <= 8 {
			return ext
		}
	}
	return "attach"
}

func mashibingMarkdownHTML(text, title string) string {
	return "<!doctype html><html><head><meta charset=\"utf-8\"><title>" + html.EscapeString(title) + "</title></head><body><pre>" + html.EscapeString(text) + "</pre></body></html>"
}

func mashibingMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func mashibingRecords(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return x
	case map[string]any:
		return []map[string]any{x}
	}
	return nil
}

func mashibingStringList(v any) []string {
	switch x := v.(type) {
	case string:
		if x != "" {
			return []string{x}
		}
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := mashibingFirstText(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	}
	return nil
}

func mashibingCloneHeaders(h map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[k] = v
	}
	return out
}

func mashibingFirstText(vals ...any) string {
	for _, v := range vals {
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

func mashibingNum(vals ...any) float64 {
	for _, v := range vals {
		s := strings.ReplaceAll(mashibingFirstText(v), ",", "")
		if s == "" {
			continue
		}
		f, err := strconv.ParseFloat(s, 64)
		if err == nil {
			return f
		}
	}
	return 0
}

func mashibingInt(v any) int { return int(mashibingNum(v)) }

var invalidNameChars = regexp.MustCompile(`[\\/:*?"<>|\r\n]+`)

func mashibingCleanName(s string) string {
	s = invalidNameChars.ReplaceAllString(s, "_")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
