// Package jingtongxue implements an extractor for jingtongxue.com courses.
//
// API endpoints from decompiled Mooc/Courses/Jingtongxue/:
//
//	https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid={siteid}
//	https://www.jingtongxue.com/s/api
package jingtongxue

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	url0 = "https://p.bokecc.com/servlet/getvideofile?vid={vid}&siteid={siteid}"
	url1 = "https://www.jingtongxue.com/s/api"
)

var patterns = []string{`(?:[\w-]+\.)?jingtongxue\.com/`}

func init() {
	extractor.Register(&Jingtongxue{}, extractor.SiteInfo{Name: "Jingtongxue", URL: "jingtongxue.com", NeedAuth: true})
}

type Jingtongxue struct{}

func (s *Jingtongxue) Patterns() []string { return patterns }

func (s *Jingtongxue) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("jingtongxue requires login cookies")
	}
	return nil, fmt.Errorf("jingtongxue chain not yet implemented; 2 source URL(s) recorded")
}
