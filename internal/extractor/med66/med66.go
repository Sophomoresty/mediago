// Package med66 implements an extractor for med66.com (医学教育网) courses —
// shares the cdeledu.com infrastructure (sister site of jianshe99).
//
// Endpoints from decompiled Mooc/Courses/Med66/:
//
//	https://member.med66.com/homes/mycourse
//	https://member.med66.com/homes/mycourse/courseInfo
//	https://live.cdeledu.com/                   (live playback host)
//	https://www.med66.com/OtherItem/loginAgain/index.shtml
//
// Video playback uses csslcloud (view.csslcloud.net).
package med66

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlMyCourse   = "https://member.med66.com/homes/mycourse"
	urlCourseInfo = "https://member.med66.com/homes/mycourse/courseInfo"
	urlLive       = "https://live.cdeledu.com/"
	urlLoginAgain = "https://www.med66.com/OtherItem/loginAgain/index.shtml"
)

var patterns = []string{`(?:[\w-]+\.)?med66\.com/`}

func init() {
	extractor.Register(&Med66{}, extractor.SiteInfo{Name: "Med66", URL: "med66.com", NeedAuth: true})
}

type Med66 struct{}

func (m *Med66) Patterns() []string { return patterns }

var cwareRe = regexp.MustCompile(`cwareID=(\w+)`)

func (m *Med66) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("med66 requires login cookies")
	}
	if !cwareRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse med66 cwareID from URL")
	}
	return nil, fmt.Errorf("med66 → csslcloud video chain not yet implemented")
}
