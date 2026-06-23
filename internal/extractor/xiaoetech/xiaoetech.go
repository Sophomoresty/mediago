// Package xiaoetech implements an extractor for xiaoe-tech.com (小鹅通) courses.
package xiaoetech

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

// Endpoints from decompiled Mooc/Courses/Xiaoetech/:
const (
	urlLearnPackage = "https://{app_id}.h5.xiaoeknow.com/xe.data.learn_center.user_learn_package/1.0.0"
	urlRecordsList  = "https://{app_id}.h5.xiaoeknow.com/xe.course.business.e_course.user.learn.records.list/1.0.0"
	urlLiveList     = "https://study.xiaoe-tech.com/xe.learn-pc/living_live_list.get/1.0.0?page_size={page_size}&page_params={page_params}"
)

var patterns = []string{`(?:[\w-]+\.)?(?:xiaoe-tech\.com|xiaoeknow\.com)/`}

func init() {
	extractor.Register(&Xiaoetech{}, extractor.SiteInfo{Name: "Xiaoetech", URL: "xiaoe-tech.com", NeedAuth: true})
}

type Xiaoetech struct{}

func (x *Xiaoetech) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:resource_id|product_id|app_id|course_id)=(\w+)|/p/course/[a-z]+/(\w+)`)

func (x *Xiaoetech) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xiaoetech requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse xiaoetech course/resource id from URL")
	}
	return nil, fmt.Errorf("xiaoetech learn-pc course flow not yet implemented")
}
