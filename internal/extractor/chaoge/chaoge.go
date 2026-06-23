// Package chaoge implements an extractor for chaogejiaoyu.com courses.
//
// API endpoints from decompiled Mooc/Courses/Chaoge/:
//
//	https://chaogejiaoyu.com/course/index/getCourseDetailAjax?id={cid}&get_offline_info=0
//	https://chaogejiaoyu.com/course/index/getCourseFileListAjax?course_id={course_id}
//	https://chaogejiaoyu.com/course/index/getSeriesCourseListAjax?pid={pid}&is_end={is_end}&page={page}&huifang_sort=1&page_size=1000
//	https://chaogejiaoyu.com/course/room/{course_id}
//	https://chaogejiaoyu.com/course/{cid}
//	https://chaogejiaoyu.com/user/index/getLoginUserInfo
//	https://chaogejiaoyu.com/user/index/getMyCourseListAjax
//	https://view.csslcloud.net/replay/data/meta
package chaoge

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://chaogejiaoyu.com/course/index/getCourseDetailAjax?id={cid}&get_offline_info=0"
	url1 = "https://chaogejiaoyu.com/course/index/getCourseFileListAjax?course_id={course_id}"
	url2 = "https://chaogejiaoyu.com/course/index/getSeriesCourseListAjax?pid={pid}&is_end={is_end}&page={page}&huifang_sort=1&page_size=1000"
	url3 = "https://chaogejiaoyu.com/course/room/{course_id}"
	url4 = "https://chaogejiaoyu.com/course/{cid}"
	url5 = "https://chaogejiaoyu.com/user/index/getLoginUserInfo"
	url6 = "https://chaogejiaoyu.com/user/index/getMyCourseListAjax"
	url7 = "https://view.csslcloud.net/replay/data/meta"
)

var patterns = []string{`(?:[\w-]+\.)?chaogejiaoyu\.com/`}

func init() {
	extractor.Register(&Chaoge{}, extractor.SiteInfo{Name: "Chaoge", URL: "chaogejiaoyu.com", NeedAuth: true})
}

type Chaoge struct{}

func (s *Chaoge) Patterns() []string { return patterns }

func (s *Chaoge) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("chaoge requires login cookies")
	}
	return nil, fmt.Errorf("chaoge chain not yet implemented; 8 source URL(s) recorded")
}
