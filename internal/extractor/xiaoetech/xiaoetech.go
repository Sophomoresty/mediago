// Package xiaoetech implements an extractor for xiaoe-tech.com (小鹅通) courses.
package xiaoetech

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
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
	termURL           = "https://%s%s/xe.course.business.camp.catalog.get/2.0.0"
	pcTermURL         = "https://%s/xe.course.business.camp.catalog.get/2.0.0"
	nodeURL           = "https://%s%s/xe.course.business.camp.node.get/2.0.0"
	pcNodeURL         = "https://%s/xe.course.business.camp.node.get/2.0.0"
	videoPlayURL      = "https://%s%s/xe.material-center.play/getPlayUrl"
	sourceURL         = "https://%s%s/xe.course.business.video.detail_info.get/2.0.0"
	pcSourceURL       = "https://%s/xe.course.business.video.detail_info.get/2.0.0"
	liveURL           = "https://%s%s/_alive/v3/get_lookback_url?app_id=%s&alive_id=%s&hls_support=1"
	protectedLiveURL  = "https://%s%s/_alive/v3/get_lookback_list?app_id=%s&alive_id=%s&protection=1&client=6"
	liveTextTabURL    = "https://%s%s/_alive/tab/text?app_id=%s&alive_id=%s"
	pcLiveURL         = "https://%s/_alive/api/alive/xe.alive.page.get/1.0.0?app_id=%s"
	pptListURL        = "https://%s%s/_alive/v2/get_courseware_records?alive_id=%s"
	audioURL          = "https://%s%s/xe.course.business.audio.info.get/2.0.0"
	pcAudioURL        = "https://%s/xe.course.business.audio.info.get/2.0.0"
	textURL           = "https://%s%s/xe.course.business.get.detail/2.0.0"
	pcTextURL         = "https://%s/xe.course.business.get.detail/2.0.0"
	ebookURL          = "https://%s%s/xe.course.business.ebook.info/2.0.0"
	pcEbookURL        = "https://%s/xe.course.business.ebook.info/2.0.0"
	documentInfoURL   = "https://%s%s/xe.course.business.e_course.document_info.get/1.0.0"
	pcDocumentInfoURL = "https://%s/xe.course.business.e_course.document_info.get/1.0.0"
	htmlVideoURL      = "https://iframe.xiaoeknow.com/api/richtext/get_video_data"
	htmlAudioURL      = "https://iframe.xiaoeknow.com/api/richtext/get_audio_data"
	fileURL           = "https://%s%s/xe.course.business.courseware_list.get/2.0.0"
	pcFileURL         = "https://%s/xe.course.business.courseware_list.get/2.0.0"
	purchasedURL      = "https://%s%s/xe.course.business.resource.available.get/2.0.0"
	pcPurchasedURL    = "https://%s/xe.course.business.resource.available.get/2.0.0"
	newPurchasedURL   = "https://%s%s/xe.course.business.subscribe.check.get/2.0.0"
	pcNewPurchasedURL = "https://%s/xe.course.business.subscribe.check.get/2.0.0"
	priceURL          = "https://%s%s/xe.course.business.goods.info.get/2.0.0"
	pcPriceURL        = "https://%s/xe.course.business.goods.info.get/2.0.0"
	activityPriceURL  = "https://%s%s/xe.marketing.basic.activity.info/1.0.0"
	orderURL          = "https://%s%s/xe.transaction.order.list.get/1.0.0"
	pcOrderURL        = "https://%s/api/xe.shop.purchased.get/1.0.0?page_index=1&page_size=99"
	clockIntroURL     = "https://%s%s/punch_card/get_clock_introduction"
	clockTreeURL      = "https://%s%s/punch_card/get_chapter_tree_list"
	clockChapterURL   = "https://%s%s/punch_card/get_chapter_detail"
	trainPCClockURL   = "https://%s%s/punch_card/get_work_clock_detail"
	trainClockURL     = "https://%s%s/punch_card/get_punch_clock_theme_detail"
	pageSize          = 30
)

var patterns = []string{`(?:[\w-]+\.)*(?:xiaoe-tech\.com|xiaoeknow\.com|xiaoecloud\.com|xe-live\.com|xet(?:-pc)?\.citv\.cn)/`}
var pageRe = regexp.MustCompile(`(?i)/(?:p/course/([^/]+)/([^/?#]+)|p/t_pc/course_pc_detail/([^/]+)/([^/?#]+)|p/t_pc/goods_pc_detail/goods_detail/([^/?#]+)|v\d+/course/([^/]+)/([^/?#]+)|v\d+/goods/goods_detail/([^/?#]+))`)
var appHostRe = regexp.MustCompile(`(?i)^([a-z0-9_]+)(\.h5(?:\.[a-z0-9-]+)*\.(?:(?:xiaoeknow|xiaoecloud|xe-live)\.com|xet(?:-pc)?\.[a-z0-9.-]+)|(?:\.h5)?\.xet(?:-pc)?\.citv\.cn)$`)
var httpRe = regexp.MustCompile(`https?:\\?/\\?/[^"'\s\\]+|https?://[^"'\s]+`)

func init() {
	extractor.Register(&Xiaoetech{}, extractor.SiteInfo{Name: "Xiaoetech", URL: "xiaoe-tech.com", NeedAuth: true})
}

type Xiaoetech struct{}

func (x *Xiaoetech) Patterns() []string { return patterns }

type xetCtx struct {
	appID, xetDomain, domain, cid, typ, userID, title, referer, productID string
	pc                                                                    bool
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
	loginChecked := false
	if ctx.cid != "" {
		items = append(items, xetItem{id: ctx.cid, title: firstNonEmpty(ctx.title, ctx.cid), typ: ctx.typ, appID: ctx.appID, userID: ctx.userID, pageURL: rawURL, raw: map[string]any{"resource_id": ctx.cid, "resource_type": ctx.typ, "app_id": ctx.appID, "product_id": ctx.productID}})
	}
	if listed, err := fetchCourseList(c, opts.Cookies); err == nil {
		loginChecked = true
		items = append(items, listed...)
	} else if ctx.cid == "" {
		return nil, err
	}
	entries := []*extractor.MediaInfo{}
	blockedReasons := []string{}
	seenURL, seenItem := map[string]bool{}, map[string]bool{}
	var processItem func(xetItem, bool)
	addEntry := func(it xetItem, itemCtx xetCtx) bool {
		u, extra := resolveItem(c, opts.Cookies, itemCtx, it)
		if reason := val(extra, "blocked_reason"); reason != "" {
			blockedReasons = append(blockedReasons, reason)
			return false
		}
		if u == "" || seenURL[u] {
			return false
		}
		seenURL[u] = true
		entries = append(entries, media(firstNonEmpty(it.title, ctx.title, it.id), u, extra, referer(itemCtx)))
		return true
	}
	processItem = func(it xetItem, topLevel bool) {
		key := it.id + "|" + normType(it.typ)
		if it.id == "" || seenItem[key] || (topLevel && ctx.cid != "" && it.id != ctx.cid) {
			return
		}
		seenItem[key] = true
		itemCtx := ctx.withItem(it)
		if expanded := expandContainerItem(c, opts.Cookies, itemCtx, it); len(expanded) > 0 {
			for _, child := range expanded {
				processItem(child, false)
			}
			return
		}
		addEntry(it, itemCtx)
		for _, child := range supplementalItems(c, opts.Cookies, itemCtx, it) {
			processItem(child, false)
		}
	}
	for _, it := range items {
		processItem(it, true)
	}
	if len(entries) == 0 {
		if len(blockedReasons) > 0 {
			return nil, fmt.Errorf("blocked: %s", blockedReasons[0])
		}
		return nil, fmt.Errorf("xiaoetech: no playable URL resolved from source APIs")
	}
	metaCtx := ctx
	if ctx.cid != "" {
		for _, it := range items {
			if it.id == ctx.cid {
				metaCtx = ctx.withItem(it)
				break
			}
		}
	}
	extra := map[string]any{"course_id": ctx.cid, "app_id": metaCtx.appID, "resource_type": metaCtx.typ, "login_checked": loginChecked}
	for k, v := range fetchEntitlement(c, opts.Cookies, metaCtx) {
		extra[k] = v
	}
	return &extractor.MediaInfo{Site: "xiaoetech", Title: firstNonEmpty(ctx.title, "xiaoetech"), Entries: entries, Extra: compactMap(extra)}, nil
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
			c.referer = p.referer
		} else if p.pc && p.domain != "" {
			c.domain, c.pc = p.domain, true
			c.referer = p.referer
		}
		if p.typ != "" {
			c.typ = p.typ
		}
	}
	if parentID := val(it.raw, "_parent_id"); parentID != "" && c.cid == "" {
		c.cid = parentID
	} else if c.cid == "" {
		c.cid = it.id
	}
	if typ := normType(it.typ); typ != "" {
		c.typ = typ
	}
	if productID := firstNonEmpty(val(it.raw, "product_id"), val(it.raw, "pro_id"), val(it.raw, "course_id")); productID != "" {
		c.productID = productID
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
	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		hostname = strings.ToLower(u.Host)
	}
	if m := appHostRe.FindStringSubmatch(hostname); m != nil {
		ctx.appID, ctx.xetDomain = strings.ToLower(m[1]), normalizeXETDomain(m[2])
	} else {
		ctx.domain = u.Host
		ctx.pc = strings.Contains(hostname, "xiaoe-tech.com")
	}
	if m := pageRe.FindStringSubmatch(u.Path); m != nil {
		if m[1] != "" {
			ctx.typ, ctx.cid = m[1], m[2]
		} else if m[3] != "" {
			ctx.typ, ctx.cid, ctx.pc = m[3], m[4], true
		} else if m[5] != "" {
			ctx.cid, ctx.pc = m[5], true
		} else if m[6] != "" {
			ctx.typ, ctx.cid = m[6], m[7]
		} else {
			ctx.cid = m[8]
		}
	}
	if ctx.typ == "" && strings.Contains(strings.ToLower(u.Path), "/clock/") {
		ctx.typ = "clock"
	}
	q := u.Query()
	ctx.appID = strings.ToLower(firstNonEmpty(q.Get("app_id"), q.Get("appId"), ctx.appID))
	ctx.userID = firstNonEmpty(q.Get("user_id"), q.Get("uid"))
	ctx.productID = firstNonEmpty(q.Get("product_id"), q.Get("pro_id"), q.Get("course_id"))
	ctx.cid = firstNonEmpty(ctx.cid, q.Get("activity_id"), q.Get("resource_id"), q.Get("product_id"), q.Get("pro_id"), q.Get("course_id"), q.Get("id"))
	queryType := normType(firstNonEmpty(q.Get("resource_type"), q.Get("resourceType"), q.Get("course_type"), q.Get("courseType")))
	ctx.typ = normType(ctx.typ)
	if queryType != "" && (ctx.typ == "" || ctx.typ == "content" || ctx.typ == "goods" || ctx.typ == "detail") {
		ctx.typ = queryType
	}
	if strings.HasPrefix(strings.ToLower(ctx.cid), "d_") {
		ctx.typ = "document"
	}
	if ctx.typ == "" {
		ctx.typ = typeFromResourceID(ctx.cid)
	}
	if ctx.typ == "clock" && raw != "" {
		ctx.referer = raw
	} else {
		ctx.referer = referer(ctx)
	}
	return ctx
}

func normalizeXETDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return xetDomainDefault
	}
	domain = strings.Replace(domain, ".xet-pc.", ".xet.", 1)
	if domain == ".xet.citv.cn" {
		return ".h5.xet.citv.cn"
	}
	return domain
}

func typeFromResourceID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	switch {
	case strings.HasPrefix(id, "v_"):
		return "video"
	case strings.HasPrefix(id, "a_"):
		return "audio"
	case strings.HasPrefix(id, "l_"):
		return "live"
	case strings.HasPrefix(id, "i_"):
		return "text"
	case strings.HasPrefix(id, "d_"):
		return "document"
	case strings.HasPrefix(id, "e_"):
		return "book"
	case strings.HasPrefix(id, "p_"):
		return "column"
	case strings.HasPrefix(id, "ac_"):
		return "clock"
	case strings.HasPrefix(id, "term_"):
		return "train"
	case strings.HasPrefix(id, "course_"):
		return "ecourse"
	default:
		return ""
	}
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
