// Package yangcong implements an extractor for yangcongxueyuan.com / yangcong345.com (洋葱学院).
//
// API endpoints from decompiled Mooc/Courses/Yangcong/:
//
//	https://school.yangcongxueyuan.com/
//	https://school-api.yangcong345.com/me
//	https://school-api.yangcong345.com  (course tree + lesson detail)
package yangcong

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlSchool    = "https://school.yangcongxueyuan.com/"
	urlMe        = "https://school-api.yangcong345.com/me"
	urlSchoolAPI = "https://school-api.yangcong345.com"
)

var patterns = []string{`(?:[\w-]+\.)?(?:yangcong345|yangcongxueyuan)\.com/`}

func init() {
	extractor.Register(&Yangcong{}, extractor.SiteInfo{Name: "Yangcong", URL: "yangcongxueyuan.com", NeedAuth: true})
}

type Yangcong struct{}

func (y *Yangcong) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/(?:lesson|course|video)/(\w+)`)

func (y *Yangcong) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("yangcong requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse yangcong lesson/course id from URL")
	}
	return nil, fmt.Errorf("yangcong school-api chain not yet implemented")
}
