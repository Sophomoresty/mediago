// Package jianshe99 implements an extractor for jianshe99.com (建设工程教育网) courses.
//
// Endpoints from decompiled Mooc/Courses/Jianshe99/:
//   https://member.jianshe99.com/homes/mycourse
//   https://elearning.jianshe99.com/
//   https://gateway.jianshe99.com/doorman/op/
//   https://elearning.jianshe99.com/xcware/myhome/teachingMaterials.shtm?cwareID={cware_id}&identity={identity}
//
// Video playback uses csslcloud (view.csslcloud.net) — implemented in
// internal/extractor/shared/csslcloud.go (when present).
package jianshe99

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlMyCourse  = "https://member.jianshe99.com/homes/mycourse"
	urlElearning = "https://elearning.jianshe99.com/"
	urlGateway   = "https://gateway.jianshe99.com/doorman/op/"
	urlMaterial  = "https://elearning.jianshe99.com/xcware/myhome/teachingMaterials.shtm?cwareID={cware_id}&identity={identity}"
)

var patterns = []string{`(?:[\w-]+\.)?jianshe99\.com/`}

func init() {
	extractor.Register(&Jianshe99{}, extractor.SiteInfo{Name: "Jianshe99", URL: "jianshe99.com", NeedAuth: true})
}

type Jianshe99 struct{}

func (j *Jianshe99) Patterns() []string { return patterns }

var cwareRe = regexp.MustCompile(`cwareID=(\w+)`)

func (j *Jianshe99) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("jianshe99 requires login cookies (use --cookies or --cookies-from-browser)")
	}
	if !cwareRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse jianshe99 cwareID from URL: %s", rawURL)
	}
	return nil, fmt.Errorf("jianshe99 → csslcloud video chain not yet implemented")
}
