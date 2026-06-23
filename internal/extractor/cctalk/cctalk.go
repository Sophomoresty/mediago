// Package cctalk implements an extractor for cctalk.com (沪江CCTalk) groups.
//
// API endpoints from decompiled Mooc/Courses/cctalk/Cctalk_Course.pyc:
//
//	https://m.cctalk.com/webapi/content/v1.1/user/my_group_list?start={}&limit={}&sortType=1&keyword={}
//	https://m.cctalk.com/mycourse
//	https://m.cctalk.com
package cctalk

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlGroupList = "https://m.cctalk.com/webapi/content/v1.1/user/my_group_list?start={start}&limit={limit}&sortType=1&keyword={keyword}"
	urlMyCourse  = "https://m.cctalk.com/mycourse"
	urlMobile    = "https://m.cctalk.com"
)

var patterns = []string{`(?:[\w-]+\.)?cctalk\.com/`}

func init() {
	extractor.Register(&CCTalk{}, extractor.SiteInfo{Name: "CCTalk", URL: "cctalk.com", NeedAuth: true})
}

type CCTalk struct{}

func (c *CCTalk) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/(?:group|lecture|course)/(\w+)|groupId=(\w+)`)

func (c *CCTalk) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("cctalk requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse cctalk group/lecture id from URL")
	}
	return nil, fmt.Errorf("cctalk group lecture playback not yet implemented")
}
