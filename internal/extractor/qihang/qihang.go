// Package qihang implements an extractor for iqihang.com courses.
//
// API endpoints from decompiled Mooc/Courses/Qihang/:
//
//	https://iqihang.com/api/ark/web/v1/product/{product_id}
//	https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid=A183AC83A2983CCC
//	https://view.csslcloud.net/api/record/vod?accountId={user_id}&recordId={record_id}&terminal=3&token={token}
//	https://view.csslcloud.net/api/room/replay/login?roomid={room_id}&userid={user_id}&recordid={record_id}&viewertoken={uid}%3A{lid}
//	https://www.iqihang.com/api/ark/web/v1/course/catalog/{cid}
//	https://www.iqihang.com/api/ark/web/v1/lecture/curriculum/node?curriculumId={cid}
//	https://www.iqihang.com/api/ark/web/v1/user/course/course-list?isMarketingCourse=&status=&type=1
//	https://www.iqihang.com/api/ark/web/v1/user/course/live/replay?liveId={live_id}
package qihang

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://iqihang.com/api/ark/web/v1/product/{product_id}"
	url1 = "https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid=A183AC83A2983CCC"
	url2 = "https://view.csslcloud.net/api/record/vod?accountId={user_id}&recordId={record_id}&terminal=3&token={token}"
	url3 = "https://view.csslcloud.net/api/room/replay/login?roomid={room_id}&userid={user_id}&recordid={record_id}&viewertoken={uid}%3A{lid}"
	url4 = "https://www.iqihang.com/api/ark/web/v1/course/catalog/{cid}"
	url5 = "https://www.iqihang.com/api/ark/web/v1/lecture/curriculum/node?curriculumId={cid}"
	url6 = "https://www.iqihang.com/api/ark/web/v1/user/course/course-list?isMarketingCourse=&status=&type=1"
	url7 = "https://www.iqihang.com/api/ark/web/v1/user/course/live/replay?liveId={live_id}"
)

var patterns = []string{`(?:[\w-]+\.)?iqihang\.com/`}

func init() {
	extractor.Register(&Qihang{}, extractor.SiteInfo{Name: "Qihang", URL: "iqihang.com", NeedAuth: true})
}

type Qihang struct{}

func (s *Qihang) Patterns() []string { return patterns }

func (s *Qihang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("qihang requires login cookies")
	}
	return nil, fmt.Errorf("qihang chain not yet implemented; 8 source URL(s) recorded")
}
