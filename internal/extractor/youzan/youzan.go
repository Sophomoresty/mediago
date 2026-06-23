// Package youzan implements an extractor for youzan.com knowledge shops.
//
// The decompiled Python source contains very limited URL data for Youzan
// (https://www.youzan.com home only). The full course/audio extraction flow
// needs further reverse engineering.
package youzan

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const urlHome = "https://www.youzan.com"

var patterns = []string{`(?:[\w-]+\.)?youzan\.com/`}

func init() {
	extractor.Register(&Youzan{}, extractor.SiteInfo{Name: "Youzan", URL: "youzan.com", NeedAuth: true})
}

type Youzan struct{}

func (y *Youzan) Patterns() []string { return patterns }

func (y *Youzan) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	return nil, fmt.Errorf("youzan extraction flow has incomplete API samples in upstream source; not implemented (home: %s)", urlHome)
}
