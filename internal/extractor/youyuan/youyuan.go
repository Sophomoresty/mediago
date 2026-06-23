// Package youyuan implements an extractor for yijiayk.com courses.
//
// API endpoints from decompiled Mooc/Courses/Youyuan/:
//
//	https://m.yijiayk.com/course-api/app/course/getByCourseId?courseId={}
//	https://m.yijiayk.com/course-api/app/courseChapter/listPresentOrPrevious?courseId={}&annualValue=0
//	https://m.yijiayk.com/course-api/app/courseVideo/getToken?chapterId={}&cacheId=0&clientType=pc
//	https://www.baijiayun.com/vod/video/getPlayUrl?vid={}&token={}
package youyuan

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://m.yijiayk.com/course-api/app/course/getByCourseId?courseId={}"
	url1 = "https://m.yijiayk.com/course-api/app/courseChapter/listPresentOrPrevious?courseId={}&annualValue=0"
	url2 = "https://m.yijiayk.com/course-api/app/courseVideo/getToken?chapterId={}&cacheId=0&clientType=pc"
	url3 = "https://www.baijiayun.com/vod/video/getPlayUrl?vid={}&token={}"
)

var patterns = []string{`(?:[\w-]+\.)?yijiayk\.com/`}

func init() {
	extractor.Register(&Youyuan{}, extractor.SiteInfo{Name: "Youyuan", URL: "yijiayk.com", NeedAuth: true})
}

type Youyuan struct{}

func (s *Youyuan) Patterns() []string { return patterns }

func (s *Youyuan) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("youyuan requires login cookies")
	}
	return nil, fmt.Errorf("youyuan chain not yet implemented; 4 source URL(s) recorded")
}
