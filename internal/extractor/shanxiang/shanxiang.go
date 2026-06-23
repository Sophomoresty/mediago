// Package shanxiang implements an extractor for sx1211.com courses.
//
// API endpoints from decompiled Mooc/Courses/Shanxiang/:
//
//	https://view.csslcloud.net/replay/data/meta
//	https://view.csslcloud.net/replay/user/login
//	https://view.csslcloud.net/replay/video/play
//	https://www.sx1211.com/User/getAjaxCourseList
//	https://www.sx1211.com/course/docview.html?product_id={cid}&doc_id={doc_id}
//	https://www.sx1211.com/course/playbackView?id={playback_id}&skuId={sku_id}&scheduleId={schedule_id}
//	https://www.sx1211.com/course/study.html?id={cid}&skuId={sku_id}
//	https://www.sx1211.com/user/course.html
package shanxiang

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://view.csslcloud.net/replay/data/meta"
	url1 = "https://view.csslcloud.net/replay/user/login"
	url2 = "https://view.csslcloud.net/replay/video/play"
	url3 = "https://www.sx1211.com/User/getAjaxCourseList"
	url4 = "https://www.sx1211.com/course/docview.html?product_id={cid}&doc_id={doc_id}"
	url5 = "https://www.sx1211.com/course/playbackView?id={playback_id}&skuId={sku_id}&scheduleId={schedule_id}"
	url6 = "https://www.sx1211.com/course/study.html?id={cid}&skuId={sku_id}"
	url7 = "https://www.sx1211.com/user/course.html"
)

var patterns = []string{`(?:[\w-]+\.)?sx1211\.com/`}

func init() {
	extractor.Register(&Shanxiang{}, extractor.SiteInfo{Name: "Shanxiang", URL: "sx1211.com", NeedAuth: true})
}

type Shanxiang struct{}

func (s *Shanxiang) Patterns() []string { return patterns }

func (s *Shanxiang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("shanxiang requires login cookies")
	}
	return nil, fmt.Errorf("shanxiang chain not yet implemented; 8 source URL(s) recorded")
}
