// Icve_Material – www.icve.com.cn / zyk.icve.com.cn material/resource extraction.
//
// Source: Icve_Material.pyc.1shot.cdc.py
// API: zyk.icve.com.cn/prod-api/website/resource/detail/info for material details,
//      reuses Profession's source resolution for download URLs.
// Auth: requires Bearer token (NeedAuth: true).
package icve

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	materialURLDetail = "https://zyk.icve.com.cn/prod-api/website/resource/detail/info?id=%s"
)

// Source: Mooc_Config courses_re['Icve_Material']
var materialPatterns = []string{
	`\s*https?://www\.icve\.com\.cn/.*?doc[Ii]d=(?P<cid1>[-\w]+)`,
	`\s*https?://zyk\.icve\.com\.cn/materialDetailed.*?id=(?P<cid2>[-\w]+)`,
}

var materialCIDRe = regexp.MustCompile(
	`(?i)(?:doc[Ii]d=|materialDetailed.*?id=)([-\w]+)`,
)

func init() {
	extractor.Register(&IcveMaterial{}, extractor.SiteInfo{Name: "IcveMaterial", URL: "zyk.icve.com.cn/material", NeedAuth: true})
}

type IcveMaterial struct{}

func (i *IcveMaterial) Patterns() []string { return materialPatterns }

type materialCtx struct {
	c       *util.Client
	headers map[string]string
	mode    int
	cid     string
	title   string
}

func (i *IcveMaterial) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil {
		opts = &extractor.ExtractOpts{}
	}
	jar := opts.Cookies
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}

	resolved, err := resolveSmartEduURL(rawURL, jar)
	if err == nil && resolved != "" {
		rawURL = resolved
	}

	x := newMaterialCtx(jar, modeFromQuality(opts.Quality))
	x.cid = parseMaterialCID(rawURL)
	if x.cid == "" {
		return nil, fmt.Errorf("icve_material: cannot parse material id from URL")
	}

	return x.loadAndBuild()
}

func newMaterialCtx(jar http.CookieJar, mode int) *materialCtx {
	c := util.NewClient()
	c.SetCookieJar(jar)
	headers := map[string]string{
		"Sec-Fetch-Site":     "same-origin",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Sec-Ch-Ua-Platform": `"Windows"`,
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua":          `"Not/A)Brand";v="99", "Google Chrome";v="115", "Chromium";v="115"`,
		"Referer":            "https://zyk.icve.com.cn",
		"cookie":             cookieHeader(jar, []string{"https://zyk.icve.com.cn/", referer + "/", "https://www.icve.com.cn/"}),
		"User-Agent":         util.RandomUA(),
	}
	return &materialCtx{c: c, headers: headers, mode: mode}
}

func parseMaterialCID(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := materialCIDRe.FindStringSubmatch(raw); len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	for _, key := range []string{"docId", "docid", "id"} {
		if v := strings.TrimSpace(u.Query().Get(key)); v != "" {
			return v
		}
	}
	return ""
}

// loadAndBuild fetches material detail and resolves download URL.
// Source: Icve_Material._get_infos – single resource per URL.
func (x *materialCtx) loadAndBuild() (*extractor.MediaInfo, error) {
	body, err := x.c.GetString(fmt.Sprintf(materialURLDetail, url.QueryEscape(x.cid)), x.headers)
	if err != nil {
		return nil, fmt.Errorf("icve_material: load detail: %w", err)
	}
	root := parseJSONMap(body)
	data := mapAt(root, "data")

	name := cleanTitle(firstNonEmpty(str(data["name"]), str(data["title"]), str(data["resourceName"])))
	if name != "" {
		x.title = name
	}

	// Try to get file URL from various fields
	fileURL := firstNonEmpty(
		str(data["fileUrl"]),
		str(data["ossOriUrl"]),
		str(data["downloadUrl"]),
		str(data["url"]),
	)

	// If fileUrl is JSON (like in Profession), parse it
	if strings.HasPrefix(fileURL, "{") {
		innerURL := regexExtract(`"ossOriUrl"\s*:\s*"(.*?)"`, fileURL)
		if innerURL == "" {
			innerURL = regexExtract(`"fileUrl"\s*:\s*"(.*?)"`, fileURL)
		}
		if innerURL != "" {
			fileURL = innerURL
		}
	}

	// Try transcoded video URL via fileGenUrl + upload status
	fileGenURL := firstNonEmpty(str(data["fileGenUrl"]), str(data["ossGenUrl"]))
	urlShort := firstNonEmpty(str(data["urlShort"]), str(data["content"]))
	fileType := strings.ToLower(strings.TrimRight(firstNonEmpty(str(data["fileType"]), str(data["type"])), "x"))

	if fileGenURL != "" && urlShort != "" && isVideoType(fileType) {
		statusBody, err := x.c.GetString(fmt.Sprintf(urlSourceStatus, strings.TrimLeft(urlShort, "/")), x.headers)
		if err == nil {
			status := parseJSONMap(statusBody)
			args := mapAt(status, "args")
			ac := &aiCtx{c: x.c, headers: x.headers, mode: x.mode}
			u := ac.selectTranscodedURL(fileGenURL, "mp4", map[string]any{"args": args})
			if u != "" {
				fileURL = u
			}
		}
	}

	if fileURL == "" {
		return nil, fmt.Errorf("icve_material: no download URL found")
	}

	// Strip query params
	if idx := strings.LastIndex(fileURL, "?"); idx > 0 {
		fileURL = fileURL[:idx]
	}

	isVideo := isVideoType(fileType) || isVideoType(pickExt(fileURL))
	if isVideo && x.mode == ONLY_PDF {
		return nil, fmt.Errorf("icve_material: video skipped in PDF-only mode")
	}

	ext := pickExt(fileURL)
	if ext == "" {
		ext = fileType
	}
	if ext == "" {
		ext = "html"
	}

	return &extractor.MediaInfo{
		Site:  "icve",
		Title: firstNonEmpty(name, x.cid),
		Streams: map[string]extractor.Stream{
			ext: {
				Quality: ext,
				URLs:    []string{fileURL},
				Format:  ext,
				Headers: cloneHeaders(x.headers),
			},
		},
		Extra: map[string]any{"kind": fileType, "module": "material"},
	}, nil
}
