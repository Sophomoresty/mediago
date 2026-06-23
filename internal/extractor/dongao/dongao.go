// Package dongao implements an extractor for course.dongao.com (东奥会计在线).
//
// API endpoints from decompiled Mooc/Courses/Dongao/Dongao_Base.pyc:
//   https://serveapi.dongao.com/search/memberExamSubjectSeasonListV2
//   https://serveapi.dongao.com/search/memberServeExamList
//   https://course.dongao.com/v4/liveAndCourseList
//   https://my.dongao.com/qrcode/deviceVerify?redirectUrl=...
package dongao

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlExamSeason       = "https://serveapi.dongao.com/search/memberExamSubjectSeasonListV2"
	urlExamList         = "https://serveapi.dongao.com/search/memberServeExamList"
	urlLiveCourseList   = "https://course.dongao.com/v4/liveAndCourseList"
	urlDeviceVerify     = "https://my.dongao.com/qrcode/deviceVerify"
)

var patterns = []string{`(?:[\w-]+\.)?dongao\.com/`}

func init() {
	extractor.Register(&Dongao{}, extractor.SiteInfo{Name: "Dongao", URL: "dongao.com", NeedAuth: true})
}

type Dongao struct{}

func (d *Dongao) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:lectureId|courseId|productId)=(\w+)`)

func (d *Dongao) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("dongao requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse dongao id from URL")
	}
	return nil, fmt.Errorf("dongao serveapi+v4/liveAndCourseList chain not yet implemented")
}
