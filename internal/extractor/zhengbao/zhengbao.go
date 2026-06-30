// Package zhengbao implements an extractor for chinaacc.com (正保会计网校) courses.
package zhengbao

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	memberOrigin        = "https://member.chinaacc.com"
	memberHomeURL       = "https://member.chinaacc.com/home/"
	elearningHomeURL    = "https://elearning.chinaacc.com/"
	doormanBaseURL      = "https://gateway.cdeledu.com/doorman/op/"
	doormanAppID        = "b3316459-ceeb-47f8-a469-12751ff3075e"
	doormanAESKey       = "823s4125660ijf;*"
	doormanAESIV        = "qyu148#4(1p_1^4;"
	courseGroupPath     = "~/c-home/w-home/f/ru/userCourseClassList"
	courseDetailPath    = "~/c-home/a-home/f/ru/getUserHomeCourse"
	coursewareInfoPath  = "~/c-home/w-home/f/ru/courseWareInfo"
	materialsURL        = "https://elearning.chinaacc.com/xcware/myhome/teachingMaterials.shtm?cwareIDs={cware_id}&identity={identity}"
	materialDownloadURL = "https://elearning.chinaacc.com/data2file/downloadFile/getWordVipFile?cwareID=&fileUrl={file_url}&fileReName={file_name}"
)

var (
	patterns       = []string{`(?:[\w-]+\.)?chinaacc\.com/|(?:[\w-]+\.)?cdeledu\.com/`}
	courseIDRe     = regexp.MustCompile(`(?:courseIds?|courseId)=((?:acc)?[0-9A-Za-z_\-]+)`)
	cwareIDRe      = regexp.MustCompile(`(?:cwareIDs?|cwareId|cware_id|cwId)=([0-9A-Za-z_\-]+)`)
	identityRe     = regexp.MustCompile(`identity=([0-9A-Za-z_\-]+)`)
	openURLRe      = regexp.MustCompile(`window\.open\(["']([^"']+)["']`)
	videoIDRe      = regexp.MustCompile(`(?:videoID|videoId|video_id)=([0-9A-Za-z_\-]+)`)
	h5VarsRe       = regexp.MustCompile(`window\.cdelmedia\.h5Vars\s*=\s*JSON\.parse\('(?s:(.*?))'\)`)
	attrRe         = regexp.MustCompile(`(?i)(data-[a-z0-9_-]+)\s*=\s*["']([^"']+)["']`)
	mediaURLRe     = regexp.MustCompile(`https?://[^"'\s<>]+(?:\.m3u8|\.mp4|\.pdf|\.pptx?|\.docx?|\.xlsx?|\.zip|\.rar|\.7z|\.txt|\.srt)[^"'\s<>]*`)
	titleCleanRe   = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)
	htmlTagRe      = regexp.MustCompile(`<[^>]+>`)
	courseWareKeys = []string{"homeWareList", "freeWareList", "homeSpecialWareList", "freeSpecialWareList", "courseWareList", "wareList", "buyCwareList", "freeCwareList", "homeCwareList", "homeCwareTopList"}
)

func init() {
	extractor.Register(&Zhengbao{}, extractor.SiteInfo{Name: "Zhengbao", URL: "chinaacc.com", NeedAuth: true})
}

type Zhengbao struct{}

func (s *Zhengbao) Patterns() []string { return patterns }

type zbContext struct {
	c         *util.Client
	headers   map[string]string
	uid       string
	sid       string
	publicKey string
	timeDiff  *int64
	cid       string
	cwareID   string
	identity  string
}

type cwareInfo struct {
	CwareID  string
	Identity string
	Title    string
	DirURL   string
	Raw      map[string]any
	Index    int
}

type zbVideo struct {
	Title    string
	PlayURL  string
	VideoID  string
	CwareID  string
	Identity string
}

type zbFile struct {
	Title     string
	TokenURL  string
	DirectURL string
	Format    string
}

func (s *Zhengbao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("zhengbao requires login cookies")
	}
	onlyFiles := zhengbaoOnlyFilesMode(opts.Quality)
	ctx := newContext(opts.Cookies, rawURL)
	if ctx.cid == "" && ctx.cwareID == "" {
		return nil, fmt.Errorf("zhengbao: cannot parse courseId/cwareID from URL")
	}

	wares := ctx.loadCoursewares()
	if len(wares) == 0 && ctx.cwareID != "" {
		wares = []cwareInfo{{CwareID: ctx.cwareID, Identity: ctx.identity, Title: "课件", Index: 1}}
	}
	if len(wares) == 0 {
		return nil, fmt.Errorf("zhengbao: no courseware nodes found for %s", firstNonEmpty(ctx.cid, ctx.cwareID))
	}

	var videos []zbVideo
	var files []zbFile
	for i := range wares {
		wares[i].Index = i + 1
		v, lookup := ctx.parseVideoTree(wares[i])
		if len(v) == 0 && !onlyFiles {
			v = directVideosFromCware(wares[i])
			for _, item := range v {
				if item.VideoID != "" {
					lookup[item.VideoID] = item
				}
			}
		}
		if !onlyFiles {
			videos = append(videos, v...)
		}
		materials := ctx.parseMaterialTree(wares[i], lookup, onlyFiles)
		if len(materials) == 0 {
			materials = directFilesFromCware(wares[i])
		}
		files = append(files, materials...)
	}
	files = uniqueZBFiles(files)

	entries := make([]*extractor.MediaInfo, 0, len(videos)+len(files))
	for i, v := range videos {
		if entry, err := ctx.resolveVideo(v, i+1); err == nil {
			entries = append(entries, entry)
		}
	}
	for i, f := range files {
		entries = append(entries, fileEntry(f, i+1))
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("zhengbao: parsed courseware but no playable video or material URL was resolved")
	}
	return &extractor.MediaInfo{Site: "zhengbao", Title: cleanTitle(firstNonEmpty(ctx.cid, ctx.cwareID, "zhengbao")), Entries: entries}, nil
}

func zhengbaoOnlyFilesMode(quality string) bool {
	mode := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(quality)))
	switch mode {
	case "4", "pdf", "onlypdf", "file", "files", "material", "materials", "courseware", "coursewares", "attachment", "attachments", "资料", "课件":
		return true
	default:
		return false
	}
}

func uniqueZBFiles(files []zbFile) []zbFile {
	seen := map[string]bool{}
	out := make([]zbFile, 0, len(files))
	for _, f := range files {
		key := firstNonEmpty(f.TokenURL, f.DirectURL)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

func newContext(jar http.CookieJar, rawURL string) *zbContext {
	ctx := &zbContext{c: util.NewClient()}
	ctx.c.SetCookieJar(jar)
	ctx.headers, ctx.uid, ctx.sid = headersFromJar(jar)
	ctx.cid, ctx.cwareID, ctx.identity = parseURLHints(rawURL)
	return ctx
}

func parseURLHints(raw string) (courseID, cwareID, identity string) {
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		courseID = firstNonEmpty(q.Get("courseIds"), q.Get("courseId"), q.Get("courseID"))
		cwareID = firstNonEmpty(q.Get("cwareIDs"), q.Get("cwareID"), q.Get("cwareId"), q.Get("cware_id"), q.Get("cwId"))
		identity = q.Get("identity")
	}
	if courseID == "" {
		if m := courseIDRe.FindStringSubmatch(raw); len(m) > 1 {
			courseID = m[1]
		}
	}
	if cwareID == "" {
		if m := cwareIDRe.FindStringSubmatch(raw); len(m) > 1 {
			cwareID = m[1]
		}
	}
	if identity == "" {
		if m := identityRe.FindStringSubmatch(raw); len(m) > 1 {
			identity = m[1]
		}
	}
	return courseID, cwareID, identity
}

func headersFromJar(jar http.CookieJar) (map[string]string, string, string) {
	h := map[string]string{
		"Origin":     memberOrigin,
		"Referer":    memberHomeURL,
		"Accept":     "application/json, text/plain, */*",
		"User-Agent": randomUserAgent(),
	}
	var parts []string
	var uid, sid string
	for _, raw := range []string{memberHomeURL, elearningHomeURL, "https://www.chinaacc.com/", "https://gateway.cdeledu.com/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			parts = append(parts, ck.Name+"="+ck.Value)
			switch strings.ToLower(ck.Name) {
			case "cdeluid", "uid":
				uid = ck.Value
			case "sid":
				sid = ck.Value
			}
		}
	}
	parts = uniqueStrings(parts)
	if len(parts) > 0 {
		h["cookie"] = strings.Join(parts, "; ")
		h["Cookie"] = h["cookie"]
	}
	return h, uid, sid
}

func randomUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.5678.88 Safari/537.36"
}

func (x *zbContext) loadCoursewares() []cwareInfo {
	if x.uid == "" || x.sid == "" {
		return nil
	}
	courseIDs := []string{x.cid}
	if x.cid == "" {
		if groups := x.doormanRequest(courseGroupPath, map[string]any{"uid": x.uid}); len(groups) > 0 {
			courseIDs = collectCourseIDs(groups)
		}
	}
	seen := map[string]bool{}
	var out []cwareInfo
	for _, cid := range courseIDs {
		if cid == "" {
			continue
		}
		params := map[string]any{"courseIds": []string{cid}, "courseId": cid, "uid": x.uid}
		if detail := x.doormanRequest(courseDetailPath, params); len(detail) > 0 {
			for _, id := range collectCourseIDs(detail) {
				if id != "" && !contains(courseIDs, id) {
					courseIDs = append(courseIDs, id)
				}
			}
		}
		cwParams := map[string]any{"courseIds": cid, "courseId": cid, "uid": x.uid}
		if wareResp := x.doormanRequest(coursewareInfoPath, cwParams); len(wareResp) > 0 {
			for _, w := range collectCwares(wareResp) {
				key := firstNonEmpty(w.CwareID, w.DirURL, w.Title)
				if key == "" || seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, w)
			}
		}
	}
	return out
}

func collectCourseIDs(v any) []string {
	seen := map[string]bool{}
	var out []string
	for _, node := range walkMaps(v) {
		for _, k := range []string{"courseId", "courseID", "course_id", "courseIds"} {
			val := node[k]
			switch t := val.(type) {
			case []any:
				for _, it := range t {
					addID(&out, seen, fmt.Sprint(it))
				}
			default:
				for _, part := range strings.Split(fmt.Sprint(val), ",") {
					addID(&out, seen, part)
				}
			}
		}
	}
	return out
}

func addID(out *[]string, seen map[string]bool, value string) {
	value = strings.TrimSpace(value)
	if value == "" || value == "<nil>" || seen[value] {
		return
	}
	seen[value] = true
	*out = append(*out, value)
}

func collectCwares(v any) []cwareInfo {
	seen := map[string]bool{}
	var out []cwareInfo
	for _, node := range walkMaps(v) {
		for _, key := range courseWareKeys {
			for _, item := range extractItems(node[key]) {
				cw := buildCware(item)
				id := firstNonEmpty(cw.CwareID, cw.DirURL, cw.Title)
				if id != "" && !seen[id] && isRecordedCware(item) {
					seen[id] = true
					out = append(out, cw)
				}
			}
		}
		cw := buildCware(node)
		id := firstNonEmpty(cw.CwareID, cw.DirURL)
		if id != "" && !seen[id] && isRecordedCware(node) {
			seen[id] = true
			out = append(out, cw)
		}
	}
	return out
}

func buildCware(m map[string]any) cwareInfo {
	return cwareInfo{
		CwareID:  firstString(m, "cwareId", "cwareID", "cwId", "cware_id"),
		Identity: firstString(m, "identity", "cwIdentity"),
		Title:    cleanTitle(firstNonEmpty(firstString(m, "cwShowName", "cwareName", "title", "name"), "课件")),
		DirURL:   normalizeURL(firstString(m, "cwDirURL", "dirURL", "url")),
		Raw:      m,
	}
}

func isRecordedCware(m map[string]any) bool {
	formName := firstString(m, "courseFormName")
	form := firstString(m, "courseForm")
	dir := normalizeURL(firstString(m, "cwDirURL", "dirURL"))
	return strings.Contains(formName, "录播") || form == "2" || strings.Contains(dir, "videoList") || strings.Contains(dir, "courseView") || firstString(m, "cwareId", "cwareID", "cwId") != "" || directVideoURL(m) != "" || directFileURL(m) != ""
}

func (x *zbContext) doormanRequest(resourcePath string, params map[string]any) map[string]any {
	publicKey := x.getPublicKey()
	serverTime := time.Now().UnixMilli() + x.getTimeDiffer()
	aesKey := ""
	if publicKey != "" {
		aesKey = encryptAESKey(publicKey)
	}
	encParams := encryptParams(params, serverTime)
	payload := map[string]any{
		"resourcePath": resourcePath,
		"domain":       "chinaacc",
		"publicKey":    publicKey,
		"params":       encParams,
		"ve":           "0",
		"lt":           time.Now().UnixMilli(),
		"fs":           "201",
		"ap":           doormanAppID,
		"af":           "1",
		"aesKey":       aesKey,
		"sid":          x.sid,
		"appVersion":   "",
		"appType":      "pc",
		"platform":     "0",
		"siteID":       "1",
	}
	resp := x.postJSON(doormanURL(resourcePath, "chinaacc"), payload, memberHomeURL)
	return resp
}

func (x *zbContext) getPublicKey() string {
	if x.publicKey != "" {
		return x.publicKey
	}
	payload := map[string]any{"appVersion": "", "appType": "", "platform": "pc", "time": time.Now().UnixMilli(), "resourcePath": "+/key/public", "domain": "cdel"}
	resp := x.postJSON(doormanURL("+/key/public", "cdel"), payload, memberHomeURL)
	if s := strings.TrimSpace(fmt.Sprint(resp["result"])); s != "" && s != "<nil>" {
		x.publicKey = s
	}
	return x.publicKey
}

func (x *zbContext) getTimeDiffer() int64 {
	if x.timeDiff != nil {
		return *x.timeDiff
	}
	local := time.Now().UnixMilli()
	payload := map[string]any{"appVersion": "", "appType": "", "platform": "pc", "time": local, "resourcePath": "+/server/time", "domain": "cdel"}
	resp := x.postJSON(doormanURL("+/server/time", "cdel"), payload, memberHomeURL)
	server, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(resp["result"])), 10, 64)
	diff := int64(0)
	if server > 0 {
		diff = server - local
	}
	x.timeDiff = &diff
	return diff
}

func doormanURL(resourcePath, domain string) string {
	return strings.TrimRight(doormanBaseURL, "/") + "/" + domain + "@" + resourcePath
}

func encryptParams(params map[string]any, serverTime int64) string {
	body := map[string]any{}
	for k, v := range params {
		body[k] = v
	}
	body["time"] = serverTime
	plain := marshalCompactJSON(body)
	block, err := aes.NewCipher([]byte(doormanAESKey))
	if err != nil {
		return ""
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(doormanAESIV)).CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out)
}

func encryptAESKey(publicKeyHex string) string {
	der, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil {
		return ""
	}
	var pub *rsa.PublicKey
	if anyKey, err := x509.ParsePKIXPublicKey(der); err == nil {
		pub, _ = anyKey.(*rsa.PublicKey)
	}
	if pub == nil {
		if k, err := x509.ParsePKCS1PublicKey(der); err == nil {
			pub = k
		}
	}
	if pub == nil {
		return ""
	}
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(doormanAESKey))
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
}

func (x *zbContext) postJSON(api string, payload map[string]any, referer string) map[string]any {
	buf := marshalCompactJSON(payload)
	h := copyHeaders(x.headers)
	h["Origin"] = memberOrigin
	h["Content-Type"] = "application/json;charset=UTF-8"
	if referer != "" {
		h["Referer"] = referer
	}
	resp, err := x.c.Post(api, bytes.NewReader(buf), h)
	if err != nil {
		return map[string]any{}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func (x *zbContext) getText(rawURL, referer string) (string, error) {
	h := copyHeaders(x.headers)
	if referer != "" {
		h["Referer"] = referer
	}
	return x.c.GetString(rawURL, h)
}

func (x *zbContext) parseVideoTree(cw cwareInfo) ([]zbVideo, map[string]zbVideo) {
	lookup := map[string]zbVideo{}
	if cw.DirURL == "" {
		return nil, lookup
	}
	body, err := x.getText(cw.DirURL, memberHomeURL)
	if err != nil || body == "" || strings.Contains(body, "课程暂未开通") || strings.Contains(body, "暂未开通") {
		return nil, lookup
	}
	var out []zbVideo
	for _, block := range strings.Split(body, "continueStudyVideo") {
		if !strings.Contains(block, "window.open") {
			continue
		}
		m := openURLRe.FindStringSubmatch(block)
		if len(m) < 2 {
			continue
		}
		playURL := normalizeURL(m[1])
		vid := ""
		if vm := videoIDRe.FindStringSubmatch(playURL); len(vm) > 1 {
			vid = vm[1]
		}
		if vid == "" {
			vid = fmt.Sprintf("video-%d", len(out)+1)
		}
		title := cleanTitle(firstNonEmpty(extractNearbyText(block), cw.Title, fmt.Sprintf("课时%d", len(out)+1)))
		item := zbVideo{Title: fmt.Sprintf("[%d.%d]--%s", cw.Index, len(out)+1, title), PlayURL: playURL, VideoID: vid, CwareID: cw.CwareID, Identity: cw.Identity}
		out = append(out, item)
		lookup[vid] = item
	}
	if len(out) == 0 {
		for i, u := range mediaURLRe.FindAllString(body, -1) {
			if strings.Contains(strings.ToLower(u), ".m3u8") || strings.Contains(strings.ToLower(u), ".mp4") {
				item := zbVideo{Title: fmt.Sprintf("[%d.%d]--%s", cw.Index, i+1, firstNonEmpty(cw.Title, "课时")), PlayURL: u, VideoID: fmt.Sprint(i + 1), CwareID: cw.CwareID, Identity: cw.Identity}
				out = append(out, item)
				lookup[item.VideoID] = item
			}
		}
	}
	return out, lookup
}

func (x *zbContext) parseMaterialTree(cw cwareInfo, videoLookup map[string]zbVideo, onlyFiles bool) []zbFile {
	if cw.CwareID == "" {
		return nil
	}
	api := strings.ReplaceAll(materialsURL, "{cware_id}", url.QueryEscape(cw.CwareID))
	api = strings.ReplaceAll(api, "{identity}", url.QueryEscape(cw.Identity))
	body, err := x.getText(api, elearningHomeURL)
	if err != nil || body == "" {
		return nil
	}
	var out []zbFile
	seen := map[string]bool{}
	for _, attrs := range extractAttrs(body) {
		name := firstNonEmpty(attrs["data-videoname"], attrs["title"], cw.Title, "课程资料")
		if vid := attrs["data-videoid"]; vid != "" {
			if v, ok := videoLookup[vid]; ok && v.Title != "" {
				name = v.Title
			}
		}
		for _, spec := range []struct{ Key, Format, Suffix string }{{"data-fileurl", "docx", ""}, {"data-pdfurl", "pdf", ""}, {"data-sepurl", "docx", "-答案分离"}, {"data-seppdfurl", "pdf", "-答案分离"}} {
			tok := strings.TrimSpace(attrs[spec.Key])
			if tok == "" || seen[spec.Key+tok] {
				continue
			}
			seen[spec.Key+tok] = true
			fileName := cleanTitle(name + spec.Suffix)
			out = append(out, zbFile{Title: fileName, TokenURL: buildMaterialURL(tok, fileName, spec.Format), DirectURL: tok, Format: spec.Format})
		}
	}
	for _, u := range mediaURLRe.FindAllString(body, -1) {
		if onlyFiles && isVideoURL(u) {
			continue
		}
		if !seen[u] {
			seen[u] = true
			out = append(out, zbFile{Title: firstNonEmpty(cw.Title, "课程资料"), DirectURL: strings.ReplaceAll(u, `\/`, `/`), Format: pickFormat(u)})
		}
	}
	return out
}

func (x *zbContext) resolveVideo(v zbVideo, index int) (*extractor.MediaInfo, error) {
	if v.PlayURL == "" {
		return nil, fmt.Errorf("zhengbao: empty play URL")
	}
	playURL := normalizeURL(v.PlayURL)
	format := pickFormat(playURL)
	extra := map[string]any{"video_id": v.VideoID, "play_page": playURL, "cware_id": v.CwareID, "identity": v.Identity}
	if !strings.Contains(strings.ToLower(playURL), ".m3u8") && !strings.Contains(strings.ToLower(playURL), ".mp4") {
		body, err := x.getText(playURL, elearningHomeURL)
		if err != nil {
			return nil, err
		}
		vars := parseH5Vars(body)
		if len(vars) == 0 {
			return nil, fmt.Errorf("zhengbao: no h5Vars in play page")
		}
		if p := firstString(vars, "videoPath", "video_path", "path", "url"); p != "" {
			playURL = normalizeURL(strings.ReplaceAll(p, `\/`, `/`))
			format = pickFormat(playURL)
		}
		if sub := firstString(vars, "srtPath", "subtitle", "subtitleUrl"); sub != "" {
			extra["subtitle"] = normalizeURL(sub)
		}
	}
	if playURL == "" {
		return nil, fmt.Errorf("zhengbao: no videoPath resolved")
	}
	name := cleanTitle(firstNonEmpty(v.Title, fmt.Sprintf("[%02d]--%s", index, v.VideoID)))
	var subtitles []extractor.Subtitle
	if sub := firstNonEmpty(fmt.Sprint(extra["subtitle"])); sub != "" && sub != "<nil>" {
		subtitles = append(subtitles, extractor.Subtitle{Language: "zh", URL: sub, Format: pickFormat(sub)})
	}
	return &extractor.MediaInfo{Site: "zhengbao", Title: name, Streams: map[string]extractor.Stream{"default": {Quality: "source", URLs: []string{playURL}, Format: format, NeedMerge: format == "m3u8", Headers: map[string]string{"Referer": elearningHomeURL, "User-Agent": x.headers["User-Agent"]}}}, Subtitles: subtitles, Extra: extra}, nil
}

func parseH5Vars(body string) map[string]any {
	m := h5VarsRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return nil
	}
	escaped := strings.ReplaceAll(m[1], `\/`, `/`)
	escaped = strings.ReplaceAll(escaped, `\'`, `'`)
	quoted := `"` + strings.ReplaceAll(escaped, `"`, `\"`) + `"`
	if unquoted, err := strconv.Unquote(quoted); err == nil {
		escaped = unquoted
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(escaped), &out); err != nil {
		return nil
	}
	return out
}

func fileEntry(f zbFile, index int) *extractor.MediaInfo {
	u := firstNonEmpty(f.TokenURL, f.DirectURL)
	name := cleanTitle(firstNonEmpty(f.Title, fmt.Sprintf("[%02d]--资料", index)))
	return &extractor.MediaInfo{Site: "zhengbao", Title: name, Streams: map[string]extractor.Stream{"default": {Quality: "source", URLs: []string{u}, Format: f.Format, Headers: map[string]string{"Referer": elearningHomeURL}}}, Extra: map[string]any{"type": "file"}}
}

func buildMaterialURL(fileToken, fileName, fmtHint string) string {
	if fileToken == "" {
		return ""
	}
	fullName := fileName
	if !strings.HasSuffix(strings.ToLower(fullName), "."+strings.ToLower(fmtHint)) && fmtHint != "" {
		fullName += "." + fmtHint
	}
	u := strings.ReplaceAll(materialDownloadURL, "{file_url}", url.QueryEscape(fileToken))
	u = strings.ReplaceAll(u, "{file_name}", url.QueryEscape(fullName))
	return u
}

func extractAttrs(body string) []map[string]string {
	var out []map[string]string
	for _, tag := range regexp.MustCompile(`<[^>]+>`).FindAllString(body, -1) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = strings.ReplaceAll(m[2], `\/`, `/`)
		}
		if len(attrs) > 0 {
			out = append(out, attrs)
		}
	}
	return out
}

func extractNearbyText(html string) string {
	text := htmlTagRe.ReplaceAllString(html, " ")
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > 80 {
		text = string([]rune(text)[:80])
	}
	return text
}

func normalizeURL(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if raw == "" || raw == "<nil>" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "/") {
		return strings.TrimRight(elearningHomeURL, "/") + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	base, _ := url.Parse(elearningHomeURL)
	ref, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func extractItems(v any) []map[string]any {
	if arr, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(arr))
		for _, it := range arr {
			if m := asMap(it); len(m) > 0 {
				out = append(out, m)
			}
		}
		return out
	}
	if m := asMap(v); len(m) > 0 {
		return []map[string]any{m}
	}
	return nil
}

func walkMaps(v any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, v := range t {
				walk(v)
			}
		case []any:
			for _, v := range t {
				walk(v)
			}
		}
	}
	walk(v)
	return out
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func contains(vals []string, target string) bool {
	for _, v := range vals {
		if v == target {
			return true
		}
	}
	return false
}

func copyHeaders(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func uniqueStrings(vals []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func cleanTitle(s string) string { return titleCleanRe.ReplaceAllString(strings.TrimSpace(s), "_") }

func pickFormat(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, ".m3u8"):
		return "m3u8"
	case strings.Contains(lower, ".mp4"):
		return "mp4"
	case strings.Contains(lower, ".pdf"):
		return "pdf"
	case strings.Contains(lower, ".ppt"):
		return "ppt"
	case strings.Contains(lower, ".doc"):
		return "doc"
	case strings.Contains(lower, ".xls"):
		return "xls"
	case strings.Contains(lower, ".srt"):
		return "srt"
	}
	return "bin"
}
