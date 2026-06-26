// Package feishu implements an extractor for feishu.cn (Lark) minutes,
// drive-file previews, docx/docs documents, and wiki document links.
//
// The branches mirror the decompiled Python source:
//   - /minutes/{fid}: read window data and decode video_url
//   - /file/{fid}: build the internal preview download URL
//   - /docx|docs/{did}: fetch the document page and expose the document page
//     plus embedded/attached Feishu file tokens or direct media URLs
//   - /wiki/{wid}: resolve wiki token metadata, then delegate to doc/file flows
package feishu

import (
	"fmt"
	neturl "net/url"
	"regexp"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const feishuReferer = "https://www.feishu.cn"

var patterns = []string{
	`(?:[\w-]+\.)*feishu\.cn/(?:minutes|file|docx|docs|wiki)/`,
}

func init() {
	extractor.Register(&Feishu{}, extractor.SiteInfo{
		Name:     "Feishu",
		URL:      "feishu.cn",
		NeedAuth: true,
	})
}

type Feishu struct{}

func (f *Feishu) Patterns() []string { return patterns }

var (
	minutesRe   = regexp.MustCompile(`(?i)feishu\.cn/minutes/([A-Za-z0-9]+)`)
	fileRe      = regexp.MustCompile(`(?i)feishu\.cn/file/([A-Za-z0-9]+)`)
	docxRe      = regexp.MustCompile(`(?i)feishu\.cn/(?:docx|docs)/([A-Za-z0-9]+)`)
	wikiRe      = regexp.MustCompile(`(?i)feishu\.cn/wiki/([A-Za-z0-9]+)`)
	videoURLRe  = regexp.MustCompile(`(?is)\\u0022video_url\\u0022\s*:\s*\\u0022(http.*?)\\u0022`)
	titleAnyRe  = regexp.MustCompile(`(?is)"title"\s*:\s*"([^"]+?)"`)
	htmlTitleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

	windowDataRe     = regexp.MustCompile(`(?is)window\.DATA\s*=\s*Object([\s\S]+)`)
	directMediaURLRe = regexp.MustCompile(`(?i)https?://[^\s"'<>\\]+?\.(?:m3u8|mp4|m4v|mov|flv|webm|avi|mp3|m4a|aac|wav|flac|ogg)(?:[^\s"'<>\\]*)?`)
	previewURLRe     = regexp.MustCompile(`(?i)https?://internal-api-drive-stream\.feishu\.cn/space/api/box/stream/download/preview/([A-Za-z0-9]+)[^"'<>\\\s]*`)
	jsonTokenRe      = regexp.MustCompile(`(?is)"(?:file_token|fileToken|file_id|fileId|token|id)"\s*:\s*"([A-Za-z0-9]{8,})"`)
	attrTokenRe      = regexp.MustCompile(`(?is)(?:data-file-token|data-token|data-file-id|data-media-token)\s*=\s*["']([A-Za-z0-9]{8,})["']`)
)

var knownFeishuFormats = map[string]bool{
	"mp4": true, "m3u8": true, "m4v": true, "mov": true, "flv": true, "webm": true, "avi": true,
	"mp3": true, "m4a": true, "aac": true, "wav": true, "flac": true, "ogg": true,
	"pdf": true, "doc": true, "docx": true, "ppt": true, "pptx": true, "xls": true, "xlsx": true,
	"zip": true, "rar": true, "7z": true, "html": true,
}

func (f *Feishu) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("feishu requires login cookies (use --cookies or --cookies-from-browser)")
	}

	c := util.NewClient()
	c.SetCookieJar(opts.Cookies)
	h := feishuHeaders(rawURL, opts.Cookies)

	switch {
	case minutesRe.MatchString(rawURL):
		return extractMinutes(c, rawURL, h)
	case fileRe.MatchString(rawURL):
		return extractFile(c, rawURL, h)
	case docxRe.MatchString(rawURL):
		id := docxRe.FindStringSubmatch(rawURL)[1]
		return extractDocx(c, rawURL, id, h, "")
	case wikiRe.MatchString(rawURL):
		return extractWiki(c, rawURL, h)
	}
	return nil, fmt.Errorf("unsupported feishu URL shape: %s", rawURL)
}

func extractMinutes(c *util.Client, rawURL string, h map[string]string) (*extractor.MediaInfo, error) {
	body, err := c.GetString(rawURL, h)
	if err != nil {
		return nil, fmt.Errorf("fetch minutes page: %w", err)
	}

	m := videoURLRe.FindStringSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("video_url not present in minutes HTML — the recording may be private or login may have lapsed")
	}
	videoURL, err := unicodeUnescape(m[1])
	if err != nil {
		return nil, fmt.Errorf("unicode unescape video_url: %w", err)
	}
	videoURL = normalizeFeishuURL(videoURL)

	id := extractFirst(minutesRe, rawURL)
	title := firstNonEmpty(extractFeishuDocTitle(body, id), "feishu_minutes_"+id, "feishu_minutes")
	format := feishuFormatFromMime("", "video", videoURL)

	return &extractor.MediaInfo{
		Site:  "feishu",
		Title: util.SanitizeFilename(title),
		Streams: map[string]extractor.Stream{
			"default": {
				Quality: "best",
				URLs:    []string{videoURL},
				Format:  format,
				Headers: cloneHeaders(h),
			},
		},
		Extra: map[string]any{"kind": "minutes", "file_id": id},
	}, nil
}

func extractFile(c *util.Client, rawURL string, h map[string]string) (*extractor.MediaInfo, error) {
	fid := extractFirst(fileRe, rawURL)
	if fid == "" {
		return nil, fmt.Errorf("cannot parse feishu file id from URL: %s", rawURL)
	}
	body, err := c.GetString(rawURL, h)
	if err != nil {
		return nil, fmt.Errorf("fetch file page: %w", err)
	}
	title := firstNonEmpty(extractFeishuDocTitle(body, fid), "feishu_file_"+fid)
	name, format := feishuSplitFilename(title, "mp4")
	return feishuSinglePreviewMedia(name, fid, format, h, map[string]any{"kind": "file", "file_id": fid}), nil
}

func extractDocx(c *util.Client, rawURL, docID string, h map[string]string, titleOverride string) (*extractor.MediaInfo, error) {
	body, err := c.GetString(rawURL, h)
	if err != nil {
		return nil, fmt.Errorf("fetch docx page: %w", err)
	}
	title := firstNonEmpty(titleOverride, extractFeishuDocTitle(body, docID), "feishu_docx_"+docID)
	entries := []*extractor.MediaInfo{feishuDocumentEntry(title, rawURL, h, map[string]any{"kind": "document", "doc_id": docID})}

	items := extractFeishuDocItems(body)
	entries = append(entries, feishuEntriesFromItems(items, h, docID)...)

	return &extractor.MediaInfo{
		Site:    "feishu",
		Title:   util.SanitizeFilename(title),
		Entries: entries,
		Extra:   map[string]any{"kind": "docx", "doc_id": docID, "assets": len(entries) - 1},
	}, nil
}

func extractWiki(c *util.Client, rawURL string, h map[string]string) (*extractor.MediaInfo, error) {
	wikiID := extractFirst(wikiRe, rawURL)
	if wikiID == "" {
		return nil, fmt.Errorf("cannot parse feishu wiki token from URL: %s", rawURL)
	}
	origin := feishuOrigin(rawURL)
	infos := feishuWikiTokenInfos(c, origin, wikiID, h)

	var entries []*extractor.MediaInfo
	title := "feishu_wiki_" + wikiID
	seen := map[string]bool{}
	for _, info := range infos {
		if info.Title != "" && title == "feishu_wiki_"+wikiID {
			title = info.Title
		}
		key := info.ObjType + ":" + firstNonEmpty(info.ObjToken, info.Token)
		if key == ":" || seen[key] {
			continue
		}
		seen[key] = true
		switch {
		case info.isDoc():
			docToken := firstNonEmpty(info.ObjToken, info.Token)
			docURL := fmt.Sprintf("%s/%s/%s", origin, info.docPath(), neturl.PathEscape(docToken))
			doc, err := extractDocx(c, docURL, docToken, h, info.Title)
			if err == nil {
				entries = append(entries, prefixEntries(doc.Title, flattenEntries(doc))...)
			}
		case info.isFile():
			fileToken := firstNonEmpty(info.ObjToken, info.Token)
			name := firstNonEmpty(info.Title, "feishu_file_"+fileToken)
			base, format := feishuSplitFilename(name, "mp4")
			entries = append(entries, feishuSinglePreviewMedia(base, fileToken, format, h, map[string]any{"kind": "wiki_file", "wiki_token": wikiID, "file_id": fileToken}))
		default:
			// Some wiki APIs omit obj_type but still expose a backing doc token.
			docToken := firstNonEmpty(info.ObjToken, "")
			if docToken == "" || docToken == wikiID {
				continue
			}
			docURL := fmt.Sprintf("%s/docx/%s", origin, neturl.PathEscape(docToken))
			doc, err := extractDocx(c, docURL, docToken, h, info.Title)
			if err == nil {
				entries = append(entries, prefixEntries(doc.Title, flattenEntries(doc))...)
			}
		}
	}

	if len(entries) == 0 {
		body, err := c.GetString(rawURL, h)
		if err != nil {
			return nil, fmt.Errorf("resolve wiki token %s and fetch wiki page: %w", wikiID, err)
		}
		title = firstNonEmpty(extractFeishuDocTitle(body, wikiID), title)
		entries = append(entries, feishuDocumentEntry(title, rawURL, h, map[string]any{"kind": "wiki_page", "wiki_token": wikiID}))
		entries = append(entries, feishuEntriesFromItems(extractFeishuDocItems(body), h, wikiID)...)
	}

	return &extractor.MediaInfo{
		Site:    "feishu",
		Title:   util.SanitizeFilename(title),
		Entries: entries,
		Extra:   map[string]any{"kind": "wiki", "wiki_token": wikiID, "assets": len(entries)},
	}, nil
}
