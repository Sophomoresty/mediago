// Package xiaoeapp implements an extractor for xiaoeknow.com app shops.
package xiaoeapp

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

// Endpoints from decompiled Mooc/Courses/Xiaoeapp/:
const (
	urlServer      = "https://xiaoeapp-server.xiaoeknow.com"
	urlCourseCamp  = "https://{shop}/p/course/camp/{course_id}"
	urlClockIntro  = "https://{shop}/p/t/v1/clock/e_clock/clock_h5/clockIntroduce?activity_id={activity_id}"
	urlCourseAlive = "https://{shop}/v3/course/alive/{course_id}?app_id={app_id}&type=2"
)

var patterns = []string{`(?:[\w-]+\.)?xiaoeknow\.com/`}

func init() {
	extractor.Register(&Xiaoeapp{}, extractor.SiteInfo{Name: "Xiaoeapp", URL: "xiaoeknow.com", NeedAuth: true})
}

type Xiaoeapp struct{}

func (x *Xiaoeapp) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/(?:course/(?:camp|alive))/(\w+)|activity_id=(\w+)`)

func (x *Xiaoeapp) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xiaoeapp requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse xiaoeapp course/activity id from URL")
	}
	return nil, fmt.Errorf("xiaoeapp shop flow not yet implemented")
}
