// Package unipus implements a source-aligned extractor for moocs.unipus.cn (中国高校外语慕课平台).
package unipus

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	USER_AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	origin     = "https://moocs.unipus.cn"
	referer    = origin + "/"
	login_url  = "https://sso.unipus.cn/sso/gl/login?service=https%3A%2F%2Fmoocs.unipus.cn%2Flogin%2Fcas%2Fcallback%3Fgoto%3D%252Fmy%252Fcourses%252Flearning"

	course_url       = origin + "/course/%s"
	join_course_url  = origin + "/course/%s/buy"
	task_list_url    = origin + "/course/%s/tasks"
	content_preview  = origin + "/course/%s/task/%s/content/preview"
	content_url      = origin + "/course/%s/task/%s/content"
	content_show_url = origin + "/course/%s/task/%s/show"
)

var patterns = []string{`(?:[\w-]+\.)?unipus\.cn/`}

func init() {
	extractor.Register(&Unipus{}, extractor.SiteInfo{Name: "Unipus", URL: "unipus.cn", NeedAuth: true})
}

type Unipus struct{}

func (u *Unipus) Patterns() []string { return patterns }

var (
	cidRe       = regexp.MustCompile(`(?:/courses?/|[?&](?:cid|courseId)=)(\d+)`)
	iframeRe    = regexp.MustCompile(`(?is)<iframe[^>]+src=["']([^"']+)`)
	liRe        = regexp.MustCompile(`(?is)<li\b([^>]*)>(.*?)</li>`)
	tagRe       = regexp.MustCompile(`(?is)<[^>]+>`)
	anchorRe    = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	attrRe      = regexp.MustCompile(`(?is)([\w:-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	mediaExtRe  = regexp.MustCompile(`(?i)\.(?:m3u8|mp4|flv|mov|m4v|mp3|m4a|aac|wav)(?:[?#]|$)`)
	fileExtRe   = regexp.MustCompile(`(?i)\.(?:pdf|pptx?|docx?|xlsx?|xls|zip|rar|7z|caj)(?:[?#]|$)`)
	mediaURLRe  = regexp.MustCompile(`(?i)https?://[^"'<>\s]+?\.(?:m3u8|mp4|flv|mov|m4v|mp3|m4a|aac|wav)(?:\?[^"'<>\s]*)?`)
	fileURLRe   = regexp.MustCompile(`(?i)https?://[^"'<>\s]+?\.(?:pdf|pptx?|docx?|xlsx?|xls|zip|rar|7z|caj)(?:\?[^"'<>\s]*)?`)
	examTextRe  = regexp.MustCompile(`(周测|期末测试|讨论|作业|测验|考试)`)
	chapterName = regexp.MustCompile(`^\s*(试看|加入)\s*`)
)

func (u *Unipus) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("unipus requires login cookies")
	}
	cid := parseCID(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("cannot parse unipus course id from URL")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	cookie := cookieString(opts.Cookies)
	courseRef := fmt.Sprintf(course_url, url.PathEscape(cid))

	titleBody, _, err := requestText(c, courseRef, referer, cookie)
	if err != nil {
		return nil, fmt.Errorf("unipus course page: %w", err)
	}
	title := extractTitle(titleBody, cid)
	joinCourse(c, cid, cookie)
	tasks, err := fetchTasks(c, cid, cookie)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("unipus course %s has no downloadable task-item entries", cid)
	}

	cache := map[string]sourceSet{}
	var entries []*extractor.MediaInfo
	for _, task := range tasks {
		sources := resolveTaskSources(c, cid, task, cookie, cache)
		kind := "videos"
		if task.Kind == "file" {
			kind = "files"
		}
		for _, src := range sources[kind] {
			entries = append(entries, makeEntry(c, task, src, cookie))
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("unipus course %s returned no media/file URLs", cid)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Title < entries[j].Title })
	return &extractor.MediaInfo{Site: "Unipus", Title: title, Entries: entries, Extra: map[string]any{"course_id": cid, "login_url": login_url}}, nil
}

type taskItem struct {
	TaskID     string
	TaskName   string
	EntryName  string
	PreviewURL string
	Section    string
	Kind       string
	Index      int
}

type source struct{ URL, Title string }
type sourceSet map[string][]source

func parseCID(raw string) string {
	return first(match1(strings.TrimSpace(raw), cidRe), match1(strings.TrimSpace(raw), regexp.MustCompile(`/course/(\d+)`)))
}

func requestHeaders(ref, cookie string, ajax bool) map[string]string {
	if ref == "" {
		ref = referer
	}
	h := map[string]string{"Origin": origin, "Referer": ref, "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", "User-Agent": USER_AGENT}
	if cookie != "" {
		h["cookie"] = cookie
		h["Cookie"] = cookie
	}
	if ajax {
		h["X-Requested-With"] = "XMLHttpRequest"
		h["Accept"] = "application/json, text/javascript, */*; q=0.01"
	}
	return h
}

func requestText(c *util.Client, raw, ref, cookie string) (string, string, error) {
	resp, err := c.Get(absURL(raw, origin), requestHeaders(ref, cookie, false))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	finalURL := absURL(raw, origin)
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	if resp.StatusCode >= 400 {
		return "", finalURL, fmt.Errorf("HTTP %d from %s", resp.StatusCode, raw)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", finalURL, err
	}
	return string(b), finalURL, nil
}

func joinCourse(c *util.Client, cid, cookie string) {
	api := fmt.Sprintf(join_course_url, url.PathEscape(cid))
	ref := fmt.Sprintf(course_url, url.PathEscape(cid))
	_, _ = c.PostForm(api, map[string]string{}, requestHeaders(ref, cookie, false))
	_, _ = c.GetString(api, requestHeaders(ref, cookie, false))
}

func fetchTasks(c *util.Client, cid, cookie string) ([]taskItem, error) {
	api := fmt.Sprintf(task_list_url, url.PathEscape(cid))
	body, _, err := requestText(c, api, fmt.Sprintf(course_url, url.PathEscape(cid)), cookie)
	if err != nil {
		return nil, fmt.Errorf("unipus task list: %w", err)
	}
	var tasks []taskItem
	section := "{1}--默认章节"
	chapter, videoNo, fileNo := 0, 0, 0
	for _, m := range liRe.FindAllStringSubmatch(body, -1) {
		attrs := parseAttrs(m[1])
		fragment := m[0]
		if !containsClass(attrs["class"], "task-item") && !strings.Contains(fragment, "task-item") {
			continue
		}
		if containsClass(attrs["class"], "js-task-chapter") || strings.Contains(attrs["class"], "js-task-chapter") {
			chapter++
			videoNo, fileNo = 0, 0
			name := first(taskTitle(fragment, fmt.Sprintf("第%d章", chapter)), "默认章节")
			section = fmt.Sprintf("{%d}--%s", chapter, sanitizeName(name))
			continue
		}
		taskID := first(match1(attrs["id"], regexp.MustCompile(`task_id_(\d+)`)), match1(fragment, regexp.MustCompile(`task_id_(\d+)`)))
		if taskID == "" {
			continue
		}
		kind := taskType(fragment)
		if kind == "" {
			continue
		}
		name := taskTitle(fragment, taskID)
		preview := taskPreviewURL(fragment, cid, taskID)
		item := taskItem{TaskID: taskID, TaskName: name, PreviewURL: preview, Section: section, Kind: kind}
		if kind == "video" {
			videoNo++
			item.Index = videoNo
			item.EntryName = fmt.Sprintf("[%d.%d]--%s", max(chapter, 1), videoNo, first(name, taskID))
		} else {
			fileNo++
			item.Index = fileNo
			item.EntryName = fmt.Sprintf("(%d.%d)--%s", max(chapter, 1), fileNo, first(name, taskID))
		}
		tasks = append(tasks, item)
	}
	return tasks, nil
}

func taskType(fragment string) string {
	text := cleanText(fragment)
	lower := strings.ToLower(fragment)
	for _, icon := range []string{"es-icon-kaoshi", "es-icon-comment", "es-icon-homework", "es-icon-quiz"} {
		if strings.Contains(lower, icon) {
			return ""
		}
	}
	if examTextRe.MatchString(text) {
		return ""
	}
	if strings.Contains(lower, "filedownload") || strings.Contains(text, "下载资料") {
		return "file"
	}
	if (strings.Contains(lower, "videoclass") && strings.Contains(text, "视频课时")) || strings.Contains(lower, "/activity/video") {
		return "video"
	}
	return ""
}

func taskTitle(fragment, fallback string) string {
	for _, m := range anchorRe.FindAllStringSubmatch(fragment, -1) {
		attrs := parseAttrs(m[1])
		if containsClass(attrs["class"], "title") || attrs["data-url"] != "" || strings.Contains(attrs["data-url"], "/task/") {
			return first(cleanTaskName(m[2]), fallback)
		}
	}
	if m := anchorRe.FindStringSubmatch(fragment); len(m) > 2 {
		return first(cleanTaskName(m[2]), fallback)
	}
	return fallback
}

func taskPreviewURL(fragment, cid, taskID string) string {
	for _, m := range anchorRe.FindAllStringSubmatch(fragment, -1) {
		attrs := parseAttrs(m[1])
		if attrs["data-url"] == "" || !strings.Contains(attrs["data-url"], "/task/") {
			continue
		}
		if href := absURL(attrs["href"], origin); href != "" {
			return href
		}
		if dataURL := absURL(attrs["data-url"], origin); dataURL != "" {
			return dataURL
		}
	}
	return fmt.Sprintf(content_preview, url.PathEscape(cid), url.PathEscape(taskID))
}

func resolveTaskSources(c *util.Client, cid string, task taskItem, cookie string, cache map[string]sourceSet) sourceSet {
	if cached, ok := cache[task.TaskID]; ok {
		return cached
	}
	candidates := []string{}
	if task.PreviewURL != "" {
		if iframe := resolvePreviewIframe(c, cid, task.PreviewURL, cookie); iframe != "" {
			candidates = append(candidates, iframe)
		}
	}
	candidates = append(candidates,
		fmt.Sprintf(content_preview, url.PathEscape(cid), url.PathEscape(task.TaskID)),
		fmt.Sprintf(content_url, url.PathEscape(cid), url.PathEscape(task.TaskID)),
		fmt.Sprintf(content_show_url, url.PathEscape(cid), url.PathEscape(task.TaskID)),
	)
	out := sourceSet{"videos": {}, "files": {}}
	seen := map[string]bool{}
	for _, page := range candidates {
		body, finalURL, err := requestText(c, page, first(task.PreviewURL, fmt.Sprintf(course_url, url.PathEscape(cid))), cookie)
		if err != nil || body == "" || strings.Contains(finalURL, "/login") || strings.Contains(finalURL, "sso.unipus.cn") {
			continue
		}
		found := extractSourcesFromHTML(body, page, first(task.TaskName, task.TaskID))
		for _, key := range []string{"videos", "files"} {
			for _, src := range found[key] {
				if src.URL == "" || seen[src.URL] {
					continue
				}
				seen[src.URL] = true
				out[key] = append(out[key], src)
			}
		}
		if len(out["videos"]) > 0 || len(out["files"]) > 0 {
			break
		}
	}
	cache[task.TaskID] = out
	return out
}

func resolvePreviewIframe(c *util.Client, cid, preview, cookie string) string {
	body, _, err := requestText(c, preview, fmt.Sprintf(course_url, url.PathEscape(cid)), cookie)
	if err != nil || body == "" {
		return ""
	}
	if m := iframeRe.FindStringSubmatch(body); len(m) > 1 {
		return absURL(m[1], preview)
	}
	return ""
}

func extractSourcesFromHTML(text, baseURL, defaultTitle string) sourceSet {
	out := sourceSet{"videos": {}, "files": {}}
	seen := map[string]bool{}
	for _, tag := range tagRe.FindAllString(text, -1) {
		attrs := parseAttrs(tag)
		for _, name := range []string{"data-url", "src", "href", "data-download-url", "data-file-url"} {
			raw := attrs[name]
			if raw == "" {
				continue
			}
			u := absURL(raw, baseURL)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			title := cleanText(first(attrs["title"], attrs["download"], defaultTitle))
			appendSource(out, u, first(title, defaultTitle))
		}
	}
	for _, re := range []*regexp.Regexp{mediaURLRe, fileURLRe} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			u := absURL(m[0], baseURL)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			appendSource(out, u, defaultTitle)
		}
	}
	return out
}

func appendSource(out sourceSet, u, title string) {
	if mediaExtRe.MatchString(u) {
		out["videos"] = append(out["videos"], source{URL: u, Title: title})
	} else if fileExtRe.MatchString(u) {
		out["files"] = append(out["files"], source{URL: u, Title: title})
	}
}

func makeEntry(c *util.Client, task taskItem, src source, cookie string) *extractor.MediaInfo {
	title := sanitizeName(first(src.Title, task.EntryName, task.TaskName, task.TaskID))
	streamURL, format, streamExtra, extra := prepareSource(c, src.URL, first(task.PreviewURL, referer), cookie)
	quality := "best"
	if task.Kind == "file" {
		quality = "file"
	}
	headers := map[string]string{"Referer": first(task.PreviewURL, referer), "User-Agent": USER_AGENT}
	if cookie != "" {
		headers["Cookie"] = cookie
	}
	for k, v := range map[string]any{"task_id": task.TaskID, "task_name": task.TaskName, "section": task.Section, "type": task.Kind} {
		extra[k] = v
	}
	return &extractor.MediaInfo{Site: "Unipus", Title: title, Streams: map[string]extractor.Stream{quality: {Quality: quality, URLs: []string{streamURL}, Format: format, NeedMerge: format == "m3u8", Headers: headers, Extra: streamExtra}}, Extra: extra}
}

func extractTitle(body, cid string) string {
	for _, tag := range tagRe.FindAllString(body, -1) {
		attrs := parseAttrs(tag)
		if containsClass(attrs["class"], "js-social-share-params") && attrs["data-title"] != "" {
			return sanitizeName(attrs["data-title"])
		}
	}
	if s := tagTextInClass(body, "h3", "course-detail-heading"); s != "" {
		return sanitizeName(s)
	}
	if s := tagTextWithClass(body, "h3", "view-title"); s != "" {
		return sanitizeName(s)
	}
	if s := tagTextWithClass(body, "h1", ""); s != "" {
		return sanitizeName(s)
	}
	if s := tagTextWithClass(body, "title", ""); s != "" {
		s = regexp.MustCompile(`\s*-\s*中国高校外语慕课平台.*$`).ReplaceAllString(cleanText(s), "")
		if s != "" {
			return sanitizeName(s)
		}
	}
	return "外语慕课" + cid
}

func cookieString(j http.CookieJar) string {
	seen := map[string]bool{}
	var parts []string
	for _, host := range []string{origin + "/", "https://sso.unipus.cn/"} {
		u, err := url.Parse(host)
		if err != nil {
			continue
		}
		for _, ck := range j.Cookies(u) {
			if ck.Name == "" || seen[ck.Name] {
				continue
			}
			seen[ck.Name] = true
			parts = append(parts, ck.Name+"="+ck.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func parseAttrs(s string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(s, -1) {
		val := m[2]
		if val == "" {
			val = m[3]
		}
		out[strings.ToLower(m[1])] = html.UnescapeString(val)
	}
	return out
}

func containsClass(classes, want string) bool {
	for _, c := range strings.Fields(classes) {
		if c == want || strings.Contains(c, want) {
			return true
		}
	}
	return strings.Contains(classes, want)
}

func cleanTaskName(s string) string {
	s = chapterName.ReplaceAllString(cleanText(s), "")
	return sanitizeName(s)
}

func cleanText(s string) string {
	s = regexp.MustCompile(`(?is)<script\b.*?</script>`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`(?is)<style\b.*?</style>`).ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(strings.ReplaceAll(s, "\u00a0", " "))
	s = strings.ReplaceAll(s, " ", " ")
	return strings.Join(strings.Fields(s), " ")
}

func sanitizeName(s string) string {
	s = cleanText(s)
	s = regexp.MustCompile(`[\\/:*?"<>|]+`).ReplaceAllString(s, "_")
	s = strings.TrimSpace(s)
	if s == "" {
		return "unipus"
	}
	return s
}

func tagTextInClass(body, tag, classPart string) string {
	idx := strings.Index(body, classPart)
	if idx < 0 {
		return ""
	}
	return tagTextWithClass(body[idx:], tag, "")
}

func tagTextWithClass(body, tag, classPart string) string {
	re := regexp.MustCompile(`(?is)<` + regexp.QuoteMeta(tag) + `\b([^>]*)>(.*?)</` + regexp.QuoteMeta(tag) + `>`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		attrs := parseAttrs(m[1])
		if classPart == "" || containsClass(attrs["class"], classPart) {
			if text := cleanText(m[2]); text != "" {
				return text
			}
		}
	}
	return ""
}

func absURL(raw, base string) string {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(strings.ToLower(s), "javascript:") || strings.HasPrefix(strings.ToLower(s), "data:") {
		return ""
	}
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, `\u0026`, "&")
	s = strings.ReplaceAll(s, `\u003d`, "=")
	s = strings.Trim(s, " \t\r\n\"'<>;,)")
	if strings.HasPrefix(s, "//") {
		bu, _ := url.Parse(base)
		scheme := bu.Scheme
		if scheme == "" {
			scheme = "https"
		}
		return scheme + ":" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	bu, err := url.Parse(base)
	if err != nil || bu.Scheme == "" {
		bu, _ = url.Parse(origin + "/")
	}
	return bu.ResolveReference(u).String()
}

func pickFormat(raw string) string {
	low := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(low, "data:application/vnd.apple.mpegurl") || strings.HasPrefix(low, "#extm3u") {
		return "m3u8"
	}
	u, err := url.Parse(raw)
	path := raw
	if err == nil {
		path = u.Path
	}
	m := regexp.MustCompile(`(?i)\.([a-z0-9]+)$`).FindStringSubmatch(path)
	if len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return "unknown"
}

func match1(s string, re *regexp.Regexp) string {
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
