package icourse163

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type kaoyanURLInfo struct {
	cid    string
	termID string
	liveID string
}

var (
	kaoyanLearnRe = regexp.MustCompile(`^https?://www\.icourse163\.org/learn/kaopei-(?P<cid>\d+)(?:.*?tid=(?P<tid>\d+))?`)
	kaoyanTermRe  = regexp.MustCompile(`^https?://kaoyan\.icourse163\.org/course/terms/(?P<tid>\d+)(?:\.htm)?(?:.*?course[Ii]d=(?P<cid>\d+))?`)
	kaoyanLiveRe  = regexp.MustCompile(`^https?://www\.icourse163\.org/live/.*?(?P<live>\d+)\.htm`)
)

func parseKaoyanURL(rawURL string) (kaoyanURLInfo, bool) {
	if m := kaoyanLearnRe.FindStringSubmatch(rawURL); m != nil {
		return kaoyanURLInfo{
			cid:    m[kaoyanLearnRe.SubexpIndex("cid")],
			termID: m[kaoyanLearnRe.SubexpIndex("tid")],
		}, true
	}
	if m := kaoyanTermRe.FindStringSubmatch(rawURL); m != nil {
		return kaoyanURLInfo{
			cid:    m[kaoyanTermRe.SubexpIndex("cid")],
			termID: m[kaoyanTermRe.SubexpIndex("tid")],
		}, true
	}
	if m := kaoyanLiveRe.FindStringSubmatch(rawURL); m != nil {
		return kaoyanURLInfo{liveID: m[kaoyanLiveRe.SubexpIndex("live")]}, true
	}
	return kaoyanURLInfo{}, false
}

func (k kaoyanURLInfo) pageURL() string {
	switch {
	case k.termID != "" && k.cid != "":
		return kaoyanTermURL + k.termID + ".htm?courseId=" + url.QueryEscape(k.cid)
	case k.liveID != "":
		return kaoyanLiveURL + k.liveID + ".htm"
	case k.cid != "":
		return kaoyanCourseURL + k.cid
	default:
		return ""
	}
}

func extractKaoyan(c *util.Client, ky kaoyanURLInfo) (*extractor.MediaInfo, error) {
	pageURL := ky.pageURL()
	if pageURL == "" {
		return nil, fmt.Errorf("cannot parse icourse163 kaoyan URL")
	}
	page, err := c.GetString(pageURL, headers())
	if err != nil {
		return nil, fmt.Errorf("fetch kaoyan page: %w", err)
	}
	if ky.termID == "" {
		ky.termID = firstNonEmpty(
			match1(page, `termId\s*:\s*(\d+)`),
			match1(page, `currentTermId\s*:\s*"?(\d+)"?`),
		)
	}
	if ky.cid == "" {
		ky.cid = firstNonEmpty(
			match1(page, `course[Ii]d\s*[:=]\s*"?(\d+)"?`),
			match1(page, `/learn/kaopei-(\d+)`),
		)
	}
	title := titleFromPage(page, "icourse163_kaoyan_"+firstNonEmpty(ky.cid, ky.termID, ky.liveID))
	memberID, err := fetchMemberID(c, page)
	if err != nil {
		return nil, err
	}

	if ky.liveID != "" {
		if ky.termID == "" {
			return nil, fmt.Errorf("cannot find kaoyan live termId for live %s", ky.liveID)
		}
		ps, err := fetchVideoStream(c, videoUnit{
			contentID:   ky.termID,
			contentType: "7",
			unitID:      ky.liveID,
			name:        title,
		}, memberID, true)
		if err != nil {
			return nil, fmt.Errorf("resolve kaoyan live video: %w", err)
		}
		mi := mediaEntry(title, ps)
		mi.Title = title
		return mi, nil
	}

	if ky.termID == "" {
		return nil, fmt.Errorf("cannot find kaoyan termId for course %s", ky.cid)
	}
	purchased, payErr := fetchKaoyanPurchased(c, ky.termID)
	chapters, jsonErr := fetchMocTermJSONChapters(c, ky.termID)
	if len(chapters) == 0 && !purchased {
		if fallback, err := fetchChapters(c, ky.termID); err == nil && len(fallback) > 0 {
			chapters = fallback
		}
	}
	if len(chapters) == 0 {
		if jsonErr != nil {
			return nil, fmt.Errorf("kaoyan getLastLearnedMocTermDto: %w", jsonErr)
		}
		if payErr != nil {
			return nil, fmt.Errorf("kaoyan pay status: %w", payErr)
		}
		return nil, fmt.Errorf("no chapters in kaoyan course %s/%s (purchase required?)", ky.cid, ky.termID)
	}
	entries, err := entriesFromChapters(c, chapters, memberID)
	if err != nil {
		return nil, err
	}
	return &extractor.MediaInfo{
		Site:    "icourse163",
		Title:   title,
		Entries: entries,
		Extra: map[string]any{
			"course_id":   ky.cid,
			"term_id":     ky.termID,
			"purchased":   purchased,
			"source_api":  "kaoyan.icourse163.org",
			"source_path": "courseBean.getLastLearnedMocTermDto",
		},
	}, nil
}

func fetchKaoyanPurchased(c *util.Client, termID string) (bool, error) {
	body, err := c.PostForm(fmt.Sprintf(kaoyanPayURL, srckey), map[string]string{
		"termId": termID,
	}, headers())
	if err != nil {
		return false, err
	}
	var out struct {
		Result struct {
			EnrollStatus any `json:"enrollStatus"`
		} `json:"result"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return false, err
	}
	return strings.TrimSpace(valueString(out.Result.EnrollStatus)) == "0", nil
}

func fetchMocTermJSONChapters(c *util.Client, termID string) ([]chapter, error) {
	body, err := c.PostForm(kaoyanNewInfosURL+srckey, map[string]string{
		"termId": termID,
	}, headers())
	if err != nil {
		return nil, err
	}
	return parseMocTermJSONChapters(body)
}

func parseMocTermJSONChapters(body string) ([]chapter, error) {
	var out struct {
		Result struct {
			MocTermDto struct {
				Chapters []struct {
					ID      any    `json:"id"`
					Name    string `json:"name"`
					Lessons []struct {
						ID    any    `json:"id"`
						Name  string `json:"name"`
						Units []struct {
							ContentID   any    `json:"contentId"`
							ContentType any    `json:"contentType"`
							ID          any    `json:"id"`
							Name        string `json:"name"`
						} `json:"units"`
					} `json:"lessons"`
				} `json:"chapters"`
			} `json:"mocTermDto"`
		} `json:"result"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return nil, err
	}
	chapters := make([]chapter, 0, len(out.Result.MocTermDto.Chapters))
	for _, rawCh := range out.Result.MocTermDto.Chapters {
		ch := chapter{id: valueString(rawCh.ID), name: rawCh.Name}
		for _, rawLesson := range rawCh.Lessons {
			ls := lesson{id: valueString(rawLesson.ID), name: rawLesson.Name}
			for _, rawUnit := range rawLesson.Units {
				contentType := valueString(rawUnit.ContentType)
				if contentType != "1" && contentType != "7" {
					continue
				}
				vu := videoUnit{
					contentID:   valueString(rawUnit.ContentID),
					contentType: contentType,
					unitID:      valueString(rawUnit.ID),
					name:        rawUnit.Name,
					lessonID:    ls.id,
				}
				if vu.contentID == "" || vu.unitID == "" {
					continue
				}
				ls.videos = append(ls.videos, vu)
			}
			ch.lessons = append(ch.lessons, ls)
		}
		chapters = append(chapters, ch)
	}
	return chapters, nil
}
