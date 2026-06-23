// Package xuelang implements an extractor for iyincaishijiao.com courses.
//
// API endpoints from decompiled Mooc/Courses/Xuelang/:
//
//	https://api.juejin.cn/user_api/v1/video/key_token
//	https://classroom.iyincaishijiao.com/classroom/playback/v1/enter_playback/?aid=2989
//	https://kds.bytedance.com/kds/api/v3/keys?source=jarvis&ak={kid:}&token={token:}
//	https://ke.qq.com/webcourse/287404/100471025#taid=3323810766152364&vid=5285890790569679069
//	https://mssdk.bytedance.com/web/common?msToken=
//	https://student-api.iyincaishijiao.com
//	https://student-api.iyincaishijiao.com/ep/student/course_resource/?course_id={cid:}&token={token:}&count=999
//	https://student-api.iyincaishijiao.com/ep/student/learn_data_v2/?course_count=999
package xuelang

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://api.juejin.cn/user_api/v1/video/key_token"
	url1 = "https://classroom.iyincaishijiao.com/classroom/playback/v1/enter_playback/?aid=2989"
	url2 = "https://kds.bytedance.com/kds/api/v3/keys?source=jarvis&ak={kid:}&token={token:}"
	url3 = "https://ke.qq.com/webcourse/287404/100471025#taid=3323810766152364&vid=5285890790569679069"
	url4 = "https://mssdk.bytedance.com/web/common?msToken="
	url5 = "https://student-api.iyincaishijiao.com"
	url6 = "https://student-api.iyincaishijiao.com/ep/student/course_resource/?course_id={cid:}&token={token:}&count=999"
	url7 = "https://student-api.iyincaishijiao.com/ep/student/learn_data_v2/?course_count=999"
)

var patterns = []string{`(?:[\w-]+\.)?iyincaishijiao\.com/`}

func init() {
	extractor.Register(&Xuelang{}, extractor.SiteInfo{Name: "Xuelang", URL: "iyincaishijiao.com", NeedAuth: true})
}

type Xuelang struct{}

func (s *Xuelang) Patterns() []string { return patterns }

func (s *Xuelang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("xuelang requires login cookies")
	}
	return nil, fmt.Errorf("xuelang chain not yet implemented; 8 source URL(s) recorded")
}
