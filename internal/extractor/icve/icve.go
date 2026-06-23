// Package icve implements an extractor for icve.com.cn (智慧职教) courses.
//
// API endpoints from decompiled Mooc/Courses/Icve/:
//
//	https://ai.icve.com.cn/prod-api/course/courseInfo/getLatestInfoByCourseId?courseId={cid}
//	https://ai.icve.com.cn/prod-api/course/courseDesign/getDesignList?courseInfoId={inf_id}&courseId={cid}
//	https://ai.icve.com.cn/prod-api/course/courseDesign/getCellList?courseInfoId=&courseId=&parentId=
//	https://upload.icve.com.cn/{content}/status
package icve

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlCourseLatest = "https://ai.icve.com.cn/prod-api/course/courseInfo/getLatestInfoByCourseId?courseId={course_id}"
	urlDesignList   = "https://ai.icve.com.cn/prod-api/course/courseDesign/getDesignList?courseInfoId={inf_id}&courseId={cid}"
	urlCellList     = "https://ai.icve.com.cn/prod-api/course/courseDesign/getCellList?courseInfoId={inf_id}&courseId={cid}&parentId={parent_id}"
	urlUploadStatus = "https://upload.icve.com.cn/{content}/status"
)

var patterns = []string{`(?:[\w-]+\.)?icve\.com\.cn/`}

func init() {
	extractor.Register(&Icve{}, extractor.SiteInfo{Name: "Icve", URL: "icve.com.cn", NeedAuth: true})
}

type Icve struct{}

func (i *Icve) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`courseId=([\w-]+)`)

func (i *Icve) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("icve requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse icve courseId from URL")
	}
	return nil, fmt.Errorf("icve prod-api course chain not yet implemented")
}
