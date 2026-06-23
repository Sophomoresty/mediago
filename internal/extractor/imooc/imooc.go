// Package imooc implements an extractor for imooc.com / class.imooc.com / coding.imooc.com.
//
// API chain ported from decompiled Mooc/Courses/Imooc/Imooc_Class.pyc and Imooc_Code.pyc:
//
//  1. POST {host}/course/startlearn      (class) or
//     POST {host}/lesson/ajaxstartlearn  (coding) → heartbeat / open learn session
//  2. GET  /course/playlist/{mid}?t=m3u8&_id={cid}&cdn=aliyun1
//     or
//     GET  /lesson/m3u8h5?mid={mid}&cid={cid}&ssl=1&cdn=aliyun1
//     → encoded m3u8 manifest (URLs encoded
//     by JS imooc_decode())
//  3. POST {host}/course/endlearn / ajaxendlearn → close learn session
//
// imooc_decode() runs in a JS sandbox in the original Python source. Without a
// JS engine we can't decode the URLs, so paid courses return a clear error
// while still exercising the real startlearn/endlearn lifecycle. Free imooc.com
// lessons whose JSON returns plain URLs work directly.
package imooc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`(?:[\w-]+\.)*imooc\.com/`,
}

func init() {
	extractor.Register(&Imooc{}, extractor.SiteInfo{
		Name:     "imooc",
		URL:      "imooc.com",
		NeedAuth: true,
	})
}

type Imooc struct{}

func (i *Imooc) Patterns() []string { return patterns }

func (i *Imooc) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("imooc requires login cookies (use --cookies or --cookies-from-browser)")
	}

	cid, mid, host := parseURL(rawURL)
	if cid == "" {
		return nil, fmt.Errorf("cannot parse imooc URL: %s", rawURL)
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := map[string]string{"Referer": host + "/"}

	// startlearn heartbeat — class.imooc.com uses /course/startlearn, coding.imooc.com
	// uses /lesson/ajaxstartlearn. Body just needs mid+cid (or pid+cid for class).
	startURL, endURL := lifecycleURLs(host)
	_, _ = c.PostForm(startURL, map[string]string{"mid": mid, "cid": cid, "_id": cid}, h)
	defer func() {
		_, _ = c.PostForm(endURL, map[string]string{"mid": mid, "cid": cid, "_id": cid}, h)
	}()

	// Fetch the encoded m3u8 manifest. For class.imooc.com paid content the
	// response is JSON with imooc_decode-encoded URLs we can't process without
	// a JS sandbox — return blocked rather than fabricating success.
	apiURL := mediaURL(host, mid, cid)
	body, err := c.GetString(apiURL, h)
	if err != nil {
		return nil, fmt.Errorf("fetch m3u8 manifest: %w", err)
	}

	// Free imooc.com lessons return a JSON envelope with plain "result"
	// containing m3u8 URLs.
	var free struct {
		Result string `json:"result"`
		Mpath  string `json:"mpath"`
	}
	if json.Unmarshal([]byte(body), &free) == nil && free.Result != "" {
		return buildResult(cid, free.Result, host), nil
	}
	if isM3U8(body) {
		return buildResult(cid, body, host), nil
	}

	return nil, fmt.Errorf("imooc paid content returned imooc_decode-encoded URLs (requires JS sandbox; not supported)")
}

func parseURL(u string) (cid, mid, host string) {
	host = "https://www.imooc.com"
	switch {
	case strings.Contains(u, "coding.imooc.com"):
		host = "https://coding.imooc.com"
	case strings.Contains(u, "class.imooc.com"):
		host = "https://class.imooc.com"
	}
	if m := regexp.MustCompile(`(?:class|sc|learn/list|learn|course/playlist)/(\d+)`).FindStringSubmatch(u); len(m) > 1 {
		cid = m[1]
	}
	if m := regexp.MustCompile(`(?:lesson/|video/|mid=)(\d+)`).FindStringSubmatch(u); len(m) > 1 {
		mid = m[1]
	}
	return cid, mid, host
}

func lifecycleURLs(host string) (start, end string) {
	if strings.Contains(host, "coding.imooc.com") {
		return host + "/lesson/ajaxstartlearn", host + "/lesson/ajaxendlearn"
	}
	if strings.Contains(host, "class.imooc.com") {
		return host + "/course/startlearn", host + "/course/endlearn"
	}
	// Free imooc.com — no formal lifecycle endpoint, but stub to same shape.
	return host + "/course/startlearn", host + "/course/endlearn"
}

func mediaURL(host, mid, cid string) string {
	switch {
	case strings.Contains(host, "coding.imooc.com"):
		return fmt.Sprintf("%s/lesson/m3u8h5?mid=%s&cid=%s&ssl=1&cdn=aliyun1", host, mid, cid)
	case strings.Contains(host, "class.imooc.com"):
		return fmt.Sprintf("%s/lesson/m3u8h5?mid=%s&cid=%s&ssl=1&cdn=aliyun1", host, mid, cid)
	}
	return fmt.Sprintf("%s/course/playlist/%s?t=m3u8&_id=%s&cdn=aliyun1", host, mid, cid)
}

func buildResult(cid, m3u8, host string) *extractor.MediaInfo {
	return &extractor.MediaInfo{
		Site:  "imooc",
		Title: "imooc_" + cid,
		Streams: map[string]extractor.Stream{
			"hls": {
				Quality: "default",
				URLs:    []string{m3u8},
				Format:  "m3u8",
				Headers: map[string]string{"Referer": host + "/"},
			},
		},
	}
}

func isM3U8(body string) bool {
	return strings.HasPrefix(strings.TrimSpace(body), "#EXTM3U")
}
