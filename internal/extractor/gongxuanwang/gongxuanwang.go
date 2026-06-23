// Package gongxuanwang implements an extractor for gongxuanwang.com courses.
//
// API endpoints from decompiled Mooc/Courses/Gongxuanwang/:
//
//	https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}
//	https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/getOpenCourseDetail?courseSkuId={}
//	https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/info
//	https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/page
//	https://lms.gongxuanwang.com/api/gxw-web-student/webLive/getVidAuthorization?userId={user_id}&vid={vid}
//	https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/getWebSectionPeriodVidVO?courseSkuId={}
//	https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/microLessonCourseDetail?courseSkuId={}
//	https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/pageMicroLessonCourse
package gongxuanwang

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}"
	url1 = "https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/getOpenCourseDetail?courseSkuId={}"
	url2 = "https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/info"
	url3 = "https://lms.gongxuanwang.com/api/gxw-web-student/sku/course/page"
	url4 = "https://lms.gongxuanwang.com/api/gxw-web-student/webLive/getVidAuthorization?userId={user_id}&vid={vid}"
	url5 = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/getWebSectionPeriodVidVO?courseSkuId={}"
	url6 = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/microLessonCourseDetail?courseSkuId={}"
	url7 = "https://lms.gongxuanwang.com/api/gxw-web-student/webTimeArrange/pageMicroLessonCourse"
)

var patterns = []string{`(?:[\w-]+\.)?gongxuanwang\.com/`}

func init() {
	extractor.Register(&Gongxuanwang{}, extractor.SiteInfo{Name: "Gongxuanwang", URL: "gongxuanwang.com", NeedAuth: true})
}

type Gongxuanwang struct{}

func (s *Gongxuanwang) Patterns() []string { return patterns }

func (s *Gongxuanwang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("gongxuanwang requires login cookies")
	}
	return nil, fmt.Errorf("gongxuanwang chain not yet implemented; 8 source URL(s) recorded")
}
