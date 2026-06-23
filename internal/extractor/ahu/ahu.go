// Package ahu implements an extractor for ahuyikao.com (安徽医考).
//
// API endpoints from decompiled Mooc/Courses/Ahu/:
//
//	https://www.ahuyikao.com/
//	https://www.ahuyikao.com/center/mycourse.html
//	https://www.ahuyikao.com/course/courseinfo.html?courseId={cid}
package ahu

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlHome       = "https://www.ahuyikao.com/"
	urlMyCourse   = "https://www.ahuyikao.com/center/mycourse.html"
	urlCourseInfo = "https://www.ahuyikao.com/course/courseinfo.html?courseId={course_id}"
)

var patterns = []string{`(?:[\w-]+\.)?ahuyikao\.com/`}

func init() {
	extractor.Register(&Ahu{}, extractor.SiteInfo{Name: "Ahu", URL: "ahuyikao.com", NeedAuth: true})
}

type Ahu struct{}

func (a *Ahu) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`courseId=(\d+)`)

func (a *Ahu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("ahu requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse ahu courseId from URL")
	}
	return nil, fmt.Errorf("ahu courseinfo flow not yet implemented")
}
