// Package meeting implements an extractor for meeting.tencent.com (腾讯会议) replays.
//
// URL pattern from decompiled Mooc/Courses/Meeting/:
//
//	https?://meeting\.tencent\.com/(?:(?:cw)|(?:cr?m)|(?:ctm?))/(?P<id>[\w-]+)
//	https?://meeting\.tencent\.com/.*?/share.*?id=(?P<id>[\w-]+)
package meeting

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const urlBase = "https://meeting.tencent.com"

var patterns = []string{`meeting\.tencent\.com/`}

func init() {
	extractor.Register(&Meeting{}, extractor.SiteInfo{Name: "Meeting", URL: "meeting.tencent.com", NeedAuth: true})
}

type Meeting struct{}

func (m *Meeting) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/(?:cw|crm?|ctm?)/([\w-]+)|/share.*?id=([\w-]+)`)

func (m *Meeting) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("meeting.tencent requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse meeting.tencent id from URL")
	}
	return nil, fmt.Errorf("meeting.tencent.com replay extraction not yet implemented")
}
