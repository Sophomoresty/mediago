// Icve_Material – www.icve.com.cn / zyk.icve.com.cn material/resource extraction.
//
// Source: Icve_Material.pyc.1shot.cdc.py
// API: zyk.icve.com.cn/prod-api/website/resource/detail/info for material details,
//
//	reuses Profession's source resolution for download URLs.
//
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
		"cookie":             cookieHeader(jar, icveCookieOrigins("https://zyk.icve.com.cn/", "https://www.icve.com.cn/")),
		"User-Agent":         util.RandomUA(),
	}
	_ = ensureICVEBearerAuth(c, headers, profURLPassLogin, profURLCheckLogin)
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
// Source: Icve_Material inherits Icve_Profession download resolution; material
// detail responses may expose direct file fields, nested resource/content
// objects, VOS resource lists, or a JSON-encoded fileUrl payload.
func (x *materialCtx) loadAndBuild() (*extractor.MediaInfo, error) {
	body, err := x.c.GetString(fmt.Sprintf(materialURLDetail, url.QueryEscape(x.cid)), x.headers)
	if err != nil {
		return nil, fmt.Errorf("icve_material: load detail: %w", err)
	}
	root := parseJSONMap(body)
	data := mapAt(root, "data")
	if len(data) == 0 {
		data = root
	}

	name := cleanTitle(firstNonEmpty(
		str(data["name"]),
		str(data["title"]),
		str(data["resourceName"]),
		str(data["fileName"]),
		str(data["filename"]),
		str(data["resName"]),
	))
	if name != "" {
		x.title = name
	}

	fileType := normalizeICVEFileType(firstNonEmpty(
		str(data["fileType"]),
		str(data["type"]),
		str(data["resourceType"]),
		str(data["suffix"]),
		str(data["fileSuffix"]),
	))
	resource := firstNonEmpty(
		str(data["fileUrl"]),
		str(data["fileInfo"]),
		str(data["resourceUrl"]),
		str(data["resource"]),
		str(data["ossOriUrl"]),
		str(data["downloadUrl"]),
		str(data["downloadurl"]),
		str(data["url"]),
	)
	if resource == "" {
		resource = jsonText(firstNonEmptyMap(data, "cloudFileInfo", "file"))
	}
	fileURL, ext, kind := x.resolveResourcePayload(resource, data, fileType)
	if fileURL == "" {
		return nil, fmt.Errorf("icve_material: no download URL found")
	}

	if idx := strings.LastIndex(fileURL, "?"); idx > 0 {
		fileURL = fileURL[:idx]
	}
	if kind == "video" && x.mode == ONLY_PDF {
		return nil, fmt.Errorf("icve_material: video skipped in PDF-only mode")
	}
	if ext == "" {
		ext = pickExt(fileURL)
	}
	if ext == "" && fileType != "" && !isVideoType(fileType) {
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
				Quality:   ext,
				URLs:      []string{fileURL},
				Format:    ext,
				NeedMerge: ext == "m3u8",
				Headers:   cloneHeaders(x.headers),
			},
		},
		Extra: map[string]any{"kind": kind, "file_type": fileType, "module": "material"},
	}, nil
}

func (x *materialCtx) resolveResourcePayload(resource string, data map[string]any, fileType string) (string, string, string) {
	payload := parseICVEResourcePayload(resource)
	if len(payload) == 0 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(resource)), "http") {
		payload["fileUrl"] = resource
	}
	for k, v := range data {
		if _, exists := payload[k]; !exists {
			payload[k] = v
		}
	}
	return resolveICVEResourceMedia(x.c, x.headers, x.mode, payload, fileType)
}
