package icourse163

// App "我的课程" flow ported from decompiled
// Mooc/Courses/Mooc163/Icourse163/Icourse163_App.pyc.
//
// Lists the authenticated user's enrolled courses across MOOC (courseType 1+2)
// and Column types via:
//   1. learnerCourseRpcBean.getMyLearnedCoursePanelList.rpc  -> MOOC courses
//   2. columnBean.getColumnInfoListForMember.rpc             -> Column courses
//
// Each returned course entry carries enough metadata (school, course_id,
// term_id, spoc flag) to construct the canonical course/column URL that the
// existing MOOC / Column / Kaoyan extractors can resolve.

import (
	"fmt"
	"strconv"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

// appCourse mirrors the dict produced by Icourse163_App._get_mooc_course_list
// and _get_column_course_list in the Python source.
type appCourse struct {
	courseID string
	termID  string
	title   string
	school  string
	spoc    bool
}

// extractAppCourseList implements the Icourse163_App flow: fetch the user's
// enrolled course list and return each as a sub-entry with the resolved
// course URL, delegating to the appropriate extractor.
func extractAppCourseList(c *util.Client) (*extractor.MediaInfo, error) {
	var courses []appCourse
	// MOOC type=1 (regular) and type=2 (SPOC) per source
	for _, ct := range []string{"1", "2"} {
		mc, err := fetchMoocCourseList(c, ct)
		if err != nil {
			return nil, fmt.Errorf("getMyLearnedCoursePanelList (type=%s): %w", ct, err)
		}
		courses = append(courses, mc...)
	}
	// Column courses
	cc, err := fetchColumnCourseList(c)
	if err != nil {
		return nil, fmt.Errorf("getColumnInfoListForMember: %w", err)
	}
	courses = append(courses, cc...)

	if len(courses) == 0 {
		return nil, fmt.Errorf("no enrolled courses found (login cookies may be invalid)")
	}

	entries := make([]*extractor.MediaInfo, 0, len(courses))
	for _, ac := range courses {
		courseURL := appCourseURL(ac)
		entries = append(entries, &extractor.MediaInfo{
			Site:  "icourse163",
			Title: sanitize(ac.title),
			Extra: map[string]any{
				"course_id":  ac.courseID,
				"term_id":    ac.termID,
				"school":     ac.school,
				"spoc":       ac.spoc,
				"course_url": courseURL,
				"source_api": "learnerCourseRpcBean/columnBean",
			},
		})
	}

	return &extractor.MediaInfo{
		Site:    "icourse163",
		Title:   "我的课程",
		Entries: entries,
		Extra: map[string]any{
			"source_api":  "Icourse163_App",
			"total_count": len(courses),
		},
	}, nil
}

// appCourseURL constructs the canonical URL for a course exactly as
// Icourse163_App._select_my_course does in the Python source.
func appCourseURL(ac appCourse) string {
	switch ac.school {
	case "kaopei":
		// kaoyan course: https://www.icourse163.org/learn/kaopei-{cid}?tid={term_id}
		return fmt.Sprintf("https://www.icourse163.org/learn/kaopei-%s?tid=%s", ac.courseID, ac.termID)
	case "column":
		// column: https://www.icourse163.org/column/learn/{cid}.htm
		return fmt.Sprintf("https://www.icourse163.org/column/learn/%s.htm", ac.courseID)
	default:
		// regular/spoc MOOC
		prefix := ""
		if ac.spoc {
			prefix = "spoc/"
		}
		cid := ac.courseID
		if ac.school != "" {
			cid = ac.school + "-" + ac.courseID
		}
		return fmt.Sprintf("https://www.icourse163.org/%slearn/%s?tid=%s", prefix, cid, ac.termID)
	}
}

// fetchMoocCourseList calls learnerCourseRpcBean.getMyLearnedCoursePanelList.rpc
// with the given courseType ("1" = regular, "2" = SPOC) and paginates up to
// 10 pages (matching source range(1, 10)).
//
// Source: Icourse163_App._get_mooc_course_list
func fetchMoocCourseList(c *util.Client, courseType string) ([]appCourse, error) {
	var all []appCourse
	for page := 1; page < 10; page++ {
		body, err := c.PostForm(appMoocCourseListURL+srckey, map[string]string{
			"courseType": courseType,
			"psize":     "999",
			"p":         strconv.Itoa(page),
			"type":      "30",
		}, headers())
		if err != nil {
			return nil, err
		}

		var out struct {
			Result struct {
				Result []struct {
					ID        any    `json:"id"`
					Name      string `json:"name"`
					TermPanel struct {
						ID any `json:"id"`
					} `json:"termPanel"`
					SchoolPanel struct {
						ShortName string `json:"shortName"`
						Name      string `json:"name"`
					} `json:"schoolPanel"`
				} `json:"result"`
			} `json:"result"`
		}
		if err := decodeJSON(body, &out); err != nil {
			break
		}
		if len(out.Result.Result) == 0 {
			break
		}
		for _, r := range out.Result.Result {
			cid := valueString(r.ID)
			termID := valueString(r.TermPanel.ID)
			title := r.Name
			school := r.SchoolPanel.ShortName
			isSpoc := courseType == "2"
			// Source: if school != 'kaopei': title = '{}_{}' % (title, schoolPanel.name)
			if school != "kaopei" && r.SchoolPanel.Name != "" {
				title = title + "_" + r.SchoolPanel.Name
			}
			all = append(all, appCourse{
				courseID: cid,
				termID:  termID,
				title:   title,
				school:  school,
				spoc:    isSpoc,
			})
		}
	}
	return all, nil
}

// fetchColumnCourseList calls columnBean.getColumnInfoListForMember.rpc
// and paginates up to 10 pages (matching source range(1, 10)).
//
// Source: Icourse163_App._get_column_course_list
func fetchColumnCourseList(c *util.Client) ([]appCourse, error) {
	var all []appCourse
	for page := 1; page < 10; page++ {
		body, err := c.PostForm(appColumnCourseListURL+srckey, map[string]string{
			"pageSize":  "999",
			"pageIndex": strconv.Itoa(page),
		}, headers())
		if err != nil {
			return nil, err
		}

		var out struct {
			Result struct {
				List []struct {
					ColumnID   any    `json:"columnId"`
					ColumnName string `json:"columnName"`
				} `json:"list"`
			} `json:"result"`
		}
		if err := decodeJSON(body, &out); err != nil {
			break
		}
		if len(out.Result.List) == 0 {
			break
		}
		for _, r := range out.Result.List {
			all = append(all, appCourse{
				courseID: valueString(r.ColumnID),
				title:   r.ColumnName,
				school:  "column",
				spoc:    false,
			})
		}
	}
	return all, nil
}
