// Package luffycity implements an extractor for luffycity.com (路飞学城) courses.
//
// API endpoints from decompiled Mooc/Courses/Luffycity/:
//
//	https://www.luffycity.com
//	https://api.luffycity.com/api/v1
//	https://hcdn2.luffycity.com   (CDN)
//	https://mts.{}.aliyuncs.com/?  (Aliyun Media Transcoding signed playback)
//
// Video playback uses polyv (hls.videocc.net) for some courses.
package luffycity

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlHome = "https://www.luffycity.com"
	urlAPI  = "https://api.luffycity.com/api/v1"
	urlCDN  = "https://hcdn2.luffycity.com"
)

var patterns = []string{`(?:[\w-]+\.)?luffycity\.com/`}

func init() {
	extractor.Register(&Luffycity{}, extractor.SiteInfo{Name: "Luffycity", URL: "luffycity.com", NeedAuth: true})
}

type Luffycity struct{}

func (l *Luffycity) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/course/(\d+)|courseId=(\d+)`)

func (l *Luffycity) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("luffycity requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse luffycity course id from URL")
	}
	return nil, fmt.Errorf("luffycity → polyv/aliyun-mts chain not yet implemented")
}
