// Package eoffcn implements an extractor for xue.eoffcn.com (中公教育 e-课堂).
//
// API chain ported from decompiled Mooc/Courses/Eoffcn/Eoffcn_Course.pyc:
//
//	https://xue.eoffcn.com/api/check/member
//	https://xue.eoffcn.com/api/order/complete
//	https://xue.eoffcn.com/api/new/goods/list
//	https://xue.eoffcn.com/api/package/list?system_order={}&coding={}
//	https://xue.eoffcn.com/api/lesson/catagory?package_id={}&system_order={}
//	https://xue.eoffcn.com/api/new/course/list?system_order={}
//	https://xue.eoffcn.com/api/lesson/detail?lesson_id={}&package_id={}&module_type={}&system_order={}
package eoffcn

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlCheckMember    = "https://xue.eoffcn.com/api/check/member"
	urlOrderComplete  = "https://xue.eoffcn.com/api/order/complete"
	urlGoodsList      = "https://xue.eoffcn.com/api/new/goods/list"
	urlPackageList    = "https://xue.eoffcn.com/api/package/list?system_order={system_order}&coding={coding}"
	urlLessonCatagory = "https://xue.eoffcn.com/api/lesson/catagory?package_id={package_id}&system_order={system_order}"
	urlCourseList     = "https://xue.eoffcn.com/api/new/course/list?system_order={system_order}"
	urlLessonDetail   = "https://xue.eoffcn.com/api/lesson/detail?lesson_id={lid}&package_id={cid}&module_type={m_type}&system_order={system_order}"
)

var patterns = []string{`(?:[\w-]+\.)?eoffcn\.com/`}

func init() {
	extractor.Register(&Eoffcn{}, extractor.SiteInfo{Name: "Eoffcn", URL: "eoffcn.com", NeedAuth: true})
}

type Eoffcn struct{}

func (e *Eoffcn) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:package_id|lesson_id|coding)=(\w+)`)

func (e *Eoffcn) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("eoffcn requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse eoffcn package/lesson id from URL")
	}
	return nil, fmt.Errorf("eoffcn package/lesson API chain not yet implemented")
}
