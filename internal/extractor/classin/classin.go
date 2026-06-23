// Package classin implements an extractor for eeo.cn (ClassIn) record-class
// playback shares.
//
// API endpoints from decompiled Mooc/Courses/Classin/Classin_Config.pyc:
//
//	https://w0d-cdn.eeo.cn/cloudspace/api/tencent/getM3u8Token
//	https://w0d-cdn.eeo.cn/api/classin.api.php?action=getLessonRecordInfo
//	https://a0d-cdn.eeo.cn/uc/classin_uc.php?action=getuserRecordclasses
//	https://w0d-cdn.eeo.cn/lms/app/activity/recordClass/get
package classin

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlM3u8Token   = "https://w0d-cdn.eeo.cn/cloudspace/api/tencent/getM3u8Token"
	urlLessonInfo  = "https://w0d-cdn.eeo.cn/api/classin.api.php?action=getLessonRecordInfo"
	urlUserRecords = "https://a0d-cdn.eeo.cn/uc/classin_uc.php?action=getuserRecordclasses"
	urlRecordGet   = "https://w0d-cdn.eeo.cn/lms/app/activity/recordClass/get"
	// w0s-cdn variant for some shards (mirrors w0d-cdn).
	urlW0sCDN = "https://w0s-cdn.eeo.cn/files/pm3u8/"
)

var patterns = []string{`(?:[\w-]+\.)?eeo\.cn/`}

func init() {
	extractor.Register(&Classin{}, extractor.SiteInfo{Name: "Classin", URL: "eeo.cn", NeedAuth: true})
}

type Classin struct{}

func (c *Classin) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`lessonId=(\w+)|recordId=(\w+)|/record/(\w+)`)

func (c *Classin) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("classin requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse classin lessonId/recordId from URL")
	}
	return nil, fmt.Errorf("classin lesson record + tencent m3u8 token chain not yet implemented")
}
