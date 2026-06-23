// Package ckjr implements an extractor for ckjr001.com courses.
//
// API endpoints from decompiled Mooc/Courses/Ckjr/:
//
//	https://kpapiop.ckjr001.com
//	https://playvideo.qcloud.com/getplayinfo/v4/{}/{}
package ckjr

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://kpapiop.ckjr001.com"
	url1 = "https://playvideo.qcloud.com/getplayinfo/v4/{}/{}"
)

var patterns = []string{`(?:[\w-]+\.)?ckjr001\.com/`}

func init() {
	extractor.Register(&Ckjr{}, extractor.SiteInfo{Name: "Ckjr", URL: "ckjr001.com", NeedAuth: true})
}

type Ckjr struct{}

func (s *Ckjr) Patterns() []string { return patterns }

func (s *Ckjr) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("ckjr requires login cookies")
	}
	return nil, fmt.Errorf("ckjr chain not yet implemented; 2 source URL(s) recorded")
}
