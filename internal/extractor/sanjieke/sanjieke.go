// Package sanjieke implements an extractor for sanjieke.cn (三节课) courses.
package sanjieke

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

// Endpoints from decompiled Mooc/Courses/Sanjieke/:
const (
	urlStudy     = "https://study.sanjieke.cn"
	urlClassroom = "https://classroom.sanjieke.cn"
	urlMyCourse  = "https://classroom.sanjieke.cn/my_course"
)

var patterns = []string{`(?:[\w-]+\.)?sanjieke\.cn/`}

func init() {
	extractor.Register(&Sanjieke{}, extractor.SiteInfo{Name: "Sanjieke", URL: "sanjieke.cn", NeedAuth: true})
}

type Sanjieke struct{}

func (s *Sanjieke) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/(?:my_course|classroom|course)/(\w+)`)

func (s *Sanjieke) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("sanjieke requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse sanjieke id from URL")
	}
	return nil, fmt.Errorf("sanjieke classroom playback chain not yet implemented")
}
