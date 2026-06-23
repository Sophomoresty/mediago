// Package tmooc implements an extractor for tmooc.cn (达内TMOOC) courses.
//
// API endpoints from decompiled Mooc/Courses/Tmooc/:
//   https://tts10.tmooc.cn/
//   https://uc.tmooc.cn/studentCenter/toMyttsPage
//   https://ttsservice.tmooc.cn/tedu-student/v1/sso-tmooc
//   https://uc.tmooc.cn/userValidate/getUserInfo
package tmooc

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlTts10      = "https://tts10.tmooc.cn/"
	urlMyTts      = "https://uc.tmooc.cn/studentCenter/toMyttsPage"
	urlSSOService = "https://ttsservice.tmooc.cn/tedu-student/v1/sso-tmooc"
	urlUserInfo   = "https://uc.tmooc.cn/userValidate/getUserInfo"
)

var patterns = []string{`(?:[\w-]+\.)?tmooc\.cn/`}

func init() {
	extractor.Register(&Tmooc{}, extractor.SiteInfo{Name: "Tmooc", URL: "tmooc.cn", NeedAuth: true})
}

type Tmooc struct{}

func (t *Tmooc) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:courseId|cid)=(\d+)|/course/(\d+)`)

func (t *Tmooc) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("tmooc requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse tmooc course id from URL")
	}
	return nil, fmt.Errorf("tmooc sso-tmooc + playback chain not yet implemented")
}
