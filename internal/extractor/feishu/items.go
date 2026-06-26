package feishu

import (
	"fmt"
	neturl "net/url"
	"path"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

type feishuItem struct {
	Token string
	URL   string
	Name  string
	Fmt   string
	Kind  string
	Mime  string
	Size  int64
}

func extractFeishuDocItems(body string) []feishuItem {
	decoded := decodeFeishuEscapes(body)
	items := append([]feishuItem{}, extractFeishuDocAttachments(decoded)...)
	items = append(items, extractFeishuDocMediaItems(decoded)...)
	return dedupeFeishuItems(items)
}

func extractFeishuDocAttachments(decoded string) []feishuItem {
	scope := decoded
	if m := windowDataRe.FindStringSubmatch(decoded); len(m) > 1 {
		scope = m[1]
	}
	var items []feishuItem
	fallbacks := []struct {
		re         *regexp.Regexp
		tokenFirst bool
	}{
		{regexp.MustCompile(`(?is)"(?:token|file_token|fileToken)"\s*:\s*"([A-Za-z0-9]{8,})"[\s\S]{0,800}?"(?:name|file_name|fileName|title)"\s*:\s*"([^"]+?\.[^"]+?)"`), true},
		{regexp.MustCompile(`(?is)"(?:name|file_name|fileName|title)"\s*:\s*"([^"]+?\.[^"]+?)"[\s\S]{0,800}?"(?:token|file_token|fileToken)"\s*:\s*"([A-Za-z0-9]{8,})"`), false},
	}
	for _, fb := range fallbacks {
		for _, m := range fb.re.FindAllStringSubmatch(scope, -1) {
			token, name := m[1], m[2]
			if !fb.tokenFirst {
				name, token = m[1], m[2]
			}
			name = decodeTextValue(name)
			base, format := feishuSplitFilename(name, "")
			if token != "" && format != "" {
				items = append(items, feishuItem{Token: token, Name: base, Fmt: format, Kind: feishuKindFromFormat(format)})
			}
		}
	}
	for _, loc := range jsonTokenRe.FindAllStringSubmatchIndex(scope, -1) {
		frag := jsonFragmentAround(scope, loc[0], 2200, 2600)
		item := itemFromFragment(frag)
		if item.Token != "" && item.Name != "" && item.Fmt != "" {
			items = append(items, item)
		}
	}
	return items
}

func extractFeishuDocMediaItems(decoded string) []feishuItem {
	var items []feishuItem
	for _, raw := range directMediaURLRe.FindAllString(decoded, -1) {
		u := normalizeFeishuURL(raw)
		name := path.Base(urlPath(u))
		base, format := feishuSplitFilename(name, feishuFormatFromMime("", "", u))
		if format == "" {
			continue
		}
		items = append(items, feishuItem{URL: u, Name: base, Fmt: format, Kind: feishuKindFromFormat(format)})
	}
	for _, m := range previewURLRe.FindAllStringSubmatchIndex(decoded, -1) {
		if len(m) < 4 {
			continue
		}
		token := decoded[m[2]:m[3]]
		frag := jsonFragmentAround(decoded, m[0], 2200, 2600)
		item := itemFromFragment(frag)
		item.Token = firstNonEmpty(item.Token, token)
		item.URL = normalizeFeishuURL(decoded[m[0]:m[1]])
		if item.Name == "" {
			item.Name = "feishu_file_" + item.Token
		}
		if item.Fmt == "" {
			_, item.Fmt = feishuSplitFilename(item.Name, "mp4")
		}
		item.Kind = firstNonEmpty(item.Kind, feishuKindFromFormat(item.Fmt))
		items = append(items, item)
	}
	for _, loc := range jsonTokenRe.FindAllStringSubmatchIndex(decoded, -1) {
		frag := jsonFragmentAround(decoded, loc[0], 2200, 2600)
		item := itemFromFragment(frag)
		if shouldKeepFragmentItem(item) {
			items = append(items, item)
		}
	}
	for _, loc := range attrTokenRe.FindAllStringSubmatchIndex(decoded, -1) {
		frag := jsonFragmentAround(decoded, loc[0], 1200, 1600)
		item := itemFromFragment(frag)
		if item.Token == "" && len(loc) >= 4 {
			item.Token = decoded[loc[2]:loc[3]]
		}
		if shouldKeepFragmentItem(item) {
			items = append(items, item)
		}
	}
	return items
}

func itemFromFragment(fragment string) feishuItem {
	item := feishuItem{
		Token: decodeTextValue(extractJSONField(fragment, []string{"file_token", "fileToken", "file_id", "fileId", "token", "id"})),
		URL:   decodeTextValue(extractJSONField(fragment, []string{"url", "src", "href", "download_url", "downloadUrl", "preview_url", "previewUrl", "cdn_url", "cdnUrl", "play_url", "playUrl"})),
		Name:  decodeTextValue(extractJSONField(fragment, []string{"name", "file_name", "fileName", "title", "text", "filename", "data-name", "data-file-name"})),
		Mime:  decodeTextValue(extractJSONField(fragment, []string{"mime", "mime_type", "mimeType", "content_type", "contentType"})),
		Kind:  strings.ToLower(decodeTextValue(extractJSONField(fragment, []string{"kind", "media_type", "mediaType", "file_type", "fileType", "type", "block_type", "blockType", "source"}))),
	}
	item.URL = normalizeFeishuURL(item.URL)
	if m := previewURLRe.FindStringSubmatch(item.URL); len(m) > 1 && item.Token == "" {
		item.Token = m[1]
	}
	if item.URL != "" && (strings.HasPrefix(item.URL, "blob:") || strings.HasPrefix(item.URL, "data:")) {
		item.URL = ""
	}
	item.Kind = feishuMediaKindFromHint(item.Kind, item.Mime, item.Name, item.URL)
	if item.Fmt == "" {
		item.Fmt = feishuFormatFromMime(item.Mime, item.Kind, firstNonEmpty(item.Name, item.URL))
	}
	if item.Name != "" {
		base, format := feishuSplitFilename(item.Name, item.Fmt)
		item.Name = base
		item.Fmt = firstNonEmpty(format, item.Fmt)
	}
	if item.Name == "" && item.URL != "" {
		base, format := feishuSplitFilename(path.Base(urlPath(item.URL)), item.Fmt)
		item.Name = base
		item.Fmt = firstNonEmpty(format, item.Fmt)
	}
	if item.Kind == "" {
		item.Kind = feishuKindFromFormat(item.Fmt)
	}
	return item
}

func shouldKeepFragmentItem(item feishuItem) bool {
	if item.Token == "" && item.URL == "" {
		return false
	}
	if item.Fmt == "" || !knownFeishuFormats[item.Fmt] {
		return false
	}
	if item.Name == "" {
		item.Name = firstNonEmpty(item.Token, "feishu_asset")
	}
	return item.Kind != "" || feishuKindFromFormat(item.Fmt) != ""
}

func feishuEntriesFromItems(items []feishuItem, h map[string]string, parentToken string) []*extractor.MediaInfo {
	entries := make([]*extractor.MediaInfo, 0, len(items))
	for i, item := range items {
		url := item.URL
		if url == "" && item.Token != "" {
			url = feishuPreviewURL(item.Token)
		}
		if url == "" {
			continue
		}
		name := firstNonEmpty(item.Name, item.Token, fmt.Sprintf("feishu_asset_%02d", i+1))
		format := firstNonEmpty(item.Fmt, feishuFormatFromMime(item.Mime, item.Kind, firstNonEmpty(name, url)), "mp4")
		entries = append(entries, &extractor.MediaInfo{
			Site:  "feishu",
			Title: util.SanitizeFilename(name),
			Streams: map[string]extractor.Stream{
				"source": {Quality: firstNonEmpty(item.Kind, "source"), URLs: []string{url}, Format: format, Size: item.Size, Headers: cloneHeaders(h)},
			},
			Extra: map[string]any{"kind": firstNonEmpty(item.Kind, "file"), "token": item.Token, "parent_token": parentToken, "mime": item.Mime},
		})
	}
	return entries
}

func feishuDocumentEntry(title, rawURL string, h map[string]string, extra map[string]any) *extractor.MediaInfo {
	return &extractor.MediaInfo{
		Site:  "feishu",
		Title: util.SanitizeFilename(firstNonEmpty(title, "feishu_document")),
		Streams: map[string]extractor.Stream{
			"document": {Quality: "document", URLs: []string{rawURL}, Format: "html", Headers: cloneHeaders(h)},
		},
		Extra: extra,
	}
}

func feishuSinglePreviewMedia(title, token, format string, h map[string]string, extra map[string]any) *extractor.MediaInfo {
	if format == "" {
		format = "mp4"
	}
	return &extractor.MediaInfo{
		Site:  "feishu",
		Title: util.SanitizeFilename(title),
		Streams: map[string]extractor.Stream{
			"preview": {Quality: "source", URLs: []string{feishuPreviewURL(token)}, Format: format, Headers: cloneHeaders(h)},
		},
		Extra: extra,
	}
}

func feishuPreviewURL(fileID string) string {
	return "https://internal-api-drive-stream.feishu.cn/space/api/box/stream/download/preview/" + neturl.PathEscape(fileID) + "?preview_type=16"
}
