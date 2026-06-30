package gaotu

import (
	"fmt"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type gaotuCourse struct {
	ID        string
	Title     string
	URL       string
	Price     float64
	Purchased bool
}

func fetchGaotuCourseList(c *util.Client, headers map[string]string, endpoints gaotuEndpoints) ([]gaotuCourse, error) {
	seen := map[string]bool{}
	var out []gaotuCourse
	var lastErr error
	for page := 1; page < 10; page++ {
		payload, err := postJSON(c, endpoints.courseURL(), gaotuCourseListRequestPayload(page), headers)
		if err != nil {
			lastErr = err
			continue
		}
		courses := collectGaotuCourses(payload)
		for _, course := range courses {
			if course.ID == "" || seen[course.ID] {
				continue
			}
			seen[course.ID] = true
			out = append(out, course)
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, fmt.Errorf("fetch gaotu course list: %w", lastErr)
	}
	return out, nil
}

func collectGaotuCourses(v any) []gaotuCourse {
	seen := map[string]bool{}
	var out []gaotuCourse
	var walk func(any, bool)
	walk = func(x any, expired bool) {
		switch vv := x.(type) {
		case map[string]any:
			if title := strings.TrimSpace(valueString(vv, "moduleTitle")); title == "已过期" {
				expired = true
			}
			if course, ok := parseGaotuCourse(vv, expired); ok {
				if !seen[course.ID] {
					seen[course.ID] = true
					out = append(out, course)
				}
			}
			for _, key := range []string{"moduleList", "moduleCardList", "courseList", "items", "list", "records", "rows", "data", "result", "children"} {
				if child, ok := vv[key]; ok {
					walk(child, expired)
				}
			}
		case []any:
			for _, child := range vv {
				walk(child, expired)
			}
		}
	}
	walk(v, false)
	return out
}

func parseGaotuCourse(m map[string]any, expired bool) (gaotuCourse, bool) {
	if expired {
		return gaotuCourse{}, false
	}
	id := valueString(m, "clazzNumber", "course_id", "courseId", "courseID", "productSpuNumber")
	if id == "" {
		return gaotuCourse{}, false
	}
	title := firstNonEmpty(valueString(m, "cardTitle", "clazzName", "courseName", "title", "name"), id)
	course := gaotuCourse{
		ID:        id,
		Title:     title,
		URL:       normalizeURL(valueString(m, "url", "courseUrl", "courseURL", "shareUrl", "link")),
		Purchased: true,
	}
	if !isHTTPURL(course.URL) {
		course.URL = ""
	}
	if price, ok := gaotuPriceFromPayload(m); ok {
		course.Price = price
	}
	return course, true
}

func gaotuCourseListMedia(endpoints gaotuEndpoints, courses []gaotuCourse) *extractor.MediaInfo {
	entries := make([]*extractor.MediaInfo, 0, len(courses))
	for _, course := range courses {
		url := firstNonEmpty(course.URL, gaotuCourseURL(endpoints, course.ID))
		extra := map[string]any{
			"url":              url,
			"clazz_number":     course.ID,
			"p_client":         endpoints.pClient,
			"api_host":         endpoints.apiHost,
			"interactive_host": endpoints.interactiveHost,
			"purchased":        course.Purchased,
		}
		if course.Price > 0 {
			extra["price"] = course.Price
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:  "gaotu",
			Title: util.SanitizeFilename(firstNonEmpty(course.Title, course.ID)),
			Extra: compactExtra(extra),
		})
	}
	return &extractor.MediaInfo{
		Site:    "gaotu",
		Title:   util.SanitizeFilename(gaotuBrandKey(endpoints) + "_courses"),
		Entries: entries,
	}
}

func gaotuCourseURL(endpoints gaotuEndpoints, clazz string) string {
	if clazz == "" {
		return strings.TrimRight(endpoints.referer, "/")
	}
	return strings.TrimRight(endpoints.referer, "/") + "/course?clazzNumber=" + q(clazz)
}

func gaotuBrandKey(endpoints gaotuEndpoints) string {
	switch strings.ToLower(endpoints.apiHost) {
	case "api.gaotu100.com":
		return "gaotu100"
	case "api.gtgz.cn":
		return "gtgz"
	case "api.naiyouxuexi.com":
		return "naiyouxuexi"
	default:
		return "gaotu"
	}
}
