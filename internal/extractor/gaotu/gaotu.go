// Package gaotuk12 implements an extractor for gaotu.cn (高途课堂) — distinct
// from the gaodun.com extractor (gaodun is finance/accounting).
//
// API endpoints from decompiled Mooc/Courses/Gaotu/:
//
//	https://api.gaotu.cn/web/order/pay/shape/list
//	https://api.gaotu.cn/studyPlatform/v1/unit/clazz/list?isDebounce=true&os=h5-pc&p_client=1
//	https://interactive.gaotu.cn/live/api/studyCenter/v1/user/pc/clazz/detail
//	https://api.gaotu.cn/live/zplan/login/videoLive
//	https://interactive.gaotu.cn/live/api/live/zplan/playbackWeb
package gaotuk12

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlOrderShape  = "https://api.gaotu.cn/web/order/pay/shape/list"
	urlClazzList   = "https://api.gaotu.cn/studyPlatform/v1/unit/clazz/list?isDebounce=true&os=h5-pc&p_client=1"
	urlClazzDetail = "https://interactive.gaotu.cn/live/api/studyCenter/v1/user/pc/clazz/detail"
	urlVideoLive   = "https://api.gaotu.cn/live/zplan/login/videoLive"
	urlPlayback    = "https://interactive.gaotu.cn/live/api/live/zplan/playbackWeb"
)

var patterns = []string{`(?:[\w-]+\.)?gaotu\.cn/`}

func init() {
	extractor.Register(&Gaotu{}, extractor.SiteInfo{Name: "Gaotu", URL: "gaotu.cn", NeedAuth: true})
}

type Gaotu struct{}

func (g *Gaotu) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:clazzId|liveId|courseId)=(\d+)`)

func (g *Gaotu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("gaotu requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse gaotu clazzId/liveId from URL")
	}
	return nil, fmt.Errorf("gaotu zplan playback chain not yet implemented")
}
