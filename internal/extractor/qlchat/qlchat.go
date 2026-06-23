// Package qlchat implements an extractor for qlchat.com (千聊) live courses.
package qlchat

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

// Endpoints from decompiled Mooc/Courses/Qlchat/:
const (
	urlMemberInfo     = "https://m.qlchat.com/api/wechat/member/memberInfo"
	urlPurchaseRecord = "https://m.qlchat.com/api/wechat/transfer/h5/topic/getPurchaseRecord"
	urlTopicDetails   = "https://m.qlchat.com/topic/details?topicId={topic_id}"
	urlTransferUser   = "https://m.qianliao.net/financial/api/transfer?url=/gate/user/getUserInfoById"
)

var patterns = []string{`(?:[\w-]+\.)?qlchat\.com/`}

func init() {
	extractor.Register(&Qlchat{}, extractor.SiteInfo{Name: "Qlchat", URL: "qlchat.com", NeedAuth: true})
}

type Qlchat struct{}

func (q *Qlchat) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`topicId=(\w+)|/live/(\w+)|/topic/(\w+)`)

func (q *Qlchat) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("qlchat requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse qlchat topicId from URL")
	}
	return nil, fmt.Errorf("qlchat topic playback flow not yet implemented")
}
