// Package hqwx implements an extractor for hqwx.com (环球网校) courses.
//
// API chain ported from decompiled Mooc/Courses/Hqwx/Hqwx_Base.pyc:
//
//	https://japi.hqwx.com/uc/study/v2/getList
//	https://japi.hqwx.com/al/v3/getStagesByProduct
//	https://adminapi.hqwx.com/goods-siteapp/app/v1/course-schedules/list
//	https://adminapi.hqwx.com/goods-siteapp/app/v2/course-lessons/list
//	https://japi.hqwx.com/al/v3/selfTask/getStageTasks
//	https://japi.hqwx.com/al/userKnowledge/resource
//	https://japi.hqwx.com/al/userKnowledge/resourceBatch
package hqwx

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlGetList     = "https://japi.hqwx.com/uc/study/v2/getList"
	urlStages      = "https://japi.hqwx.com/al/v3/getStagesByProduct"
	urlSchedules   = "https://adminapi.hqwx.com/goods-siteapp/app/v1/course-schedules/list"
	urlLessons     = "https://adminapi.hqwx.com/goods-siteapp/app/v2/course-lessons/list"
	urlTasks       = "https://japi.hqwx.com/al/v3/selfTask/getStageTasks"
	urlResource    = "https://japi.hqwx.com/al/userKnowledge/resource"
	urlResourceBat = "https://japi.hqwx.com/al/userKnowledge/resourceBatch"
)

var patterns = []string{`(?:[\w-]+\.)?hqwx\.com/`}

func init() {
	extractor.Register(&Hqwx{}, extractor.SiteInfo{Name: "Hqwx", URL: "hqwx.com", NeedAuth: true})
}

type Hqwx struct{}

func (h *Hqwx) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:productId|courseId|class_id)=(\d+)`)

func (h *Hqwx) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("hqwx requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse hqwx productId/courseId from URL")
	}
	return nil, fmt.Errorf("hqwx schedules+lessons+resource chain not yet implemented")
}
