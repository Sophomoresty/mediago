// Package feishu implements an extractor for feishu.cn (Lark) docs / files /
// minutes / wiki URLs.
//
// API approach ported from decompiled Mooc/Courses/Feishu/Feishu_Course.pyc:
//
// Feishu doesn't expose a single video_info endpoint — every artefact type has
// its own embed format. The Python source distinguishes by URL pattern and:
//   - /file/{fid}    → fetch URL HTML, regex window.SERVER_DATA for "title"+"token",
//     then GET the file's preview URL from _feishu_preview_url
//   - /minutes/{fid} → fetch URL HTML, regex `"video_url":"(http...)"`
//     → unicode-unescape that captured string → mp4 URL
//   - /docx, /wiki   → document download flow (TXT/PDF), not a video extractor
//
// This Go port handles minutes (the most common video case) and surfaces a
// clear blocked error for file/docx/wiki since they aren't video flows.
package feishu

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/util"
)

var patterns = []string{
	`(?:[\w-]+\.)*feishu\.cn/(?:minutes|file|docx|docs|wiki)/`,
}

func init() {
	extractor.Register(&Feishu{}, extractor.SiteInfo{
		Name:     "Feishu",
		URL:      "feishu.cn",
		NeedAuth: true,
	})
}

type Feishu struct{}

func (f *Feishu) Patterns() []string { return patterns }

var (
	minutesRe  = regexp.MustCompile(`feishu\.cn/minutes/(\w+)`)
	fileRe     = regexp.MustCompile(`feishu\.cn/file/(\w+)`)
	docxRe     = regexp.MustCompile(`feishu\.cn/(?:docx|docs)/(\w+)`)
	wikiRe     = regexp.MustCompile(`feishu\.cn/wiki/(\w+)`)
	videoURLRe = regexp.MustCompile(`\\u0022video_url\\u0022\s*:\s*\\u0022(http[^"\\]+)\\u0022`)
	titleRe    = regexp.MustCompile(`window\.SERVER_DATA\s*=[\s\S]*?"title"\s*:\s*"([^"]+)"`)
)

func (f *Feishu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("feishu requires login cookies (use --cookies or --cookies-from-browser)")
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := map[string]string{"Referer": "https://www.feishu.cn"}

	switch {
	case minutesRe.MatchString(rawURL):
		return extractMinutes(c, rawURL, h)
	case fileRe.MatchString(rawURL):
		return nil, fmt.Errorf("feishu /file/* preview download requires the _feishu_preview_url server endpoint that needs an internal LWP token; not implemented")
	case docxRe.MatchString(rawURL), wikiRe.MatchString(rawURL):
		return nil, fmt.Errorf("feishu docx/wiki are document downloads (TXT/PDF), not video extracts")
	}
	return nil, fmt.Errorf("unsupported feishu URL shape: %s", rawURL)
}

func extractMinutes(c *util.Client, rawURL string, h map[string]string) (*extractor.MediaInfo, error) {
	body, err := c.GetString(rawURL, h)
	if err != nil {
		return nil, fmt.Errorf("fetch minutes page: %w", err)
	}

	m := videoURLRe.FindStringSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("video_url not present in minutes HTML — the recording may be private or login may have lapsed")
	}
	videoURL, err := unicodeUnescape(m[1])
	if err != nil {
		return nil, fmt.Errorf("unicode unescape video_url: %w", err)
	}

	title := "feishu_minutes"
	if mt := titleRe.FindStringSubmatch(body); len(mt) > 1 {
		title = mt[1]
	} else if mt := minutesRe.FindStringSubmatch(rawURL); len(mt) > 1 {
		title = "feishu_minutes_" + mt[1]
	}

	format := "mp4"
	if strings.Contains(videoURL, ".m3u8") {
		format = "m3u8"
	}

	return &extractor.MediaInfo{
		Site:  "feishu",
		Title: title,
		Streams: map[string]extractor.Stream{
			"default": {
				Quality: "best",
				URLs:    []string{videoURL},
				Format:  format,
				Headers: h,
			},
		},
	}, nil
}

// unicodeUnescape decodes Python-style `\uXXXX` escapes since the Python source
// captures the URL inside double-encoded JSON literals.
func unicodeUnescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+5 < len(s) && s[i] == '\\' && s[i+1] == 'u' {
			n, err := strconv.ParseUint(s[i+2:i+6], 16, 32)
			if err != nil {
				return "", err
			}
			b.WriteRune(rune(n))
			i += 6
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), nil
}
