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

	if objectID := extractObjectID(rawURL); objectID != "" {
		entry, err := ctx.resolveObjectResource(chaoxingResource{Title: "chaoxing_video", Kind: "video", ObjectID: objectID, Ext: "mp4"})
		if err != nil {
			return nil, err
		}
		return entry, nil
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

const (
	defaultCourseHost = "https://mooc1.chaoxing.com"
	audioListURL      = "https://appswh.chaoxing.com/vclass/page/viewlist/data?uuid=%s"
	audioUpdateURL    = "https://appswh.chaoxing.com/vclass/page/update/data?pageId=%s&objectId=%s"
	meetReviewURL     = "https://k.chaoxing.com/apis/chapter/getMeetReview4Job?crossOrigin=true&uuid=%s"
	yunFileURL        = "https://k.chaoxing.com/apis/file/getYunFile?crossOrigin=true&objectId=%s&key="
)

type chaoxingContext struct {
	c         *util.Client
	jar       http.CookieJar
	courseURL string
	courseID  string
	clazzID   string
	enc       string
	oldEnc    string
	cpi       string
	openc     string
	downpath  string
	title     string
	headers   map[string]string
}

func newChaoxingContext(c *util.Client, jar http.CookieJar, rawURL string) *chaoxingContext {
	ctx := &chaoxingContext{
		c:         c,
		jar:       jar,
		courseURL: defaultCourseHost,
		downpath:  "https://cs-ans.chaoxing.com",
		headers: map[string]string{
			"Accept":  "text/html,application/xhtml+xml,application/xml;q=0.9,application/json,*/*;q=0.8",
			"Referer": defaultCourseHost + "/",
			"Origin":  defaultCourseHost,
		},
	}
	if u, err := url.Parse(rawURL); err == nil && u.Scheme != "" && u.Host != "" {
		host := strings.ToLower(u.Host)
		if strings.Contains(host, "chaoxing.com") && !strings.Contains(host, "mooc2-ans") {
			ctx.courseURL = u.Scheme + "://" + u.Host
			ctx.headers["Referer"] = ctx.courseURL + "/"
			ctx.headers["Origin"] = ctx.courseURL
		}
	}
	ctx.extractAccessFromURL(rawURL)
	return ctx
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
