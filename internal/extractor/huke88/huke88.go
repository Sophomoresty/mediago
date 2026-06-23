// Package huke88 implements an extractor for huke88.com (虎课网) courses.
//
// API endpoints from decompiled Mooc/Courses/Huke88/:
//
//	https://huke88.com/course/{cid}.html
//	https://huke88.com/person/study/{uid}.html?page={page}&per-page=30
//	https://asyn.huke88.com/video/video-play
package huke88

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlCourse    = "https://huke88.com/course/{cid}.html"
	urlPerson    = "https://huke88.com/person/study/{uid}.html?page={page}&per-page=30"
	urlVideoPlay = "https://asyn.huke88.com/video/video-play"
)

var patterns = []string{`(?:[\w-]+\.)?huke88\.com/`}

func init() {
	extractor.Register(&Huke88{}, extractor.SiteInfo{Name: "Huke88", URL: "huke88.com", NeedAuth: true})
}

type Huke88 struct{}

func (h *Huke88) Patterns() []string { return patterns }

var cidRe = regexp.MustCompile(`/course/(\d+)\.html`)

func (h *Huke88) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("huke88 requires login cookies")
	}
	if !cidRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse huke88 course id from URL")
	}
	return nil, fmt.Errorf("huke88 video-play API chain not yet implemented")
}
