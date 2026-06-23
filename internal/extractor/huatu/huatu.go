// Package huatu implements an extractor for huatu.com (华图教育) courses.
//
// API chain ported from decompiled Mooc/Courses/Huatu/Huatu_Course.pyc:
//
//	https://ocfapi.huatu.com/api/user/my_course
//	https://ocfapi.huatu.com/api/goods/syllabusBuy
//	https://ocfapi.huatu.com/api/course/goods/get_player
//	https://playvideo.vodplayvideo.net/getplayinfo/v4/{app_id}/{file_id}?psign={psign}
//	                                                    (Tencent VOD signature flow)
//
// The get_player call returns Tencent VOD app_id/file_id/psign which then go
// through playvideo.vodplayvideo.net for the manifest URL.
//
// Status: URL parsing only; full Tencent VOD signed manifest flow not implemented.
package huatu

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlMyCourse    = "https://ocfapi.huatu.com/api/user/my_course"
	urlSyllabusBuy = "https://ocfapi.huatu.com/api/goods/syllabusBuy"
	urlGetPlayer   = "https://ocfapi.huatu.com/api/course/goods/get_player"
	urlVodPlayInfo = "https://playvideo.vodplayvideo.net/getplayinfo/v4/{app_id}/{file_id}?psign={psign}"
)

var patterns = []string{`(?:[\w-]+\.)?huatu\.com/`}

func init() {
	extractor.Register(&Huatu{}, extractor.SiteInfo{Name: "Huatu", URL: "huatu.com", NeedAuth: true})
}

type Huatu struct{}

func (h *Huatu) Patterns() []string { return patterns }

var courseIDRe = regexp.MustCompile(`(?:courseId|goods_id|class_id)=(\d+)`)

func (h *Huatu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("huatu requires login cookies (use --cookies or --cookies-from-browser)")
	}
	if !courseIDRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse huatu courseId from URL: %s", rawURL)
	}
	return nil, fmt.Errorf("huatu chain (%s → %s → Tencent VOD) requires Tencent VOD signature flow; not yet implemented",
		urlGetPlayer, urlVodPlayInfo)
}
