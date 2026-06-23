// Package haozaixian implements an extractor for zuoyebang.com courses.
//
// API endpoints from decompiled Mooc/Courses/Haozaixian/:
//
//	https://aiclass.zuoyebang.com/aiclass-course/api/lesson/getcourseinfo
//	https://aiclass.zuoyebang.com/aiclass-course/api/lesson/getdetail
//	https://aiclass.zuoyebang.com/aiclass-course/api/lesson/getvideobyroundid
//	https://c3-jx-stable.zuoyebang.com/frontcourse/public/courseemphasis/courseemphasisdetail
//	https://c3-jx-stable.zuoyebang.com/frontcourse/public/lecture/lessonlecture
//	https://c3-jx-stable.zuoyebang.com/frontcourse/teach/course/pccoursefull
//	https://c3-jx-stable.zuoyebang.com/frontcourse/teach/course/pccoursefull?courseId=0&appId=winhaoke
//	https://c3-jx-stable.zuoyebang.com/liveme/student/classroom/pre
package haozaixian

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://aiclass.zuoyebang.com/aiclass-course/api/lesson/getcourseinfo"
	url1 = "https://aiclass.zuoyebang.com/aiclass-course/api/lesson/getdetail"
	url2 = "https://aiclass.zuoyebang.com/aiclass-course/api/lesson/getvideobyroundid"
	url3 = "https://c3-jx-stable.zuoyebang.com/frontcourse/public/courseemphasis/courseemphasisdetail"
	url4 = "https://c3-jx-stable.zuoyebang.com/frontcourse/public/lecture/lessonlecture"
	url5 = "https://c3-jx-stable.zuoyebang.com/frontcourse/teach/course/pccoursefull"
	url6 = "https://c3-jx-stable.zuoyebang.com/frontcourse/teach/course/pccoursefull?courseId=0&appId=winhaoke"
	url7 = "https://c3-jx-stable.zuoyebang.com/liveme/student/classroom/pre"
)

var patterns = []string{`(?:[\w-]+\.)?zuoyebang\.com/`}

func init() {
	extractor.Register(&Haozaixian{}, extractor.SiteInfo{Name: "Haozaixian", URL: "zuoyebang.com", NeedAuth: true})
}

type Haozaixian struct{}

func (s *Haozaixian) Patterns() []string { return patterns }

func (s *Haozaixian) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("haozaixian requires login cookies")
	}
	return nil, fmt.Errorf("haozaixian chain not yet implemented; 8 source URL(s) recorded")
}
