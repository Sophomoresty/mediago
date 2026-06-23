// Package htknow implements an extractor for htknow.com courses.
//
// API endpoints from decompiled Mooc/Courses/Htknow/:
//
//	https://saas.clientapi.htknow.com/course/column_course_detail
//	https://saas.clientapi.htknow.com/course/column_course_list
//	https://saas.clientapi.htknow.com/course/column_play_details
//	https://saas.clientapi.htknow.com/course/series_course_detail
//	https://saas.clientapi.htknow.com/course/series_course_list
//	https://saas.clientapi.htknow.com/course/single_detail
//	https://saas.clientapi.htknow.com/learn/list_v2
//	https://saas.clientapi.htknow.com/live/live_wx/playback_list
package htknow

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://saas.clientapi.htknow.com/course/column_course_detail"
	url1 = "https://saas.clientapi.htknow.com/course/column_course_list"
	url2 = "https://saas.clientapi.htknow.com/course/column_play_details"
	url3 = "https://saas.clientapi.htknow.com/course/series_course_detail"
	url4 = "https://saas.clientapi.htknow.com/course/series_course_list"
	url5 = "https://saas.clientapi.htknow.com/course/single_detail"
	url6 = "https://saas.clientapi.htknow.com/learn/list_v2"
	url7 = "https://saas.clientapi.htknow.com/live/live_wx/playback_list"
)

var patterns = []string{`(?:[\w-]+\.)?htknow\.com/`}

func init() {
	extractor.Register(&Htknow{}, extractor.SiteInfo{Name: "Htknow", URL: "htknow.com", NeedAuth: true})
}

type Htknow struct{}

func (s *Htknow) Patterns() []string { return patterns }

func (s *Htknow) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("htknow requires login cookies")
	}
	return nil, fmt.Errorf("htknow chain not yet implemented; 8 source URL(s) recorded")
}
