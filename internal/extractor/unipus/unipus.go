// Package unipus implements an extractor for moocs.unipus.cn (U校园).
//
// API endpoints from decompiled Mooc/Courses/Unipus/:
//
//	https://moocs.unipus.cn
//	https://sso.unipus.cn/sso/gl/login?service=...
package unipus

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlMoocs = "https://moocs.unipus.cn"
	urlSSO   = "https://sso.unipus.cn/sso/gl/login"
)

var patterns = []string{`(?:[\w-]+\.)?unipus\.cn/`}

func init() {
	extractor.Register(&Unipus{}, extractor.SiteInfo{Name: "Unipus", URL: "unipus.cn", NeedAuth: true})
}

type Unipus struct{}

func (u *Unipus) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/courses/([\w-]+)|courseId=(\w+)`)

func (u *Unipus) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("unipus requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse unipus courseId from URL")
	}
	return nil, fmt.Errorf("unipus moocs course chain not yet implemented")
}
