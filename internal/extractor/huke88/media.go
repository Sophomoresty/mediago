package huke88

import (
	"fmt"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func (x *huke88Ctx) mediaFromSources(srcs []hukeSource) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for _, src := range srcs {
		entry := x.mediaEntry(src)
		if entry != nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("huke88: no downloadable entries")
	}
	if len(entries) == 1 {
		entries[0].Title = firstNonEmpty(entries[0].Title, x.title)
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "huke88", Title: firstNonEmpty(x.title, "huke88_"+x.cid), Entries: entries, Extra: map[string]any{"course_id": x.cid, "paid_course_id": x.paidCourseID}}, nil
}

func (x *huke88Ctx) mediaEntry(src hukeSource) *extractor.MediaInfo {
	if src.URL == "" {
		return nil
	}
	format := firstNonEmpty(src.Format, mediaFormat(src.URL, "mp4"))
	key := format
	quality := "best"
	if src.Kind == "file" {
		key = "file"
		quality = "file"
	}
	return &extractor.MediaInfo{
		Site:  "huke88",
		Title: firstNonEmpty(src.Name, src.ID),
		Streams: map[string]extractor.Stream{
			key: {Quality: quality, URLs: []string{src.URL}, Format: format, NeedMerge: strings.Contains(strings.ToLower(src.URL), ".m3u8"), Headers: x.streamHeaders(src.ID)},
		},
		Extra: map[string]any{"kind": src.Kind, "video_id": src.ID, "file_type": src.FileType},
	}
}

func (x *huke88Ctx) streamHeaders(courseID string) map[string]string {
	return map[string]string{"Referer": fmtCourseURL(firstNonEmpty(courseID, x.cid)), "Cookie": x.cookie}
}
