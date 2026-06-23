// Package caixuetang implements an extractor for caixuetang.cn.
//
// API endpoints from decompiled Mooc/Courses/Caixuetang/:
//
//	https://www.caixuetang.cn/
//	https://service.agent.pro.caixuetang.cn
package caixuetang

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://www.caixuetang.cn/"
	url1 = "https://service.agent.pro.caixuetang.cn"
)

var patterns = []string{`(?:[\w-]+\.)?caixuetang\.cn/`}

func init() {
	extractor.Register(&Caixuetang{}, extractor.SiteInfo{Name: "Caixuetang", URL: "caixuetang.cn", NeedAuth: true})
}

type Caixuetang struct{}

func (s *Caixuetang) Patterns() []string { return patterns }

func (s *Caixuetang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("caixuetang requires login cookies")
	}
	return nil, fmt.Errorf("caixuetang chain not yet implemented; source URL constants recorded")
}
