// Package magedu implements an extractor for edu.magedu.com (马哥教育) courses.
//
// API endpoints from decompiled Mooc/Courses/Magedu/:
//
//	https://edu.magedu.com/v1/api
//	https://edu.magedu.com/play/{vid}
//	https://edu.magedu.com/person/home/0/course
//
// Video playback uses polyv (hls.videocc.net).
package magedu

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlAPI    = "https://edu.magedu.com/v1/api"
	urlPlay   = "https://edu.magedu.com/play/{vid}"
	urlPerson = "https://edu.magedu.com/person/home/0/course"
)

var patterns = []string{`(?:[\w-]+\.)?magedu\.com/`}

func init() {
	extractor.Register(&Magedu{}, extractor.SiteInfo{Name: "Magedu", URL: "magedu.com", NeedAuth: true})
}

type Magedu struct{}

func (m *Magedu) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/(?:play|course/(?:vip|detail))/(\d+)|/play/(\d+)`)

func (m *Magedu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("magedu requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse magedu play/course id from URL")
	}
	return nil, fmt.Errorf("magedu → polyv playback chain not yet implemented")
}
