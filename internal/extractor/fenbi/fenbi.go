// Package fenbi implements an extractor for ke.fenbi.com (粉笔教育) lectures.
//
// API chain mapped from decompiled Mooc/Courses/Fenbi/Fenbi_Course.pyc:
//
//	Authentication probes:
//	  https://login.fenbi.com/api/users/current
//	  https://ke.fenbi.com/win/v3/users/current?nickname=true
//	Course tree:
//	  https://ke.fenbi.com/win/{prefix}/v3/my/lectures/visible?start={s}&len={n}
//	  https://ke.fenbi.com/win/{prefix}/v3/lectures/{lecture_id}
//	  https://ke.fenbi.com/api/{prefix}/v3/lectures/{lecture_id}
//	  https://ke.fenbi.com/win/{prefix}/v3/my/lectures/{lecture_id}/summary
//	The lecture object contains episode IDs that resolve to MP4 URLs through
//	protected per-prefix endpoints (the prefix encodes the exam vertical:
//	gaozhi, kuaiji, etc.) — login session + device fingerprint required.
//
// Status: URL parsing implemented; full lecture chain requires session-bound
// device fingerprint and is left as a follow-up.
package fenbi

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlMyLectures     = "https://ke.fenbi.com/win/{prefix}/v3/my/lectures/visible?start={start}&len={length}"
	urlLectureDetail  = "https://ke.fenbi.com/win/{prefix}/v3/lectures/{lecture_id}"
	urlLectureAPI     = "https://ke.fenbi.com/api/{prefix}/v3/lectures/{lecture_id}"
	urlLectureSummary = "https://ke.fenbi.com/win/{prefix}/v3/my/lectures/{lecture_id}/summary"
	urlLogin          = "https://login.fenbi.com/api/users/current"
	urlUsersCurrent   = "https://ke.fenbi.com/win/v3/users/current?nickname=true"
)

var patterns = []string{`(?:[\w-]+\.)?fenbi\.com/`}

func init() {
	extractor.Register(&Fenbi{}, extractor.SiteInfo{Name: "Fenbi", URL: "fenbi.com", NeedAuth: true})
}

type Fenbi struct{}

func (f *Fenbi) Patterns() []string { return patterns }

var lectureRe = regexp.MustCompile(`/(?:lecture|lectures)/(\d+)`)

func (f *Fenbi) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("fenbi requires login cookies (use --cookies or --cookies-from-browser)")
	}
	m := lectureRe.FindStringSubmatch(rawURL)
	if m == nil {
		return nil, fmt.Errorf("cannot parse fenbi lecture id from URL: %s", rawURL)
	}
	return nil, fmt.Errorf(
		"fenbi lecture %s: full chain (%s → %s → episode mp4) requires session-bound device fingerprint; not yet implemented",
		m[1], urlLectureDetail, urlLectureSummary)
}
