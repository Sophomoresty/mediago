// Package speiyou implements an extractor for speiyou.com (学而思培优 / S-培优).
//
// API endpoints from decompiled Mooc/Courses/Speiyou/:
//   https://course-api-online.speiyou.com/course/v1/student/course/subject-list?stuId={}
//   https://course-api-online.speiyou.com/course/v1/student/course/list?...
//   https://course-api-online.speiyou.com/course/v1/student/course/user-live-list?...
//   https://course-api-online.speiyou.com/course/v1/student/live/list?...
//   https://classroom-api-online.speiyou.com/classroom/basic/v2/init/auth?resVer=1.1&classroomMode=playback
package speiyou

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlSubjectList     = "https://course-api-online.speiyou.com/course/v1/student/course/subject-list?stuId={stu_id}"
	urlCourseList      = "https://course-api-online.speiyou.com/course/v1/student/course/list?businessBelong=1,3,5,10&courseStatus=0&stdSubject={}&page={}&perPage=20&order=asc&stuId={}"
	urlUserLiveList    = "https://course-api-online.speiyou.com/course/v1/student/course/user-live-list?stdCourseId={}&type=1&needPage=1&page={}&perPage=50&order=asc&stuId={}"
	urlLiveList        = "https://course-api-online.speiyou.com/course/v1/student/live/list?businessBelong=1,3,5,10&stuId={stu_id}&liveStatus={status}&nowTime={now_time}&stdSubject={subject}&order={order}&needCourseInfo=1&needPage=1&page={page}&perPage={per_page}"
	urlClassroomAuth   = "https://classroom-api-online.speiyou.com/classroom/basic/v2/init/auth?resVer=1.1&classroomMode=playback"
)

var patterns = []string{`(?:[\w-]+\.)?speiyou\.com/`}

func init() {
	extractor.Register(&Speiyou{}, extractor.SiteInfo{Name: "Speiyou", URL: "speiyou.com", NeedAuth: true})
}

type Speiyou struct{}

func (s *Speiyou) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`(?:stuId|courseId|stdCourseId|liveId)=(\d+)`)

func (s *Speiyou) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("speiyou requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse speiyou stuId/courseId from URL")
	}
	return nil, fmt.Errorf("speiyou classroom auth+playback chain not yet implemented")
}
