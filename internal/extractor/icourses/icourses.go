// Package icourses implements an extractor for icourses.cn (爱课程网).
//
// API endpoints from decompiled Mooc/Courses/Icourses/:
//
//	https://www.icourses.cn
//	https://www.icourses.cn/prod/icourse-portal-api
//	https://www.icourses.cn/videoCourseDetail?courseId={cid}
//	https://www.icourses.cn/shareCourseDetail?courseId={cid}
package icourses

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlHome      = "https://www.icourses.cn"
	urlPortalAPI = "https://www.icourses.cn/prod/icourse-portal-api"
	urlVideoCD   = "https://www.icourses.cn/videoCourseDetail?courseId={course_id}"
	urlShareCD   = "https://www.icourses.cn/shareCourseDetail?courseId={course_id}"
)

var patterns = []string{`(?:[\w-]+\.)?icourses\.cn/`}

func init() {
	extractor.Register(&Icourses{}, extractor.SiteInfo{Name: "Icourses", URL: "icourses.cn", NeedAuth: true})
}

type Icourses struct{}

func (i *Icourses) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`courseId=([\w-]+)|/course/([\w-]+)`)

func (i *Icourses) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("icourses requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse icourses courseId from URL")
	}
	return nil, fmt.Errorf("icourses portal-api video chain not yet implemented")
}
