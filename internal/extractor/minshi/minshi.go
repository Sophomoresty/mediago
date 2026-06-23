// Package minshi implements an extractor for minshiedu.com courses.
//
// API endpoints from decompiled Mooc/Courses/Minshi/:
//
//	https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}
//	https://player.polyv.net/secure/{vid}.json
//	https://vip.minshiedu.com/#/course/courseHome
package minshi

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}"
	url1 = "https://player.polyv.net/secure/{vid}.json"
	url2 = "https://vip.minshiedu.com/#/course/courseHome"
)

var patterns = []string{`(?:[\w-]+\.)?minshiedu\.com/`}

func init() {
	extractor.Register(&Minshi{}, extractor.SiteInfo{Name: "Minshi", URL: "minshiedu.com", NeedAuth: true})
}

type Minshi struct{}

func (s *Minshi) Patterns() []string { return patterns }

func (s *Minshi) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("minshi requires login cookies")
	}
	return nil, fmt.Errorf("minshi chain not yet implemented; 3 source URL(s) recorded")
}
