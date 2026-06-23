// Package renrenjiang implements an extractor for renrenjiang.cn (人人讲) courses.
package renrenjiang

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

// Endpoints from decompiled Mooc/Courses/Renrenjiang/:
const (
	urlAPI       = "https://api.renrenjiang.cn"
	urlKE        = "https://ke.renrenjiang.cn"
	urlQCloudVod = "https://playvideo.qcloud.com/getplayinfo/v4/{appId}/{fileId}"
)

var patterns = []string{`(?:[\w-]+\.)?renrenjiang\.cn/`}

func init() {
	extractor.Register(&Renrenjiang{}, extractor.SiteInfo{Name: "Renrenjiang", URL: "renrenjiang.cn", NeedAuth: true})
}

type Renrenjiang struct{}

func (r *Renrenjiang) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/(?:live|series|course)/(\w+)|courseId=(\w+)`)

func (r *Renrenjiang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("renrenjiang requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse renrenjiang id from URL")
	}
	return nil, fmt.Errorf("renrenjiang → Tencent VOD playback chain not yet implemented")
}
