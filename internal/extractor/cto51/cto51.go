// Package cto51 implements an extractor for edu.51cto.com (51CTO学院) courses.
//
// API endpoints from decompiled Mooc/Courses/Cto51/:
//
//	https://edu.51cto.com/center/course/user/get-study-course
//	https://edu.51cto.com/center/wejob/user/index?train_id={}
//	https://edu.51cto.com/center/wejob/user/course?train_id={}
//	https://e.51cto.com/study
package cto51

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlStudyCourse = "https://edu.51cto.com/center/course/user/get-study-course"
	urlWejobIndex  = "https://edu.51cto.com/center/wejob/user/index?train_id={train_id}"
	urlWejobCourse = "https://edu.51cto.com/center/wejob/user/course?train_id={train_id}"
	urlEStudy      = "https://e.51cto.com/study"
)

var patterns = []string{`(?:[\w-]+\.)?51cto\.com/`}

func init() {
	extractor.Register(&Cto51{}, extractor.SiteInfo{Name: "Cto51", URL: "51cto.com", NeedAuth: true})
}

type Cto51 struct{}

func (c *Cto51) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:train_id|course_id|cid)=(\d+)|/course/course_id/(\d+)`)

func (c *Cto51) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("51cto requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse 51cto train_id/course_id from URL")
	}
	return nil, fmt.Errorf("51cto wejob/study playback chain not yet implemented")
}
