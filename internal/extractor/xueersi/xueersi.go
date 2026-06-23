// Package xueersi implements an extractor for xueersi.com (好未来学而思) courses.
//
// API endpoints from decompiled Mooc/Courses/Xueersi/:
//
//	https://api.xueersi.com/login/V1/Web/checkLogin?X-Businessline-Id=10
//	https://i.xueersi.com/janus/App/StudyCenter/v2/courseList
//	http://i.xueersi.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList
//	http://i.xueersi.com/icenter-go/App/StudyCenter/MyPlans/planListV2
//	http://studentlive.xueersi.com/v1/student/classroom/playback/enter
package xueersi

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlCheckLogin    = "https://api.xueersi.com/login/V1/Web/checkLogin?X-Businessline-Id=10"
	urlCourseList    = "https://i.xueersi.com/janus/App/StudyCenter/v2/courseList"
	urlStuCourseList = "http://i.xueersi.com/icenter-go/App/StudyCenter/MyCourse/stuCourseList"
	urlPlanListV2    = "http://i.xueersi.com/icenter-go/App/StudyCenter/MyPlans/planListV2"
	urlPlaybackEnter = "http://studentlive.xueersi.com/v1/student/classroom/playback/enter"
)

var patterns = []string{`(?:[\w-]+\.)?xueersi\.com/`}

func init() {
	extractor.Register(&Xueersi{}, extractor.SiteInfo{Name: "Xueersi", URL: "xueersi.com", NeedAuth: true})
}

type Xueersi struct{}

func (x *Xueersi) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:planId|courseId|liveId)=(\d+)`)

func (x *Xueersi) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xueersi requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse xueersi planId/courseId from URL")
	}
	return nil, fmt.Errorf("xueersi studentlive playback chain not yet implemented")
}
