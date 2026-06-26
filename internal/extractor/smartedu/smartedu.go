// Package smartedu implements source-aligned Smartedu static JSON extraction.
package smartedu

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL  = "https://basic.smartedu.cn/"
	loginURL    = "https://auth.smartedu.cn/uias/login"
	staticBase0 = "https://bdcs-file-2.ykt.cbern.com.cn/zxx_secondary"
	staticBase1 = "https://bdcs-file-1.ykt.cbern.com.cn/zxx_secondary"
	special0    = "https://bdcs-file-2.ykt.cbern.com.cn/zxx"
	special1    = "https://bdcs-file-1.ykt.cbern.com.cn/zxx"
	special2    = "https://s-file-2.ykt.cbern.com.cn/zxx"
	special3    = "https://s-file-1.ykt.cbern.com.cn/zxx"
	privateHost = "https://r1-ndr-private.ykt.cbern.com.cn"

	nationalResourceDetailURL            = "%s/ndrv2/national_lesson/resources/details/%s.json"
	nationalRelationResourceURL          = "%s/ndrs/national_lesson/resources/%s/relation_resource.json"
	nationalTeachingmaterialResourcesURL = "%s/ndrs/national_lesson/teachingmaterials/%s/resources/parts.json"
	prepareResourceDetailURL             = "%s/ndrv2/prepare_lesson/resources/details/%s.json"
	prepareSubTypeResourceDetailURL      = "%s/ndrv2/prepare_sub_type/resources/details/%s.json"
	prepareRelationResourceURL           = "%s/ndrs/prepare_lesson/resources/%s/relation_resource.json"
	prepareTeachingmaterialResourcesURL  = "%s/ndrs/prepare_lesson/teachingmaterials/%s/resources/parts.json"
	tchMaterialDetailURL                 = "%s/ndrv2/resources/tch_material/details/%s.json"
	tchMaterialContentURL                = "%s/api_static/contents/%s.json"
	tchMaterialThematicDetailURL         = "%s/ndrs/special_edu/resources/details/%s.json"
	tchMaterialThematicTreeURL           = "%s/ndrs/special_edu/thematic_course/trees/%s.json"
	tchMaterialThematicResourcesURL      = "%s/ndrs/special_edu/thematic_course/%s/resources/list.json"
)

var patterns = []string{`(?:[\w-]+\.)?smartedu\.cn/`}

func init() {
	extractor.Register(&Smartedu{}, extractor.SiteInfo{Name: "Smartedu", URL: "smartedu.cn", NeedAuth: true})
}

type Smartedu struct{}

func (s *Smartedu) Patterns() []string { return patterns }

type smCtx struct {
	c           *util.Client
	headers     map[string]string
	accessToken string
}

type sourceItem struct {
	kind, url, fmt, name, title, id string
	size                            int64
}

func (s *Smartedu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("smartedu requires login cookies")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	ctx := newCtx(opts.Cookies)
	q := u.Query()
	p := strings.TrimRight(u.Path, "/")
	var sources []sourceItem
	title := "smartedu"

	switch p {
	case "/tchMaterial/detail":
		contentID := firstQuery(q, "contentId", "contentid")
		if contentID == "" {
			return nil, fmt.Errorf("smartedu: missing contentId")
		}
		contentType := firstQuery(q, "contentType", "contenttype")
		if contentType == "thematic_course" {
			resources, err := ctx.loadTchMaterialThematic(contentID)
			if err != nil {
				return nil, err
			}
			sources = ctx.extractSources(resources, true, false, contentType)
			title = "thematic_" + contentID
		} else {
			detail, err := ctx.getFirst(tplURLs(tchMaterialDetailURL, staticBases(), contentID))
			if err != nil {
				return nil, err
			}
			_, _ = ctx.getFirst(tplURLs(tchMaterialContentURL, staticBases(), contentID))
			sources = ctx.extractResources(detail, contentID, false, false, contentType)
			title = firstNonEmpty(globalTitle(detail), contentID)
		}
	case "/syncClassroom":
		activityID := firstQuery(q, "activityId", "activityid")
		fromPrepare := firstQuery(q, "fromPrepare", "fromprepare") == "1"
		if activityID != "" {
			detail, err := ctx.loadActivity(activityID, fromPrepare)
			if err != nil {
				return nil, err
			}
			sources = ctx.extractResources(detail, activityID, true, fromPrepare, "")
			title = firstNonEmpty(globalTitle(detail), activityID)
		} else {
			teachingID := firstQuery(q, "teachingmaterialId", "teachingmaterialid")
			if teachingID == "" {
				return nil, fmt.Errorf("smartedu: missing activityId or teachingmaterialId")
			}
			resources, err := ctx.loadTeachingResources(teachingID)
			if err != nil {
				return nil, err
			}
			sources = ctx.extractSources(resources, true, false, "")
			title = teachingID
		}
	default:
		resourceID := firstQuery(q, "activityId", "activityid", "contentId", "contentid", "resourceId", "resourceid")
		if resourceID == "" {
			return nil, fmt.Errorf("smartedu: unsupported URL path %s", p)
		}
		detail, err := ctx.loadActivity(resourceID, false)
		if err != nil {
			return nil, err
		}
		sources = ctx.extractResources(detail, resourceID, true, false, "")
		title = firstNonEmpty(globalTitle(detail), resourceID)
	}
	return mediaFromSources(title, sources)
}

func newCtx(jar http.CookieJar) *smCtx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	cookie := cookieHeader(jar, []string{refererURL, loginURL, "https://www.smartedu.cn/"})
	h := map[string]string{"Origin": "https://basic.smartedu.cn", "Referer": refererURL, "Accept": "application/json,text/plain,*/*"}
	if cookie != "" {
		h["Cookie"] = cookie
	}
	return &smCtx{c: c, headers: h, accessToken: decodeAccessToken(cookie)}
}

func (x *smCtx) getFirst(urls []string) (map[string]any, error) {
	var last error
	for _, raw := range urls {
		body, err := x.c.GetString(raw, x.headers)
		if err != nil {
			last = err
			continue
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			last = err
			continue
		}
		if len(v) > 0 {
			return v, nil
		}
	}
	if last != nil {
		return nil, last
	}
	return nil, fmt.Errorf("smartedu: empty JSON candidates")
}

func (x *smCtx) loadActivity(id string, prepare bool) (map[string]any, error) {
	var urls []string
	if prepare {
		urls = append(urls, tplURLs(prepareSubTypeResourceDetailURL, specialBases(), id)...)
		urls = append(urls, tplURLs(prepareResourceDetailURL, specialBases(), id)...)
	}
	urls = append(urls, tplURLs(nationalResourceDetailURL, staticBases(), id)...)
	return x.getFirst(urls)
}

func (x *smCtx) loadRelation(id string, prepare bool) []map[string]any {
	tpl, bases := nationalRelationResourceURL, staticBases()
	if prepare {
		tpl, bases = prepareRelationResourceURL, specialBases()
	}
	m, err := x.getFirst(tplURLs(tpl, bases, id))
	if err != nil {
		return nil
	}
	return collectResourceMaps(m)
}

func (x *smCtx) loadTeachingResources(id string) ([]map[string]any, error) {
	m, err := x.getFirst(tplURLs(nationalTeachingmaterialResourcesURL, staticBases(), id))
	if err != nil {
		return nil, err
	}
	return collectResourceMaps(m), nil
}

func (x *smCtx) loadTchMaterialThematic(id string) ([]map[string]any, error) {
	_, _ = x.getFirst(tplURLs(tchMaterialThematicDetailURL, specialBases(), id))
	_, _ = x.getFirst(tplURLs(tchMaterialThematicTreeURL, specialBases(), id))
	m, err := x.getFirst(tplURLs(tchMaterialThematicResourcesURL, specialBases(), id))
	if err != nil {
		return nil, err
	}
	return collectResourceMaps(m), nil
}

func (x *smCtx) extractResources(detail map[string]any, id string, enrich bool, prepare bool, contentType string) []sourceItem {
	resources := relationResources(detail)
	if len(resources) == 0 {
		resources = []map[string]any{detail}
	}
	if len(resources) == 1 && id != "" {
		resources = append(resources, x.loadRelation(id, prepare)...)
	}
	return x.extractSources(resources, enrich, prepare, contentType)
}

func (x *smCtx) extractSources(resources []map[string]any, enrich bool, prepare bool, contentType string) []sourceItem {
	seen := map[string]bool{}
	var out []sourceItem
	for i, r := range resources {
		if enrich && len(items(r)) == 0 {
			if id := str(r["id"]); id != "" {
				if d, err := x.loadActivity(id, prepare); err == nil {
					r = d
				}
			}
		}
		if src := x.sourceFromResource(r, i+1, contentType); src.url != "" {
			key := src.url + "|" + src.id
			if !seen[key] {
				seen[key] = true
				out = append(out, src)
			}
		}
	}
	return out
}

func (x *smCtx) sourceFromResource(r map[string]any, idx int, contentType string) sourceItem {
	title := firstNonEmpty(globalTitle(r), str(r["id"]), fmt.Sprintf("resource_%02d", idx))
	id := str(r["id"])
	if it := selectVideoItem(r); it != nil {
		fmtv := strings.ToLower(firstNonEmpty(str(it["ti_format"]), extFormat(itemURL(it))))
		return sourceItem{kind: "video", url: x.withAccess(itemURL(it)), fmt: firstNonEmpty(fmtv, "m3u8"), name: fmt.Sprintf("(%d)--%s", idx, title), title: title, id: id, size: itemSize(it)}
	}
	if it := selectFileItem(r); it != nil {
		u := x.withAccess(itemURL(it))
		if contentType == "thematic_course" && str(it["ti_file_flag"]) == "source" {
			u = privateToPublic(u)
		}
		return sourceItem{kind: "file", url: u, fmt: strings.ToLower(firstNonEmpty(str(it["ti_format"]), extFormat(u))), name: fmt.Sprintf("(%d)--%s", idx, title), title: title, id: id, size: itemSize(it)}
	}
	return sourceItem{}
}

func (x *smCtx) withAccess(raw string) string {
	raw = normalize(raw, "")
	if raw == "" || x.accessToken == "" || !isPrivate(raw) || strings.Contains(strings.ToLower(raw), "accesstoken=") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("accessToken", x.accessToken)
	u.RawQuery = q.Encode()
	return u.String()
}

func mediaFromSources(title string, srcs []sourceItem) (*extractor.MediaInfo, error) {
	if len(srcs) == 0 {
		return nil, fmt.Errorf("smartedu: no playable resource found")
	}
	mk := func(src sourceItem) *extractor.MediaInfo {
		return &extractor.MediaInfo{Site: "smartedu", Title: src.name, Streams: map[string]extractor.Stream{"default": {Quality: src.kind, URLs: []string{src.url}, Format: firstNonEmpty(src.fmt, "mp4"), Size: src.size, Headers: map[string]string{"Referer": refererURL}}}, Extra: map[string]any{"id": src.id, "kind": src.kind, "title": src.title}}
	}
	if len(srcs) == 1 {
		m := mk(srcs[0])
		m.Title = firstNonEmpty(srcs[0].title, title)
		return m, nil
	}
	entries := make([]*extractor.MediaInfo, 0, len(srcs))
	for _, src := range srcs {
		entries = append(entries, mk(src))
	}
	return &extractor.MediaInfo{Site: "smartedu", Title: title, Entries: entries}, nil
}
