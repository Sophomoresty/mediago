// Package baijiayunxiao implements an extractor for baijiayun.com / baijiayunxiao.com
// vod replays (used by multiple parent shops like Itbaizhan etc).
//
// API endpoints from decompiled Mooc/Courses/Baijiayunxiao/Baijiayun_Video.pyc:
//
//	https://api.baijiayun.com/web/playback/getPlayInfo?room_id={}&token={}&use_encrypt=0&render=jsonp
//	https://www.baijiayun.com/vod/video/getPlayUrl?vid={live_id}&render=jsonp&token={token}&use_encrypt=0
//	https://www.baijiayun.com
package baijiayunxiao

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlGetPlayInfo = "https://api.baijiayun.com/web/playback/getPlayInfo?room_id={room_id}&token={token}&use_encrypt=0&render=jsonp"
	urlGetPlayURL  = "https://www.baijiayun.com/vod/video/getPlayUrl?vid={live_id}&render=jsonp&token={token}&use_encrypt=0"
	urlHome        = "https://www.baijiayun.com"
)

var patterns = []string{`(?:[\w-]+\.)?(?:baijiayun|baijiayunxiao)\.com/`}

func init() {
	extractor.Register(&Baijiayunxiao{}, extractor.SiteInfo{Name: "Baijiayunxiao", URL: "baijiayun.com", NeedAuth: true})
}

type Baijiayunxiao struct{}

func (b *Baijiayunxiao) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/web/room/prepare|/s/(\w+)|token=[\w-]+|vid=(\d+)|room_id=(\d+)`)

func (b *Baijiayunxiao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("baijiayunxiao requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse baijiayun playback share params from URL")
	}
	return nil, fmt.Errorf("baijiayun playback signing flow not yet implemented")
}
