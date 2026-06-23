// Package kaimingzhixue implements an extractor for lckmzx.com courses.
//
// API endpoints from decompiled Mooc/Courses/Kaimingzhixue/:
//
//	https://www.baijiayun.com/vod/video/getPlayUrl?vid={video_id}&render=jsonp&token={token}&use_encrypt=0
//	https://www.lckmzx.com/api/app/courseBasis
//	https://www.lckmzx.com/api/app/getPcRoomCode/course_id={cid}/chapter_id={chapter_id}
//	https://www.lckmzx.com/api/app/getPlayToken/chapter_id={chapter_id}/course_id={cid}
//	https://www.lckmzx.com/api/app/myStudy/course/{cid}
//	https://www.lckmzx.com/api/app/myStudy/{course_type}
package kaimingzhixue

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://www.baijiayun.com/vod/video/getPlayUrl?vid={video_id}&render=jsonp&token={token}&use_encrypt=0"
	url1 = "https://www.lckmzx.com/api/app/courseBasis"
	url2 = "https://www.lckmzx.com/api/app/getPcRoomCode/course_id={cid}/chapter_id={chapter_id}"
	url3 = "https://www.lckmzx.com/api/app/getPlayToken/chapter_id={chapter_id}/course_id={cid}"
	url4 = "https://www.lckmzx.com/api/app/myStudy/course/{cid}"
	url5 = "https://www.lckmzx.com/api/app/myStudy/{course_type}"
)

var patterns = []string{`(?:[\w-]+\.)?lckmzx\.com/`}

func init() {
	extractor.Register(&Kaimingzhixue{}, extractor.SiteInfo{Name: "Kaimingzhixue", URL: "lckmzx.com", NeedAuth: true})
}

type Kaimingzhixue struct{}

func (s *Kaimingzhixue) Patterns() []string { return patterns }

func (s *Kaimingzhixue) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("kaimingzhixue requires login cookies")
	}
	return nil, fmt.Errorf("kaimingzhixue chain not yet implemented; 6 source URL(s) recorded")
}
