// Package mashibing implements an extractor for mashibing.com (马士兵教育) courses.
//
// API endpoints from decompiled Mooc/Courses/Mashibing/:
//   https://gateway.mashibing.com/uaa/user
//   https://www.mashibing.com
//   https://player.polyv.net/secure/{vid}.json    (polyv DRM playback)
//   https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}
//   https://player.polyv.net/resp/vod-player-drm/canary/next/lib_player.js
//
// Video playback uses polyv DRM (hls.videocc.net + player.polyv.net).
package mashibing

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlGateway       = "https://gateway.mashibing.com/uaa/user"
	urlPolyvSecure   = "https://player.polyv.net/secure/{vid}.json"
	urlPolyvKey      = "https://hls.videocc.net/playsafe/{path1}/{path2}/{vid}_{bitrate}.key?token={token}"
	urlPolyvLibJS    = "https://player.polyv.net/resp/vod-player-drm/canary/next/lib_player.js"
)

var patterns = []string{`(?:[\w-]+\.)?mashibing\.com/`}

func init() {
	extractor.Register(&Mashibing{}, extractor.SiteInfo{Name: "Mashibing", URL: "mashibing.com", NeedAuth: true})
}

type Mashibing struct{}

func (m *Mashibing) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:courseId|cid)=(\d+)|/course/(\d+)`)

func (m *Mashibing) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("mashibing requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse mashibing course id from URL")
	}
	return nil, fmt.Errorf("mashibing → polyv DRM playback chain not yet implemented")
}
