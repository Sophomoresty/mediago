// Package wallstreets implements an extractor for wallstreets.cn courses.
//
// API endpoints from decompiled Mooc/Courses/Wallstreets/:
//
//	https://play.qiqiuyun.net/sdk_api/play?resNo={resno}&token={token}&ssl=1&sdkType=js&lang=zh-CN
//	https://wallstreets.cn/api/me/courses?title=&limit=12&offset=0&type=learning
//	https://wallstreets.cn/api/me/courses?title=&limit=12&offset={offset}&type={ctype}
//	https://wallstreets.cn/classroom/{classroom_id}/courses
//	https://wallstreets.cn/course/{cid}/task/list/render/default
//	https://wallstreets.cn/course/{cid}/task/{vid}/activity_show
//	https://wallstreets.cn/esbar/my/classroom
//	https://wallstreets.cn/my/classrooms
package wallstreets

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://play.qiqiuyun.net/sdk_api/play?resNo={resno}&token={token}&ssl=1&sdkType=js&lang=zh-CN"
	url1 = "https://wallstreets.cn/api/me/courses?title=&limit=12&offset=0&type=learning"
	url2 = "https://wallstreets.cn/api/me/courses?title=&limit=12&offset={offset}&type={ctype}"
	url3 = "https://wallstreets.cn/classroom/{classroom_id}/courses"
	url4 = "https://wallstreets.cn/course/{cid}/task/list/render/default"
	url5 = "https://wallstreets.cn/course/{cid}/task/{vid}/activity_show"
	url6 = "https://wallstreets.cn/esbar/my/classroom"
	url7 = "https://wallstreets.cn/my/classrooms"
)

var patterns = []string{`(?:[\w-]+\.)?wallstreets\.cn/`}

func init() {
	extractor.Register(&Wallstreets{}, extractor.SiteInfo{Name: "Wallstreets", URL: "wallstreets.cn", NeedAuth: true})
}

type Wallstreets struct{}

func (s *Wallstreets) Patterns() []string { return patterns }

func (s *Wallstreets) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wallstreets requires login cookies")
	}
	return nil, fmt.Errorf("wallstreets chain not yet implemented; 8 source URL(s) recorded")
}
