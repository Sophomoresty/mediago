// Package open163 implements an extractor for open.163.com (网易公开课 VIP).
//
// API endpoints from decompiled Mooc/Courses/Open163/:
//
//	https://vip.open.163.com/open/trade/pc/pay/order/myOrders.do
//	https://vip.open.163.com/open/trade/pc/course/getCourseInfo.do
//	https://c.open.163.com/member/loginStatus.do
//	https://vip.open.163.com/courses/{course_id}
package open163

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlMyOrders    = "https://vip.open.163.com/open/trade/pc/pay/order/myOrders.do"
	urlCourseInfo  = "https://vip.open.163.com/open/trade/pc/course/getCourseInfo.do"
	urlLoginStatus = "https://c.open.163.com/member/loginStatus.do"
	urlCoursePage  = "https://vip.open.163.com/courses/{course_id}"
)

var patterns = []string{`(?:[\w-]+\.)?open\.163\.com/`}

func init() {
	extractor.Register(&Open163{}, extractor.SiteInfo{Name: "Open163", URL: "open.163.com", NeedAuth: true})
}

type Open163 struct{}

func (o *Open163) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/courses/(\d+)|courseId=(\d+)`)

func (o *Open163) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("open163 requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse open163 courseId from URL")
	}
	return nil, fmt.Errorf("open163 VIP course info chain not yet implemented")
}
