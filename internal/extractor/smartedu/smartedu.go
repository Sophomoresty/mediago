// Package smartedu implements an extractor for smartedu.cn (国家中小学智慧教育平台).
//
// API endpoints from decompiled Mooc/Courses/Smartedu/:
//
//	https://basic.smartedu.cn
//	https://auth.smartedu.cn/uias/login
//	https://bdcs-file-1.ykt.cbern.com.cn/zxx_secondary
package smartedu

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlBasic = "https://basic.smartedu.cn"
	urlLogin = "https://auth.smartedu.cn/uias/login"
	urlCDN   = "https://bdcs-file-1.ykt.cbern.com.cn/zxx_secondary"
)

var patterns = []string{`(?:[\w-]+\.)?smartedu\.cn/`}

func init() {
	extractor.Register(&Smartedu{}, extractor.SiteInfo{Name: "Smartedu", URL: "smartedu.cn", NeedAuth: true})
}

type Smartedu struct{}

func (s *Smartedu) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:contentId|courseId|chapterId)=(\w+)`)

func (s *Smartedu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("smartedu requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse smartedu contentId from URL")
	}
	return nil, fmt.Errorf("smartedu auth + content delivery chain not yet implemented")
}
