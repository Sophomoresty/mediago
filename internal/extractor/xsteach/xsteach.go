// Package xsteach implements an extractor for xsteach.com courses.
//
// API endpoints from decompiled Mooc/Courses/Xsteach/:
//
//	https://playvideo.qcloud.com/getplayinfo/v4/{}/{}
//	https://www.xsteach.com/api/common/my-course-combobox
//	https://www.xsteach.com/api/course/course-detail
//	https://www.xsteach.com/api/course/period
//	https://www.xsteach.com/api/live/enter/play
//	https://www.xsteach.com/api/period/get-period-list
//	https://www.xsteach.com/api/user/my-course/list-v3
//	https://www.xsteach.com/api/vod/period/play
package xsteach

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://playvideo.qcloud.com/getplayinfo/v4/{}/{}"
	url1 = "https://www.xsteach.com/api/common/my-course-combobox"
	url2 = "https://www.xsteach.com/api/course/course-detail"
	url3 = "https://www.xsteach.com/api/course/period"
	url4 = "https://www.xsteach.com/api/live/enter/play"
	url5 = "https://www.xsteach.com/api/period/get-period-list"
	url6 = "https://www.xsteach.com/api/user/my-course/list-v3"
	url7 = "https://www.xsteach.com/api/vod/period/play"
)

var patterns = []string{`(?:[\w-]+\.)?xsteach\.com/`}

func init() {
	extractor.Register(&Xsteach{}, extractor.SiteInfo{Name: "Xsteach", URL: "xsteach.com", NeedAuth: true})
}

type Xsteach struct{}

func (s *Xsteach) Patterns() []string { return patterns }

func (s *Xsteach) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xsteach requires login cookies")
	}
	return nil, fmt.Errorf("xsteach chain not yet implemented; 8 source URL(s) recorded")
}
