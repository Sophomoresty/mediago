package icourse163

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type columnURLInfo struct {
	cid string
}

type columnLesson struct {
	id   string
	name string
}

type columnUnit struct {
	lessonUnitID string
	contentType  string
	name         string
}

var columnURLRe = regexp.MustCompile(
	`^https?://www\.icourse163\.org/(?:columns/(?P<cid1>\d+)\.htm|column/learn/(?P<cid2>\d+)(?:/.*?\.htm)?)`,
)

func parseColumnURL(rawURL string) (columnURLInfo, bool) {
	m := columnURLRe.FindStringSubmatch(rawURL)
	if m == nil {
		return columnURLInfo{}, false
	}
	cid := firstNonEmpty(m[columnURLRe.SubexpIndex("cid1")], m[columnURLRe.SubexpIndex("cid2")])
	return columnURLInfo{cid: cid}, cid != ""
}

func extractColumn(c *util.Client, column columnURLInfo) (*extractor.MediaInfo, error) {
	pageURL := columnPageURL + column.cid + ".htm"
	page, err := c.GetString(pageURL, headers())
	if err != nil {
		return nil, fmt.Errorf("fetch column page: %w", err)
	}
	title := titleFromPage(page, "icourse163_column_"+column.cid)
	memberID, err := fetchMemberID(c, page)
	if err != nil {
		return nil, err
	}
	lessons, err := fetchColumnLessons(c, column.cid)
	if err != nil {
		return nil, err
	}

	var entries []*extractor.MediaInfo
	var firstErr error
	for li, ls := range lessons {
		units, err := fetchColumnUnits(c, ls.id)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for ui, unit := range units {
			name := fmt.Sprintf("%02d.%02d %s", li+1, ui+1, sanitize(unit.name))
			unitEntries, err := columnEntriesForUnit(c, name, unit, memberID)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			entries = append(entries, unitEntries...)
		}
	}
	if len(entries) == 0 {
		if firstErr != nil {
			return nil, fmt.Errorf("no playable column units found: %w", firstErr)
		}
		return nil, fmt.Errorf("no playable column units found")
	}
	return &extractor.MediaInfo{
		Site:    "icourse163",
		Title:   title,
		Entries: entries,
		Extra: map[string]any{
			"column_id":   column.cid,
			"source_api":  "columnBean",
			"source_path": "getMocLessonBaseDtos/getLessonUnitBaseVoByLessonId",
		},
	}, nil
}

func fetchColumnLessons(c *util.Client, columnID string) ([]columnLesson, error) {
	body, err := c.PostForm(columnTermURL+srckey, map[string]string{
		"termId":   columnID,
		"sortType": "3",
	}, headers())
	if err != nil {
		return nil, err
	}
	return parseColumnLessons(body)
}

func parseColumnLessons(body string) ([]columnLesson, error) {
	var out struct {
		Result []struct {
			ID   any    `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return nil, err
	}
	lessons := make([]columnLesson, 0, len(out.Result))
	for _, raw := range out.Result {
		id := valueString(raw.ID)
		if id == "" {
			continue
		}
		lessons = append(lessons, columnLesson{id: id, name: raw.Name})
	}
	if len(lessons) == 0 {
		return nil, fmt.Errorf("columnBean.getMocLessonBaseDtos returned no lessons")
	}
	return lessons, nil
}

func fetchColumnUnits(c *util.Client, lessonID string) ([]columnUnit, error) {
	body, err := c.PostForm(columnInfosURL+srckey, map[string]string{
		"lessonId": lessonID,
		"sortType": "7",
	}, headers())
	if err != nil {
		return nil, err
	}
	return parseColumnUnits(body)
}

func parseColumnUnits(body string) ([]columnUnit, error) {
	var out struct {
		Result []struct {
			LessonUnitID any    `json:"lessonUnitId"`
			ID           any    `json:"id"`
			ContentType  any    `json:"contentType"`
			Name         string `json:"name"`
		} `json:"result"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return nil, err
	}
	units := make([]columnUnit, 0, len(out.Result))
	for _, raw := range out.Result {
		id := firstNonEmpty(valueString(raw.LessonUnitID), valueString(raw.ID))
		contentType := valueString(raw.ContentType)
		if id == "" || contentType == "" {
			continue
		}
		units = append(units, columnUnit{
			lessonUnitID: id,
			contentType:  contentType,
			name:         raw.Name,
		})
	}
	return units, nil
}

func columnEntriesForUnit(c *util.Client, name string, unit columnUnit, memberID string) ([]*extractor.MediaInfo, error) {
	if unit.contentType == "8" {
		return fetchColumnArticleEntries(c, name, unit.lessonUnitID)
	}
	ps, err := fetchSignedVideoStream(c, unit.lessonUnitID, unit.contentType, memberID, false)
	if err != nil {
		return nil, err
	}
	return []*extractor.MediaInfo{mediaEntry(name, ps)}, nil
}

func fetchColumnArticleEntries(c *util.Client, name, articleID string) ([]*extractor.MediaInfo, error) {
	body, err := c.PostForm(columnAudioURL+srckey, map[string]string{
		"isIncludeRtfContent": "true",
		"articleId":           articleID,
	}, headers())
	if err != nil {
		return nil, err
	}
	var out struct {
		Result struct {
			AudioNosKey string `json:"audioNosKey"`
			RtfContent  string `json:"rtfContent"`
		} `json:"result"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return nil, err
	}

	var entries []*extractor.MediaInfo
	if out.Result.AudioNosKey != "" {
		audioURL := out.Result.AudioNosKey
		if !strings.HasPrefix(audioURL, "http://") && !strings.HasPrefix(audioURL, "https://") {
			audioURL = "https://edu-media.nosdn.127.net/" + strings.TrimLeft(audioURL, "/")
		}
		entries = append(entries, &extractor.MediaInfo{
			Site:  "icourse163",
			Title: name,
			Streams: map[string]extractor.Stream{
				"audio": {
					Quality: "audio",
					URLs:    []string{audioURL},
					Format:  formatFromURL(audioURL, "mp3"),
					Headers: map[string]string{"Referer": referer},
				},
			},
		})
	}
	if out.Result.RtfContent != "" {
		entries = append(entries, &extractor.MediaInfo{
			Site:  "icourse163",
			Title: name + " 富文本",
			Streams: map[string]extractor.Stream{
				"document": {
					Quality: "document",
					URLs:    []string{"data:text/html;charset=utf-8," + url.PathEscape(out.Result.RtfContent)},
					Format:  "html",
				},
			},
			Extra: map[string]any{"html_content": out.Result.RtfContent},
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("column article %s has no audioNosKey or rtfContent", articleID)
	}
	return entries, nil
}

func formatFromURL(rawURL, fallback string) string {
	u := rawURL
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndexByte(u, '.'); i >= 0 && i+1 < len(u) {
		ext := strings.ToLower(u[i+1:])
		if len(ext) <= 5 {
			return ext
		}
	}
	return fallback
}
