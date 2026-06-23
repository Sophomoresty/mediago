// Package sier implements an extractor for sieredu.com courses.
//
// API endpoints from decompiled Mooc/Courses/Sier/:
//
//	https://api2.sieredu.com/v1/video/c/videoFile/getToken
//	https://player.sieredu.com
//	https://player.sieredu.com/
//	https://player.sieredu.com/open/play?openCourseId=512&source=homeClass
//	https://playvideo.vodplayvideo.net/getplayinfo/v4/{app_id}/{file_id}
//	https://study.sieredu.com/
//	https://www.sieredu.com/web/course/catalog/getCourseCatalogDetail
//	https://www.sieredu.com/web/course/getProductByCourseId
package sier

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://api2.sieredu.com/v1/video/c/videoFile/getToken"
	url1 = "https://player.sieredu.com"
	url2 = "https://player.sieredu.com/"
	url3 = "https://player.sieredu.com/open/play?openCourseId=512&source=homeClass"
	url4 = "https://playvideo.vodplayvideo.net/getplayinfo/v4/{app_id}/{file_id}"
	url5 = "https://study.sieredu.com/"
	url6 = "https://www.sieredu.com/web/course/catalog/getCourseCatalogDetail"
	url7 = "https://www.sieredu.com/web/course/getProductByCourseId"
)

var patterns = []string{`(?:[\w-]+\.)?sieredu\.com/`}

func init() {
	extractor.Register(&Sier{}, extractor.SiteInfo{Name: "Sier", URL: "sieredu.com", NeedAuth: true})
}

type Sier struct{}

func (s *Sier) Patterns() []string { return patterns }

func (s *Sier) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("sier requires login cookies")
	}
	return nil, fmt.Errorf("sier chain not yet implemented; 8 source URL(s) recorded")
}
