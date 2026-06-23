// Package aishangke implements an extractor for loveshangke.com courses.
//
// API endpoints from decompiled Mooc/Courses/Aishangke/:
//
//	https://loveshangke.com/course/g{cid}
//	https://loveshangke.com/course/index/enterCourse?course_id={course_id}
//	https://loveshangke.com/course/index/getCourseDetailAjax?id={cid}
//	https://loveshangke.com/course/index/getMultipleSeriesCourseListAjax?pid={pid}&is_end={is_end}&page={page}&tid=0&sid=0
//	https://loveshangke.com/course/{cid}
//	https://loveshangke.com/user/index/getLoginUserInfo
//	https://loveshangke.com/user/index/getMyCourseListAjax
//	https://view.csslcloud.net/replay/user/login
package aishangke

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://loveshangke.com/course/g{cid}"
	url1 = "https://loveshangke.com/course/index/enterCourse?course_id={course_id}"
	url2 = "https://loveshangke.com/course/index/getCourseDetailAjax?id={cid}"
	url3 = "https://loveshangke.com/course/index/getMultipleSeriesCourseListAjax?pid={pid}&is_end={is_end}&page={page}&tid=0&sid=0"
	url4 = "https://loveshangke.com/course/{cid}"
	url5 = "https://loveshangke.com/user/index/getLoginUserInfo"
	url6 = "https://loveshangke.com/user/index/getMyCourseListAjax"
	url7 = "https://view.csslcloud.net/replay/user/login"
)

var patterns = []string{`(?:[\w-]+\.)?loveshangke\.com/`}

func init() {
	extractor.Register(&Aishangke{}, extractor.SiteInfo{Name: "Aishangke", URL: "loveshangke.com", NeedAuth: true})
}

type Aishangke struct{}

func (s *Aishangke) Patterns() []string { return patterns }

func (s *Aishangke) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("aishangke requires login cookies")
	}
	return nil, fmt.Errorf("aishangke chain not yet implemented; 8 source URL(s) recorded")
}
