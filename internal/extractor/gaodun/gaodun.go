// Package gaodun implements an extractor for gaodun.com (高顿教育) courses.
//
// API chain ported from decompiled Mooc/Courses/Gaodun/Gaodun_Course.pyc:
//   https://apigateway.gaodun.com/passport/api/v3/get/glive-user-info
//   https://apigateway.gaodun.com/ep-course/api/v2/front/space/vcourse/pc
//   https://apigateway.gaodun.com/ep-study/front/course/{cid}/syllabus
//   https://apigateway.gaodun.com/g-study/api/v1/front/gl/course/gradation/{cid}
//   https://apigateway.gaodun.com/g-study/api/v1/front/course/{cid}/syllabus/glive/{syllabus_id}
//   https://apigateway.gaodun.com/glive2-vod/api/v1/live/resource?code={vid}&res={mode}
//   https://apigateway.gaodun.com/glive2-vod/api/v1/vod/check?token={token}
//
// Status: URL parsing only; full glive2-vod token + resource chain not implemented.
package gaodun

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlUserInfo    = "https://apigateway.gaodun.com/passport/api/v3/get/glive-user-info"
	urlSyllabus    = "https://apigateway.gaodun.com/ep-study/front/course/{cid}/syllabus"
	urlGliveCourse = "https://apigateway.gaodun.com/g-study/api/v1/front/gl/course/gradation/{cid}"
	urlGliveSyllab = "https://apigateway.gaodun.com/g-study/api/v1/front/course/{cid}/syllabus/glive/{syllabus_id}"
	urlVodResource = "https://apigateway.gaodun.com/glive2-vod/api/v1/live/resource?code={vid}&res={mode}"
	urlVodCheck    = "https://apigateway.gaodun.com/glive2-vod/api/v1/vod/check?token={token}"
)

var patterns = []string{`(?:[\w-]+\.)?gaodun\.com/`}

func init() {
	extractor.Register(&Gaodun{}, extractor.SiteInfo{Name: "Gaodun", URL: "gaodun.com", NeedAuth: true})
}

type Gaodun struct{}

func (g *Gaodun) Patterns() []string { return patterns }

var cidRe = regexp.MustCompile(`(?:courseId|course_id|cid)=(\d+)|/course/(\d+)`)

func (g *Gaodun) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("gaodun requires login cookies (use --cookies or --cookies-from-browser)")
	}
	if !cidRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse gaodun cid from URL: %s", rawURL)
	}
	return nil, fmt.Errorf("gaodun glive2-vod chain (%s → %s) not yet implemented", urlVodResource, urlVodCheck)
}
