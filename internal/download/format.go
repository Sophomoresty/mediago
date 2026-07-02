package download

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/extractor"
)

// FormatSelector parses and applies format selection strings.
// Supported syntax:
//   - "best", "worst", "1080p", "720p", "bestvideo", "bestaudio"
//   - "best[height<=720]" — filter by height
//   - "1080p/720p/best" — fallback chain (try first, then second, etc.)
type FormatSelector struct {
	specs []formatSpec
}

type formatSpec struct {
	keyword string // "best", "worst", "1080p", "720p", "bestvideo", "bestaudio"
	filter  *formatFilter
}

type formatFilter struct {
	field string // "height"
	op    string // "<=", ">=", "<", ">", "="
	value int
}

var filterRe = regexp.MustCompile(`^(\w+)\[(\w+)([<>=!]+)(\d+)\]$`)

// ParseFormatSelector parses a format selection string.
func ParseFormatSelector(s string) *FormatSelector {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "best"
	}

	parts := strings.Split(s, "/")
	specs := make([]formatSpec, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		specs = append(specs, parseOneSpec(part))
	}
	if len(specs) == 0 {
		specs = append(specs, formatSpec{keyword: "best"})
	}
	return &FormatSelector{specs: specs}
}

func parseOneSpec(s string) formatSpec {
	// Check for filter syntax: keyword[field op value]
	if m := filterRe.FindStringSubmatch(s); m != nil {
		val, _ := strconv.Atoi(m[4])
		return formatSpec{
			keyword: m[1],
			filter:  &formatFilter{field: m[2], op: m[3], value: val},
		}
	}
	return formatSpec{keyword: s}
}

// Select applies the format selector against the stream map and returns the selected stream.
func (fs *FormatSelector) Select(streams map[string]extractor.Stream) (string, extractor.Stream) {
	if len(streams) == 0 {
		return "", extractor.Stream{}
	}

	for _, spec := range fs.specs {
		if id, s, ok := applySpec(spec, streams); ok {
			return id, s
		}
	}

	// Ultimate fallback: select best
	return applyBest(streams)
}

func applySpec(spec formatSpec, streams map[string]extractor.Stream) (string, extractor.Stream, bool) {
	// First apply any filter to narrow candidates
	candidates := streams
	if spec.filter != nil {
		candidates = filterStreams(streams, spec.filter)
		if len(candidates) == 0 {
			return "", extractor.Stream{}, false
		}
	}

	keyword := strings.ToLower(spec.keyword)

	switch keyword {
	case "best", "bestvideo":
		id, s := applyBest(candidates)
		return id, s, len(s.URLs) > 0 || s.Format != ""
	case "worst":
		id, s := applyWorst(candidates)
		return id, s, len(s.URLs) > 0 || s.Format != ""
	case "bestaudio":
		id, s := applyBestAudio(candidates)
		return id, s, len(s.URLs) > 0 || s.Format != ""
	default:
		// Try exact quality match (e.g. "1080p", "720p")
		for k, s := range candidates {
			if strings.EqualFold(s.Quality, keyword) {
				return k, s, true
			}
		}
		// Try with "p" suffix
		if !strings.HasSuffix(keyword, "p") {
			qp := keyword + "p"
			for k, s := range candidates {
				if strings.EqualFold(s.Quality, qp) {
					return k, s, true
				}
			}
		}
		return "", extractor.Stream{}, false
	}
}

func filterStreams(streams map[string]extractor.Stream, f *formatFilter) map[string]extractor.Stream {
	result := make(map[string]extractor.Stream)
	for k, s := range streams {
		if matchFilter(s, f) {
			result[k] = s
		}
	}
	return result
}

func matchFilter(s extractor.Stream, f *formatFilter) bool {
	var fieldVal int

	switch strings.ToLower(f.field) {
	case "height":
		fieldVal = extractHeight(s.Quality)
	case "width":
		fieldVal = extractWidth(s.Quality)
	default:
		return true // unknown field, pass through
	}

	if fieldVal == 0 {
		return false
	}

	switch f.op {
	case "<=":
		return fieldVal <= f.value
	case ">=":
		return fieldVal >= f.value
	case "<":
		return fieldVal < f.value
	case ">":
		return fieldVal > f.value
	case "=", "==":
		return fieldVal == f.value
	case "!=":
		return fieldVal != f.value
	default:
		return true
	}
}

var heightRe = regexp.MustCompile(`(\d{3,4})\s*[pP]?`)

func extractHeight(quality string) int {
	m := heightRe.FindStringSubmatch(quality)
	if m == nil {
		return 0
	}
	v, _ := strconv.Atoi(m[1])
	return v
}

func extractWidth(quality string) int {
	h := extractHeight(quality)
	switch h {
	case 2160:
		return 3840
	case 1440:
		return 2560
	case 1080:
		return 1920
	case 720:
		return 1280
	case 480:
		return 854
	case 360:
		return 640
	default:
		return 0
	}
}

func applyBest(streams map[string]extractor.Stream) (string, extractor.Stream) {
	keys := sortedKeys(streams)
	priorities := []string{"1080p", "720p", "480p", "360p"}
	for _, q := range priorities {
		for _, k := range keys {
			if strings.EqualFold(streams[k].Quality, q) {
				return k, streams[k]
			}
		}
	}
	if len(keys) > 0 {
		return keys[0], streams[keys[0]]
	}
	return "", extractor.Stream{}
}

func applyWorst(streams map[string]extractor.Stream) (string, extractor.Stream) {
	keys := sortedKeys(streams)
	priorities := []string{"360p", "480p", "720p", "1080p"}
	for _, q := range priorities {
		for _, k := range keys {
			if strings.EqualFold(streams[k].Quality, q) {
				return k, streams[k]
			}
		}
	}
	if len(keys) > 0 {
		return keys[len(keys)-1], streams[keys[len(keys)-1]]
	}
	return "", extractor.Stream{}
}

func applyBestAudio(streams map[string]extractor.Stream) (string, extractor.Stream) {
	keys := sortedKeys(streams)
	// Look for audio-only formats
	for _, k := range keys {
		s := streams[k]
		f := strings.ToLower(s.Format)
		if f == "m4a" || f == "mp3" || f == "aac" || f == "opus" || f == "ogg" {
			return k, s
		}
	}
	// Fallback to best
	return applyBest(streams)
}

func sortedKeys(streams map[string]extractor.Stream) []string {
	keys := make([]string, 0, len(streams))
	for k := range streams {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
