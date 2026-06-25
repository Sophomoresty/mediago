// Package xiaoetech implements an extractor for xiaoe-tech.com (小鹅通) courses.
package xiaoetech

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

const (
	refererURL        = "https://study.xiaoe-tech.com"
	xetDomainDefault  = ".h5.xiaoeknow.com"
	courseListURL     = "https://study.xiaoe-tech.com/xe.learn-pc/my_attend_list.get/1.0.0?page_size=30&page=%d&agent_type=7&tab=COURSE"
	quanziListURL     = "https://study.xiaoe-tech.com/xe.learn-pc/my_attend_list.get/1.0.0?page_size=30&page=%d&agent_type=7&tab=CLASSROOM"
	attendListURL     = "https://study.xiaoe-tech.com/xe.learn-pc/my_attend_list.get/1.0.0?page_size=%d&page=%d&agent_type=7&tab=%s"
	livingLiveListURL = "https://study.xiaoe-tech.com/xe.learn-pc/living_live_list.get/1.0.0?page_size=%d&page_params=%s"
	courseURL         = "https://%s%s/p/course/%s/%s"
	pcCourseURL       = "https://%s/p/t_pc/course_pc_detail/%s/%s"
	infoURL           = "https://%s%s/xe.course.business.column.items.get/2.0.0"
	pcInfoURL         = "https://%s/xe.course.business.column.items.get/2.0.0"
	memberInfoURL     = "https://%s%s/xe.course.business.member.single_items.get/2.0.0"
	pcMemberInfoURL   = "https://%s/xe.course.business.member.single_items.get/2.0.0"
	videoPlayURL      = "https://%s%s/xe.material-center.play/getPlayUrl"
	sourceURL         = "https://%s%s/xe.course.business.video.detail_info.get/2.0.0"
	pcSourceURL       = "https://%s/xe.course.business.video.detail_info.get/2.0.0"
	liveURL           = "https://%s%s/_alive/v3/get_lookback_url?app_id=%s&alive_id=%s&hls_support=1"
	protectedLiveURL  = "https://%s%s/_alive/v3/get_lookback_list?app_id=%s&alive_id=%s&protection=1&client=6"
	pcLiveURL         = "https://%s/_alive/api/alive/xe.alive.page.get/1.0.0?app_id=%s"
	audioURL          = "https://%s%s/xe.course.business.audio.info.get/2.0.0"
	pcAudioURL        = "https://%s/xe.course.business.audio.info.get/2.0.0"
	textURL           = "https://%s%s/xe.course.business.get.detail/2.0.0"
	pcTextURL         = "https://%s/xe.course.business.get.detail/2.0.0"
	ebookURL          = "https://%s%s/xe.course.business.ebook.info/2.0.0"
	pcEbookURL        = "https://%s/xe.course.business.ebook.info/2.0.0"
	fileURL           = "https://%s%s/xe.course.business.courseware_list.get/2.0.0"
	pcFileURL         = "https://%s/xe.course.business.courseware_list.get/2.0.0"
	pageSize          = 30
)

var patterns = []string{`(?:[\w-]+\.)?(?:xiaoe-tech\.com|xiaoeknow\.com|xet\.citv\.cn|xet-pc\.citv\.cn)/`}
var pageRe = regexp.MustCompile(`(?i)/(?:p/course/([^/]+)/([^/?#]+)|p/t_pc/course_pc_detail/([^/]+)/([^/?#]+)|v\d+/course/alive/([^/?#]+)|v\d+/goods/goods_detail/([^/?#]+))`)
var appHostRe = regexp.MustCompile(`(?i)^([a-z0-9]+)(\.h5\.(?:xiaoeknow\.com|xiaoecloud\.com|xet\.citv\.cn))$`)
var httpRe = regexp.MustCompile(`https?:\\?/\\?/[^"'\s\\]+|https?://[^"'\s]+`)

func init() {
	extractor.Register(&Xiaoetech{}, extractor.SiteInfo{Name: "Xiaoetech", URL: "xiaoe-tech.com", NeedAuth: true})
}

type Xiaoetech struct{}

func (x *Xiaoetech) Patterns() []string { return patterns }

type xetCtx struct {
	appID, xetDomain, domain, cid, typ, userID, title, referer string
	pc                                                         bool
}
type xetItem struct {
	id, title, typ, appID, userID, pageURL string
	raw                                    map[string]any
}

func (x *Xiaoetech) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xiaoetech requires login cookies")
	}
	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	ctx := parseCtx(rawURL)
	ctx = enrichFromHTML(c, opts.Cookies, ctx, rawURL)
	items := []xetItem{}
	if ctx.cid != "" {
		items = append(items, xetItem{id: ctx.cid, title: firstNonEmpty(ctx.title, ctx.cid), typ: ctx.typ, appID: ctx.appID, userID: ctx.userID, pageURL: rawURL, raw: map[string]any{"resource_id": ctx.cid, "resource_type": ctx.typ, "app_id": ctx.appID}})
	}
	if listed, err := fetchCourseList(c, opts.Cookies); err == nil {
		items = append(items, listed...)
	} else if ctx.cid == "" {
		return nil, err
	}
	entries := []*extractor.MediaInfo{}
	blockedReasons := []string{}
	seenURL, seenItem := map[string]bool{}, map[string]bool{}
	for _, it := range items {
		if it.id == "" || seenItem[it.id] || (ctx.cid != "" && it.id != ctx.cid) {
			continue
		}
		seenItem[it.id] = true
		u, extra := resolveItem(c, opts.Cookies, ctx.withItem(it), it)
		if reason := val(extra, "blocked_reason"); reason != "" {
			blockedReasons = append(blockedReasons, reason)
			continue
		}
		if u == "" || seenURL[u] {
			continue
		}
		seenURL[u] = true
		entries = append(entries, media(firstNonEmpty(it.title, ctx.title, it.id), u, extra))
	}
	if len(entries) == 0 {
		if len(blockedReasons) > 0 {
			return nil, fmt.Errorf("blocked: %s", blockedReasons[0])
		}
		return nil, fmt.Errorf("xiaoetech: no playable URL resolved from source APIs")
	}
	return &extractor.MediaInfo{Site: "xiaoetech", Title: firstNonEmpty(ctx.title, "xiaoetech"), Entries: entries}, nil
}

func (c xetCtx) withItem(it xetItem) xetCtx {
	if it.appID != "" {
		c.appID = strings.ToLower(it.appID)
		c.xetDomain = firstNonEmpty(c.xetDomain, xetDomainDefault)
	}
	if it.userID != "" {
		c.userID = it.userID
	}
	if it.pageURL != "" {
		p := parseCtx(it.pageURL)
		if p.appID != "" {
			c.appID, c.xetDomain, c.pc, c.domain = p.appID, p.xetDomain, p.pc, p.domain
		}
		if p.typ != "" {
			c.typ = p.typ
		}
	}
	if c.cid == "" {
		c.cid = it.id
	}
	if c.typ == "" {
		c.typ = normType(it.typ)
	}
	if c.referer == "" {
		c.referer = referer(c)
	}
	return c
}

func parseCtx(raw string) xetCtx {
	ctx := xetCtx{xetDomain: xetDomainDefault}
	u, err := url.Parse(raw)
	if err != nil {
		return ctx
	}
	h := strings.ToLower(u.Host)
	if m := appHostRe.FindStringSubmatch(h); m != nil {
		ctx.appID, ctx.xetDomain = m[1], m[2]
	} else {
		ctx.domain = u.Host
		ctx.pc = strings.Contains(h, "xiaoe-tech.com")
	}
	if m := pageRe.FindStringSubmatch(u.Path); m != nil {
		if m[1] != "" {
			ctx.typ, ctx.cid = m[1], m[2]
		} else if m[3] != "" {
			ctx.typ, ctx.cid, ctx.pc = m[3], m[4], true
		} else if m[5] != "" {
			ctx.typ, ctx.cid = "live", m[5]
		} else {
			ctx.typ, ctx.cid = "video", m[6]
		}
	}
	q := u.Query()
	ctx.appID = firstNonEmpty(q.Get("app_id"), q.Get("appId"), ctx.appID)
	ctx.userID = firstNonEmpty(q.Get("user_id"), q.Get("uid"))
	ctx.cid = firstNonEmpty(ctx.cid, q.Get("resource_id"), q.Get("product_id"), q.Get("course_id"), q.Get("id"))
	ctx.typ = normType(ctx.typ)
	ctx.referer = referer(ctx)
	return ctx
}

func enrichFromHTML(c *util.Client, jar http.CookieJar, ctx xetCtx, raw string) xetCtx {
	if ctx.appID != "" && ctx.userID != "" {
		return ctx
	}
	body, err := c.GetString(raw, headers(jar, referer(ctx)))
	if err != nil {
		return ctx
	}
	if ctx.appID == "" {
		ctx.appID = firstRegex(`window\.APPID\s*=\s*["'](\w+)["']`, body)
	}
	if ctx.userID == "" {
		ctx.userID = firstRegex(`window\.USERID\s*=\s*["'](\w+)["']`, body)
	}
	if ctx.title == "" {
		ctx.title = firstRegex(`<title>([^<]+)</title>`, body)
	}
	return ctx
}
