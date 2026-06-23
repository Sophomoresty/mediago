// Package cnmooc implements an extractor for cnmooc.sjtu.cn (上海交大) courses.
//
// Endpoints from decompiled Mooc/Courses/Cnmooc/:
//
//	https://cnmooc.sjtu.cn   (root only — full chain not in upstream samples)
package cnmooc

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const urlHome = "https://cnmooc.sjtu.cn"

var patterns = []string{`(?:[\w-]+\.)?cnmooc\.sjtu\.cn/`}

func init() {
	extractor.Register(&Cnmooc{}, extractor.SiteInfo{Name: "Cnmooc", URL: "cnmooc.sjtu.cn", NeedAuth: true})
}

type Cnmooc struct{}

func (c *Cnmooc) Patterns() []string { return patterns }

func (c *Cnmooc) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	return nil, fmt.Errorf("cnmooc.sjtu.cn extraction flow has incomplete API samples in upstream source; not implemented (home: %s)", urlHome)
}
