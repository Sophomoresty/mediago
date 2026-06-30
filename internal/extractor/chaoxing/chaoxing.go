package chaoxing

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

var patterns = []string{
	`chaoxing\.com`,
	`xueyinonline\.com`,
}

var (
	objectIDRe     = regexp.MustCompile(`(?i)(?:objectId|objectid)=([a-z0-9_-]+)`)
	objectIDPageRe = regexp.MustCompile(`(?i)(?:objectid|objectId)\s*[:=]\s*["']([a-z0-9_-]+)["']`)
	uuidRe         = regexp.MustCompile(`(?i)(?:\?|&|&amp;)(?:uuid|liveid)=([a-z0-9_-]{8,})`)
)

func init() {
	extractor.Register(&Chaoxing{}, extractor.SiteInfo{
		Name:     "Chaoxing",
		URL:      "chaoxing.com",
		NeedAuth: true,
	})
}

type Chaoxing struct{}

func (c *Chaoxing) Patterns() []string { return patterns }

func (c *Chaoxing) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("chaoxing requires login cookies (use --cookies or --cookies-from-browser)")
	}

	client := util.NewClient()
	client.SetCookieJar(opts.Cookies)
	ctx := newChaoxingContext(client, opts.Cookies, rawURL)
	if shouldValidateChaoxingLogin(rawURL) {
		if err := validateChaoxingLogin(ctx); err != nil {
			return nil, err
		}
	}
	if isChaoxingSpaceIndexURL(rawURL) {
		return ctx.resolveSpaceIndex(rawURL)
	}

	if objectID := extractObjectID(rawURL); objectID != "" {
		entry, err := ctx.resolveObjectResource(chaoxingResource{Title: "chaoxing_video", Kind: "video", ObjectID: objectID, Ext: "mp4"})
		if err != nil {
			return nil, err
		}
		return entry, nil
	}
	if uuid := extractChaoxingUUID(rawURL); uuid != "" && strings.Contains(strings.ToLower(rawURL), "k.chaoxing.com/res/look") {
		if entry := ctx.resolveResource(chaoxingResource{Title: "chaoxing_review_" + uuid, Kind: "review", UUID: uuid}); entry != nil {
			return entry, nil
		}
	}
	if isZhiboChaoxingURL(rawURL) {
		liveID := extractZhiboLiveID(rawURL)
		uuid := extractZhiboReviewUUID(rawURL)
		if liveID != "" || uuid != "" {
			if entry, err := ctx.resolveZhiboLiveEntry(liveID, uuid); err == nil && entry != nil {
				return entry, nil
			} else if err != nil {
				return nil, err
			}
		}
	}

	course, pageObjectID, err := ctx.resolveCourse(rawURL)
	if err == nil && len(course.Entries) > 0 {
		return course, nil
	}
	if pageObjectID != "" {
		entry, derr := ctx.resolveObjectResource(chaoxingResource{Title: "chaoxing_video", Kind: "video", ObjectID: pageObjectID, Ext: "mp4"})
		if derr == nil {
			return entry, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("chaoxing: no playable course resources found")
}

func extractObjectID(raw string) string {
	if m := objectIDRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractObjectIDFromPage(text string) string {
	if m := objectIDPageRe.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractChaoxingUUID(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		for _, key := range []string{"uuid", "liveid"} {
			if v := strings.TrimSpace(u.Query().Get(key)); v != "" {
				return v
			}
		}
	}
	if m := uuidRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	return ""
}

func extractZhiboReviewUUID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !isZhiboChaoxingHost(u.Host) {
		return ""
	}
	if v := strings.TrimSpace(u.Query().Get("uuid")); v != "" {
		return v
	}
	return ""
}

func extractZhiboLiveID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !isZhiboChaoxingHost(u.Host) {
		return ""
	}
	for _, key := range []string{"liveid", "liveId", "id", "cid"} {
		for qk, vals := range u.Query() {
			if !strings.EqualFold(qk, key) || len(vals) == 0 {
				continue
			}
			if v := strings.TrimSpace(vals[0]); isDecimalID(v) {
				return v
			}
		}
	}
	for _, seg := range strings.Split(strings.Trim(strings.TrimSpace(u.Path), "/"), "/") {
		if isDecimalID(seg) {
			return seg
		}
	}
	return ""
}

func isZhiboChaoxingURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && isZhiboChaoxingHost(u.Host)
}

func isZhiboChaoxingHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "zhibo.chaoxing.com" || strings.HasSuffix(host, ".zhibo.chaoxing.com")
}

func isDecimalID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

const (
	defaultCourseHost    = "https://mooc1.chaoxing.com"
	defaultNewHost       = "https://mooc2-ans.chaoxing.com"
	defaultPublicHost    = "https://mooc1.xueyinonline.com"
	defaultLiveHost      = "https://zhibo.chaoxing.com"
	audioListURL         = "https://appswh.chaoxing.com/vclass/page/viewlist/data?uuid=%s"
	audioUpdateURL       = "https://appswh.chaoxing.com/vclass/page/update/data?pageId=%s&objectId=%s"
	defaultMeetReviewURL = "https://k.chaoxing.com/apis/chapter/getMeetReview4Job?crossOrigin=true&uuid=%s"
	defaultYunFileURL    = "https://k.chaoxing.com/apis/file/getYunFile?crossOrigin=true&objectId=%s&key="

	portalNewHeaderURL = "https://www.xueyinonline.com/portal/new-header?cur=1"
)

type chaoxingContext struct {
	c               *util.Client
	jar             http.CookieJar
	courseURL       string
	newCourseURL    string
	publicCourseURL string
	pathPrefix      string
	newCourse       bool
	courseID        string
	clazzID         string
	enc             string
	oldEnc          string
	cpi             string
	openc           string
	portalEnc       string
	portalCourseEnc string
	portalT         string
	downpath        string
	livePageURL     string
	meetReviewURL   string
	yunFileURL      string
	sourceHost      string
	title           string
	headers         map[string]string
}

func newChaoxingContext(c *util.Client, jar http.CookieJar, rawURL string) *chaoxingContext {
	ctx := &chaoxingContext{
		c:               c,
		jar:             jar,
		courseURL:       defaultCourseHost,
		newCourseURL:    defaultNewHost,
		publicCourseURL: defaultPublicHost,
		downpath:        "https://cs-ans.chaoxing.com",
		livePageURL:     defaultLiveHost,
		meetReviewURL:   defaultMeetReviewURL,
		yunFileURL:      defaultYunFileURL,
		headers: map[string]string{
			"Accept":  "text/html,application/xhtml+xml,application/xml;q=0.9,application/json,*/*;q=0.8",
			"Referer": defaultCourseHost + "/",
			"Origin":  defaultCourseHost,
		},
	}
	if u, err := url.Parse(rawURL); err == nil && u.Scheme != "" && u.Host != "" {
		ctx.applyURLContext(u.String())
	}
	if cookie := chaoxingCookieHeader(jar); cookie != "" {
		ctx.headers["Cookie"] = cookie
	}
	ctx.extractAccessFromURL(rawURL)
	return ctx
}

func chaoxingCookieHeader(jar http.CookieJar) string {
	if jar == nil {
		return ""
	}
	hosts := []string{
		"mooc1.chaoxing.com",
		"mooc2-ans.chaoxing.com",
		"i.mooc.chaoxing.com",
		"k.chaoxing.com",
		"appswh.chaoxing.com",
		"cs-ans.chaoxing.com",
		"zhibo.chaoxing.com",
		"www.xueyinonline.com",
		"mooc1.xueyinonline.com",
	}
	seen := map[string]bool{}
	var parts []string
	for _, host := range hosts {
		u := &url.URL{Scheme: "https", Host: host, Path: "/"}
		for _, c := range jar.Cookies(u) {
			name := strings.TrimSpace(c.Name)
			if name == "" {
				continue
			}
			part := name + "=" + c.Value
			if seen[part] {
				continue
			}
			seen[part] = true
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "; ")
}

func (x *chaoxingContext) abs(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(x.courseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func (x *chaoxingContext) getString(rawURL string) (string, error) {
	return x.c.GetString(rawURL, x.headers)
}

func shouldValidateChaoxingLogin(rawURL string) bool {
	low := strings.ToLower(rawURL)
	return strings.Contains(low, "i.mooc.chaoxing.com/space/index") || strings.Contains(low, "xueyinonline.com/portal/new-header")
}

func isChaoxingSchoolHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if strings.Contains(host, "xueyinonline.com") {
		return false
	}
	return true
}

func validateChaoxingLogin(ctx *chaoxingContext) error {
	if ctx == nil || ctx.sourceHost == "" {
		return nil
	}
	host := strings.ToLower(ctx.sourceHost)
	if !strings.Contains(host, "chaoxing.com") && !strings.Contains(host, "xueyinonline.com") {
		return nil
	}
	spaceURL := "https://i.mooc.chaoxing.com/space/index"
	body, err := ctx.getString(spaceURL)
	if err != nil {
		return fmt.Errorf("chaoxing login check failed: %w", err)
	}
	if !hasChaoxingPersonalName(body) {
		return fmt.Errorf("chaoxing login check failed: i.mooc.chaoxing.com/space/index missing personalName")
	}
	body, err = ctx.getString(portalNewHeaderURL)
	if err != nil {
		return fmt.Errorf("chaoxing login check failed: %w", err)
	}
	if !strings.Contains(body, `id="logout"`) {
		return fmt.Errorf("chaoxing login check failed: portal/new-header missing logout")
	}
	return nil
}

func (x *chaoxingContext) resolveZhiboLiveEntry(liveID, uuid string) (*extractor.MediaInfo, error) {
	title := x.fetchZhiboLiveTitle(liveID, uuid)
	if title == "" {
		title = "超星直播_" + firstNonEmpty(liveID, uuid)
	}
	entry := x.resolveLiveResource(chaoxingResource{
		Title:  title,
		Kind:   "live",
		LiveID: liveID,
		UUID:   uuid,
	})
	if entry == nil {
		return nil, fmt.Errorf("chaoxing: no playable zhibo live resource found")
	}
	return entry, nil
}

func (x *chaoxingContext) fetchZhiboLiveTitle(liveID, uuid string) string {
	if liveID == "" {
		return ""
	}
	base := strings.TrimRight(firstNonEmpty(x.livePageURL, defaultLiveHost), "/")
	body, err := x.getString(base + "/" + url.PathEscape(liveID))
	if err != nil || strings.TrimSpace(body) == "" {
		return ""
	}
	for _, tag := range regexp.MustCompile(`(?is)<meta\b[^>]*>`).FindAllString(body, -1) {
		attrs := htmlAttrMap(tag)
		if !strings.EqualFold(directMapString(attrs, "itemprop"), "name") {
			continue
		}
		if title := cleanText(directMapString(attrs, "content")); title != "" {
			return util.SanitizeFilename(title)
		}
	}
	if m := regexp.MustCompile(`(?is)<meta\s*itemprop="name"\s*content="([^"]*?)"\s*/?>`).FindStringSubmatch(body); len(m) > 1 {
		if title := cleanText(m[1]); title != "" {
			return util.SanitizeFilename(title)
		}
	}
	if uuid != "" {
		return util.SanitizeFilename("超星直播_" + uuid)
	}
	return ""
}
