// Package keqq implements an extractor for ke.qq.com (腾讯课堂) courses.
//
// IMPORTANT: ke.qq.com (Tencent Classroom) was officially SHUT DOWN in 2024.
// The endpoints below are preserved only for historical reference and to
// surface a clear "site closed" error to users who still have old links.
//
// API endpoints from decompiled Mooc/Courses/Keqq/:
//
//	https://ke.qq.com/cgi-proxy/order/get_user_orders?page={}&count=15&tab=0&entrance=web&platform=pc
//	https://ke.qq.com/cgi-proxy/user/user_center/get_plan_list?count=10&page={}
//	https://ke.qq.com/course/{}#term_id={}
package keqq

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlOrders   = "https://ke.qq.com/cgi-proxy/order/get_user_orders?page={page}&count=15&tab=0&entrance=web&platform=pc"
	urlPlanList = "https://ke.qq.com/cgi-proxy/user/user_center/get_plan_list?count=10&page={page}"
	urlCourse   = "https://ke.qq.com/course/{course_id}#term_id={term_id}"
)

var patterns = []string{`ke\.qq\.com/`}

func init() {
	extractor.Register(&Keqq{}, extractor.SiteInfo{Name: "Keqq", URL: "ke.qq.com", NeedAuth: false})
}

type Keqq struct{}

func (k *Keqq) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/course/(\d+)|term_id=(\d+)`)

func (k *Keqq) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	_ = idRe
	return nil, fmt.Errorf("ke.qq.com (腾讯课堂) was shut down in 2024 — extractor preserved for historical reference only")
}
