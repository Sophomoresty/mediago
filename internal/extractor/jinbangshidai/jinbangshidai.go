// Package jinbangshidai implements an extractor for baijiayun.com courses.
//
// API endpoints from decompiled Mooc/Courses/Jinbangshidai/:
//
//	https://api.baijiayun.com/web/playback/getPlayInfo?room_id={room_id:}&token={token:}&use_encrypt=0&render=jsonp
//	https://www.baijiayun.com/vod/video/getPlayUrl?vid={live_id:}&render=jsonp&token={token:}&use_encrypt=0
package jinbangshidai

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://api.baijiayun.com/web/playback/getPlayInfo?room_id={room_id:}&token={token:}&use_encrypt=0&render=jsonp"
	url1 = "https://www.baijiayun.com/vod/video/getPlayUrl?vid={live_id:}&render=jsonp&token={token:}&use_encrypt=0"
)

var patterns = []string{`(?:[\w-]+\.)?baijiayun\.com/`}

func init() {
	extractor.Register(&Jinbangshidai{}, extractor.SiteInfo{Name: "Jinbangshidai", URL: "baijiayun.com", NeedAuth: true})
}

type Jinbangshidai struct{}

func (s *Jinbangshidai) Patterns() []string { return patterns }

func (s *Jinbangshidai) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("jinbangshidai requires login cookies")
	}
	return nil, fmt.Errorf("jinbangshidai chain not yet implemented; 2 source URL(s) recorded")
}
