package icourses

import (
	"fmt"
	"path"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

func (x *icoursesCtx) mediaFromResources(resources []resource) (*extractor.MediaInfo, error) {
	entries := make([]*extractor.MediaInfo, 0, len(resources))
	for i, r := range resources {
		entries = append(entries, x.resourceEntry(r, formatResourceName(r.Name, "", i+1, r.Kind == "video")))
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("icourses: no downloadable resources")
	}
	if len(entries) == 1 {
		entries[0].Title = firstNonEmpty(entries[0].Title, x.title)
		return entries[0], nil
	}
	return &extractor.MediaInfo{Site: "icourses", Title: firstNonEmpty(x.title, "icourses_"+x.cid), Entries: entries, Extra: map[string]any{"course_id": x.cid, "kind": string(x.kind)}}, nil
}

func (x *icoursesCtx) mediaFromMoocInfo(info moocInfo) (*extractor.MediaInfo, error) {
	var entries []*extractor.MediaInfo
	for ci, ch := range info.Chapters {
		var chapterEntries []*extractor.MediaInfo
		for ri, r := range ch.Resources {
			chapterEntries = append(chapterEntries, x.resourceEntry(r, formatResourceName(r.Name, fmt.Sprintf("%d.", ci+1), ri+1, r.Kind == "video")))
		}
		for ui, u := range ch.Units {
			unitEntries := make([]*extractor.MediaInfo, 0, len(u.Resources))
			for ri, r := range u.Resources {
				unitEntries = append(unitEntries, x.resourceEntry(r, formatResourceName(r.Name, fmt.Sprintf("%d.%d.", ci+1, ui+1), ri+1, r.Kind == "video")))
			}
			if len(unitEntries) > 0 {
				chapterEntries = append(chapterEntries, &extractor.MediaInfo{Site: "icourses", Title: fmt.Sprintf("%d.%d--%s", ci+1, ui+1, cleanName(u.Name)), Entries: unitEntries, Extra: map[string]any{"unit_id": u.ID}})
			}
		}
		if len(chapterEntries) > 0 {
			entries = append(entries, &extractor.MediaInfo{Site: "icourses", Title: fmt.Sprintf("%d--%s", ci+1, cleanName(ch.Name)), Entries: chapterEntries, Extra: map[string]any{"chapter_id": ch.ID}})
		}
	}
	if len(info.Papers) > 0 {
		entries = append(entries, x.resourceGroup("试卷", info.Papers))
	}
	if len(info.Sources) > 0 {
		entries = append(entries, x.resourceGroup("资源", info.Sources))
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("icourses: no downloadable mooc resources")
	}
	return &extractor.MediaInfo{Site: "icourses", Title: firstNonEmpty(x.title, "icourses_"+x.cid), Entries: entries, Extra: map[string]any{"course_id": x.cid, "kind": string(x.kind)}}, nil
}

func (x *icoursesCtx) resourceGroup(title string, resources []resource) *extractor.MediaInfo {
	entries := make([]*extractor.MediaInfo, 0, len(resources))
	for i, r := range resources {
		entries = append(entries, x.resourceEntry(r, formatResourceName(r.Name, "", i+1, r.Kind == "video")))
	}
	return &extractor.MediaInfo{Site: "icourses", Title: title, Entries: entries}
}

func (x *icoursesCtx) resourceEntry(r resource, title string) *extractor.MediaInfo {
	format := strings.TrimPrefix(strings.ToLower(r.Ext), ".")
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(path.Ext(strings.Split(r.URL, "?")[0])), ".")
	}
	if format == "" {
		format = "bin"
	}
	key := format
	quality := "file"
	if r.Kind == "video" {
		quality = "best"
	}
	if r.Kind != "video" {
		key = r.Kind
		if key == "" || key == "attach" {
			key = "file"
		}
	}
	return &extractor.MediaInfo{
		Site:  "icourses",
		Title: firstNonEmpty(title, r.Name),
		Streams: map[string]extractor.Stream{
			key: {Quality: quality, URLs: []string{r.URL}, Format: format, Size: r.Size, NeedMerge: strings.Contains(strings.ToLower(r.URL), ".m3u8"), Headers: x.streamHeaders()},
		},
		Extra: map[string]any{"kind": r.Kind, "resource_id": r.ResID, "media_type": r.MediaType, "category": r.Category},
	}
}

func (x *icoursesCtx) streamHeaders() map[string]string {
	h := map[string]string{"Referer": referer}
	if x.cookie != "" {
		h["Cookie"] = x.cookie
	}
	return h
}

func formatResourceName(name, prefix string, index int, isVideo bool) string {
	base := stripExt(name)
	if isVideo {
		return cleanName(fmt.Sprintf("[%s%d]--%s", prefix, index, base))
	}
	return cleanName(fmt.Sprintf("(%s%d)--%s", prefix, index, base))
}
