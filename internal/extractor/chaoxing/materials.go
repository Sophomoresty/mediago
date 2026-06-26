package chaoxing

import (
	htmlpkg "html"
	"net/url"
	"regexp"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

// chaoxingFile mirrors the dict produced by Chaoxing_Course._get_file_list:
// {'type', 'data_id', 'name', 'object_id'?, 'loadurl'?}.
type chaoxingFile struct {
	Type     string
	DataID   string
	Name     string
	ObjectID string
	LoadURL  string
}

// fileExtAllow is the exact extension/type whitelist used by
// Chaoxing_Course._get_file_list when scanning the coursedata table rows.
var fileExtAllow = map[string]bool{
	"afolder": true, "pdf": true, "ppt": true, "pptx": true, "doc": true,
	"docx": true, "xls": true, "xlsx": true, "xlsm": true, "xla": true,
	"png": true, "jpg": true, "gif": true, "jpeg": true, "webp": true,
	"swf": true, "wmv": true, "zip": true, "rar": true, "7z": true,
	"caj": true, "bas": true, "msi": true, "mp4": true, "mp3": true,
	"flv": true, "mkv": true, "avi": true, "mov": true, "m4a": true,
	"txt": true, "exe": true, "cmd": true, "iso": true, "ove": true,
}

var (
	showPageRe = regexp.MustCompile(`page\.showPage\((\d+),\s*(\d+),\s*"course_change",\s*"首页",\s*"尾页"\)`)
	// toOpen('data_name','data_type',data_id,'loadurl','object_id','url',flag,source,'class_id','enc','video_type')
	toOpenRe = regexp.MustCompile(`toOpen\('(?:\\.|[^'])*','[^']*',(\d+),'((?:\\.|[^'])*)','([^']*)','(?:\\.|[^'])*',\d+,\d+,'[^']*','[^']*','[^']*'\)`)
	// <tbody id="tableId02"> ... </tbody>
	fileTbodyRe = regexp.MustCompile(`(?is)<tbody[^>]*\bid=["']tableId02["'][^>]*>([\s\S]*?)</tbody>`)
	fileRowRe   = regexp.MustCompile(`(?is)<tr\b[\s\S]*?</tr>`)
	attrTypeRe  = regexp.MustCompile(`(?is)\btype=["']([^"']*)["']`)
	attrIDRe    = regexp.MustCompile(`(?is)\bid=["']([^"']*)["']`)
	textInputRe = regexp.MustCompile(`(?is)<input\b[^>]*\btype=["']text["'][^>]*>`)
	inputValRe  = regexp.MustCompile(`(?is)\bvalue=["']([^"']*)["']`)
	onclickRe   = regexp.MustCompile(`(?is)\bonclick=["']([^"']*)["']`)
)

// buildFilesURL ports Chaoxing_Course._build_files_url. It needs cid, clazzid,
// enc and openc; returns "" otherwise so the caller fails closed.
func (x *chaoxingContext) buildFilesURL(openc, dataID string) string {
	if x.courseID == "" || x.clazzID == "" || x.enc == "" || openc == "" {
		return ""
	}
	values := url.Values{}
	values.Set("courseId", x.courseID)
	values.Set("classId", x.clazzID)
	values.Set("enc", x.enc)
	if x.cpi != "" {
		values.Set("cpi", x.cpi)
	}
	values.Set("openc", openc)
	values.Set("dataId", dataID)
	return x.abs("/coursedata") + "?" + values.Encode()
}

// buildFileDownloadURL ports Chaoxing_Course._build_file_download_url.
func (x *chaoxingContext) buildFileDownloadURL(dataID string) string {
	if x.courseID == "" || x.clazzID == "" || dataID == "" {
		return ""
	}
	values := url.Values{}
	values.Set("courseId", x.courseID)
	values.Set("classId", x.clazzID)
	values.Set("dataId", dataID)
	return x.abs("/coursedata/downloadData") + "?" + values.Encode()
}

// buildObjectDownloadURL ports Chaoxing_Course._build_object_download_url.
func (x *chaoxingContext) buildObjectDownloadURL(objectID string) string {
	if objectID == "" {
		return ""
	}
	base := strings.TrimRight(x.downpath, "/")
	if base == "" {
		return ""
	}
	return base + "/download/" + objectID
}

// iterFileDownloadURLs ports Chaoxing_Course._iter_file_download_urls: the
// ordered candidate URLs for one course-data file entry.
func (x *chaoxingContext) iterFileDownloadURLs(f chaoxingFile) []string {
	var out []string
	if u := x.buildObjectDownloadURL(f.ObjectID); u != "" {
		out = append(out, u)
	}
	if f.LoadURL != "" {
		if strings.HasPrefix(f.LoadURL, "http://") || strings.HasPrefix(f.LoadURL, "https://") {
			out = append(out, f.LoadURL)
		} else if strings.HasPrefix(f.LoadURL, "/") {
			out = append(out, x.abs(f.LoadURL))
		}
	}
	if u := x.buildFileDownloadURL(f.DataID); u != "" {
		contains := false
		for _, e := range out {
			if e == u {
				contains = true
				break
			}
		}
		if !contains {
			out = append(out, u)
		}
	}
	return out
}

// getFileList ports Chaoxing_Course._get_file_list: enumerate the course-data
// (attachment/material) table across paginated /coursedata pages.
func (x *chaoxingContext) getFileList(openc string) []chaoxingFile {
	first := x.buildFilesURL(openc, "")
	if first == "" {
		return nil
	}
	body, err := x.getString(first)
	if err != nil {
		return nil
	}
	x.extractAccessFromText(body)

	start, end := 0, 0
	if m := showPageRe.FindStringSubmatch(body); len(m) > 2 {
		start = atoi(m[1])
		end = atoi(m[2])
	}
	if end < start {
		return nil
	}

	var files []chaoxingFile
	for page := start; page <= end; page++ {
		pageURL := x.buildFilesURL(openc, "") + "&pages=" + url.QueryEscape(toStringInt(page))
		pageBody, perr := x.getString(pageURL)
		if perr != nil {
			continue
		}
		x.extractAccessFromText(pageBody)
		files = append(files, parseFileRows(pageBody)...)
	}
	return files
}

// parseFileRows mirrors the BeautifulSoup row scan inside _get_file_list.
func parseFileRows(text string) []chaoxingFile {
	tb := fileTbodyRe.FindStringSubmatch(text)
	if len(tb) < 2 {
		return nil
	}
	var out []chaoxingFile
	for _, row := range fileRowRe.FindAllString(tb[1], -1) {
		typ := ""
		if m := attrTypeRe.FindStringSubmatch(rowTag(row)); len(m) > 1 {
			typ = strings.ToLower(strings.TrimSpace(m[1]))
		}
		if !fileExtAllow[typ] {
			continue
		}
		dataID := ""
		if m := attrIDRe.FindStringSubmatch(rowTag(row)); len(m) > 1 {
			dataID = strings.TrimSpace(m[1])
		}
		input := textInputRe.FindString(row)
		if input == "" || dataID == "" {
			continue
		}
		name := ""
		if m := inputValRe.FindStringSubmatch(input); len(m) > 1 {
			name = cleanText(m[1])
		}
		f := chaoxingFile{Type: typ, DataID: dataID, Name: name}
		if oc := onclickRe.FindStringSubmatch(row); len(oc) > 1 {
			onclick := htmlpkg.UnescapeString(oc[1])
			if m := toOpenRe.FindStringSubmatch(onclick); len(m) > 3 {
				// _extract_file_open_args html-unescapes then "\'" -> "'".
				f.LoadURL = strings.ReplaceAll(htmlpkg.UnescapeString(m[2]), `\'`, `'`)
				f.ObjectID = strings.ReplaceAll(htmlpkg.UnescapeString(m[3]), `\'`, `'`)
			}
		}
		out = append(out, f)
	}
	return out
}

// rowTag returns just the opening <tr ...> tag of a row so attribute regexes
// match the row element rather than nested cells.
func rowTag(row string) string {
	if idx := strings.Index(row, ">"); idx >= 0 {
		return row[:idx+1]
	}
	return row
}

// resolveFileEntries converts the parsed course-data files into downloadable
// MediaInfo entries. Each entry exposes the candidate download URLs as a
// single stream (object download / loadurl / coursedata downloadData), all
// grounded in _iter_file_download_urls.
func (x *chaoxingContext) resolveFileEntries(openc string) []*extractor.MediaInfo {
	files := x.getFileList(openc)
	var out []*extractor.MediaInfo
	for i, f := range files {
		urls := x.iterFileDownloadURLs(f)
		filtered := urls[:0]
		seen := map[string]bool{}
		for _, u := range urls {
			if !isHTTPURL(u) || seen[u] {
				continue
			}
			seen[u] = true
			filtered = append(filtered, u)
		}
		if len(filtered) == 0 {
			continue
		}
		title := firstNonEmpty(f.Name, "chaoxing_file_"+f.DataID)
		format := mediaFormat(filtered[0], f.Type)
		entry := &extractor.MediaInfo{
			Site:  "chaoxing",
			Title: util.SanitizeFilename(title),
			Streams: map[string]extractor.Stream{"default": {
				Quality:   "default",
				URLs:      []string{filtered[0]},
				Format:    format,
				NeedMerge: format == "m3u8",
				Headers:   x.headers,
			}},
			Extra: compactExtra(map[string]any{
				"kind":      "file",
				"data_id":   f.DataID,
				"object_id": f.ObjectID,
				"mirrors":   mirrorList(filtered),
			}),
		}
		entry.Title = util.SanitizeFilename(prefixFileTitle(i+1, len(files), entry.Title))
		out = append(out, entry)
	}
	return out
}

func mirrorList(urls []string) []string {
	if len(urls) <= 1 {
		return nil
	}
	return urls[1:]
}

func prefixFileTitle(index, total int, title string) string {
	base := cleanText(title)
	if strings.HasPrefix(base, "(") || strings.HasPrefix(base, "[") || strings.HasPrefix(base, "#") {
		return base
	}
	if total > 1 {
		return "(" + toStringInt(index) + ")--" + base
	}
	return base
}

func toStringInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
