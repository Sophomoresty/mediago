// Package wangxiao233 implements an extractor for wx.233.com (网校233 / 233网校).
//
// API chain ported from decompiled Mooc/Courses/Wangxiao233/Wangxiao233_Course.pyc:
//
//	https://japi.233.com/ess-ucs-api/doz/members/userInfo
//	https://japi.233.com/ess-study-api/vkt-course/list
//	https://japi.233.com/ess-study-api/user-course/list
//	https://wx.233.com/study/?productId=&childProductId=&versionProductId=&teacherId=
//	https://vod.{}.aliyuncs.com/?{}      (Aliyun VOD player URL signing)
//	https://mts.{}.aliyuncs.com/?        (Aliyun media transcoding)
//
// Video playback uses polyv (hls.videocc.net).
package wangxiao233

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlUserInfo  = "https://japi.233.com/ess-ucs-api/doz/members/userInfo"
	urlVktCourse = "https://japi.233.com/ess-study-api/vkt-course/list"
	urlUserList  = "https://japi.233.com/ess-study-api/user-course/list"
	urlAliyunVod = "https://vod.{}.aliyuncs.com/?{}"
)

var patterns = []string{`(?:[\w-]+\.)?233\.com/`}

func init() {
	extractor.Register(&Wangxiao233{}, extractor.SiteInfo{Name: "Wangxiao233", URL: "233.com", NeedAuth: true})
}

type Wangxiao233 struct{}

func (w *Wangxiao233) Patterns() []string { return patterns }

var pidRe = regexp.MustCompile(`productId=(\d+)`)

func (w *Wangxiao233) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("wangxiao233 requires login cookies")
	}
	if !pidRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse wangxiao233 productId from URL")
	}
	return nil, fmt.Errorf("wangxiao233 → polyv/aliyun-vod chain not yet implemented")
}
