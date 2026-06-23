// Package duanshu implements an extractor for {shop}.duanshu.com (短书) shops.
//
// Endpoints from decompiled Mooc/Courses/Duanshu/:
//
//	https://{shop}.duanshu.com   (article + audio + video flows)
//	https://cupsj.duanshu.com/#/brief/{type}/{id}
package duanshu

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlShopPattern = "https://{shop}.duanshu.com"
	urlCupsj       = "https://cupsj.duanshu.com/#/brief/{type}/{id}"
)

var patterns = []string{`(?:[\w-]+\.)?duanshu\.com/`}

func init() {
	extractor.Register(&Duanshu{}, extractor.SiteInfo{Name: "Duanshu", URL: "duanshu.com", NeedAuth: true})
}

type Duanshu struct{}

func (d *Duanshu) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/brief/(\w+)/(\w+)|/(?:article|audio|video)/(\w+)`)

func (d *Duanshu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("duanshu requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse duanshu brief id from URL")
	}
	return nil, fmt.Errorf("duanshu shop playback chain not yet implemented")
}
