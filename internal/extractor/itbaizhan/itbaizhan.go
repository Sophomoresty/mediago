// Package itbaizhan implements an extractor for itbaizhan.com (百战程序员) courses.
//
// API endpoints from decompiled Mooc/Courses/Itbaizhan/:
//
//	https://www.itbaizhan.com/index/stage/navlist?id={course_id}&stage=0
//	https://www.itbaizhan.com/index/stage/rightlist?id={stage_id}
//	https://www.itbaizhan.com/course/id/{course_id}.html
//
// Video playback uses polyv (hls.videocc.net).
package itbaizhan

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlNavList   = "https://www.itbaizhan.com/index/stage/navlist?id={course_id}&stage=0"
	urlRightList = "https://www.itbaizhan.com/index/stage/rightlist?id={stage_id}"
	urlCoursePg  = "https://www.itbaizhan.com/course/id/{course_id}.html"
)

var patterns = []string{`(?:[\w-]+\.)?itbaizhan\.com/`}

func init() {
	extractor.Register(&Itbaizhan{}, extractor.SiteInfo{Name: "Itbaizhan", URL: "itbaizhan.com", NeedAuth: true})
}

type Itbaizhan struct{}

func (i *Itbaizhan) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/course/id/(\d+)|/stages/id/(\d+)`)

func (i *Itbaizhan) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("itbaizhan requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse itbaizhan course/stage id from URL")
	}
	return nil, fmt.Errorf("itbaizhan navlist/rightlist + polyv chain not yet implemented")
}
