// Package plaso implements an extractor for plaso.cn courses.
//
// API endpoints from decompiled Mooc/Courses/Plaso/:
//
//	https://api.polyv.net/v2/video/5153980715/get-video-info
//	https://jhpy.plaso.cn/course/api/v1/m/package/list
//	https://jhpy.plaso.cn/course/api/v1/m/package/student/list
//	https://jhpy.plaso.cn/course/api/v1/nct/m/package/task/list
//	https://jhpy.plaso.cn/liveclassgo/api/v1/history/listRecord
//	https://jhpy.plaso.cn/yxt/servlet/ali/getPlayInfo
//	https://ppt-player-wwwr.plaso.com/static/ispring/Scripts/player.js?static=1&v=202304150933
//	https://www.aiwenyun.cn/course/api/v1/m/package/list
package plaso

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://api.polyv.net/v2/video/5153980715/get-video-info"
	url1 = "https://jhpy.plaso.cn/course/api/v1/m/package/list"
	url2 = "https://jhpy.plaso.cn/course/api/v1/m/package/student/list"
	url3 = "https://jhpy.plaso.cn/course/api/v1/nct/m/package/task/list"
	url4 = "https://jhpy.plaso.cn/liveclassgo/api/v1/history/listRecord"
	url5 = "https://jhpy.plaso.cn/yxt/servlet/ali/getPlayInfo"
	url6 = "https://ppt-player-wwwr.plaso.com/static/ispring/Scripts/player.js?static=1&v=202304150933"
	url7 = "https://www.aiwenyun.cn/course/api/v1/m/package/list"
)

var patterns = []string{`(?:[\w-]+\.)?plaso\.cn/`}

func init() {
	extractor.Register(&Plaso{}, extractor.SiteInfo{Name: "Plaso", URL: "plaso.cn", NeedAuth: true})
}

type Plaso struct{}

func (s *Plaso) Patterns() []string { return patterns }

func (s *Plaso) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("plaso requires login cookies")
	}
	return nil, fmt.Errorf("plaso chain not yet implemented; 8 source URL(s) recorded")
}
