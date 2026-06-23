// Package nmkjxy implements an extractor for nmkjxy.com (柠檬云课堂).
//
// API endpoints from decompiled Mooc/Courses/Nmkjxy/:
//
//	https://www.nmkjxy.com/
//	https://api.nmkjxy.com/api/V520/RecentCourse?PageSize={}&PageIndex={}&RecentMonth=false&status=1
//	https://api.nmkjxy.com/api/product/{course_id}
package nmkjxy

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlHome         = "https://www.nmkjxy.com/"
	urlRecentCourse = "https://api.nmkjxy.com/api/V520/RecentCourse?PageSize={page_size}&PageIndex={page_index}&RecentMonth=false&status=1"
	urlProduct      = "https://api.nmkjxy.com/api/product/{course_id}"
)

var patterns = []string{`(?:[\w-]+\.)?nmkjxy\.com/`}

func init() {
	extractor.Register(&Nmkjxy{}, extractor.SiteInfo{Name: "Nmkjxy", URL: "nmkjxy.com", NeedAuth: true})
}

type Nmkjxy struct{}

func (n *Nmkjxy) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:courseId|productId|prodId)=(\d+)`)

func (n *Nmkjxy) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("nmkjxy requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse nmkjxy productId from URL")
	}
	return nil, fmt.Errorf("nmkjxy V520 RecentCourse + product flow not yet implemented")
}
