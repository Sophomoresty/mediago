package main

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Sophomoresty/mediago/internal/download"
	"github.com/Sophomoresty/mediago/internal/extractor"
	"golang.org/x/term"
)

const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
)

var quietMode bool

func setQuiet(q bool) {
	quietMode = q
}

var qualityResolutionRe = regexp.MustCompile(`(?i)(\d{3,4})\s*p?`)

func ttyEnabled(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}

func writeColoredLine(w *os.File, color, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if ttyEnabled(w) && color != "" {
		msg = color + msg + ansiReset
	}
	fmt.Fprintln(w, msg)
}

func infof(format string, args ...any) {
	if quietMode {
		return
	}
	writeColoredLine(os.Stderr, ansiGreen, "[info] "+format, args...)
}

func warnf(format string, args ...any) {
	if quietMode {
		return
	}
	writeColoredLine(os.Stderr, ansiYellow, "[warn] "+format, args...)
}

func errorf(format string, args ...any) {
	writeColoredLine(os.Stderr, ansiRed, "[error] "+format, args...)
}

func interruptedf() {
	if ttyEnabled(os.Stderr) {
		fmt.Fprint(os.Stderr, "\n")
		fmt.Fprintln(os.Stderr, ansiRed+"Interrupted"+ansiReset)
		return
	}
	fmt.Fprintln(os.Stderr, "\nInterrupted")
}

func downloadf(format string, args ...any) {
	if quietMode {
		return
	}
	writeColoredLine(os.Stderr, ansiGreen, "[download] "+format, args...)
}

func mergerf(format string, args ...any) {
	if quietMode {
		return
	}
	writeColoredLine(os.Stderr, ansiGreen, "[Merger] "+format, args...)
}

func subtitlef(format string, args ...any) {
	if quietMode {
		return
	}
	writeColoredLine(os.Stderr, ansiGreen, "[subtitle] "+format, args...)
}

func reportf(format string, args ...any) {
	writeColoredLine(os.Stdout, ansiGreen, format, args...)
}

func reportInfof(format string, args ...any) {
	reportf("[info] "+format, args...)
}

func simulatef(format string, args ...any) {
	writeColoredLine(os.Stdout, ansiGreen, "[simulate] "+format, args...)
}

func downloadItemMessage(itemIndex, totalItems int, title string) string {
	return fmt.Sprintf("[download] Downloading item %d of %d: %s", itemIndex, totalItems, title)
}

func printFormats(info *extractor.MediaInfo) error {
	reportInfof("Available formats for: %s", info.Title)

	rows := formatRows(info)
	if len(rows) == 0 {
		reportf("No formats available.")
		return nil
	}

	widths := []int{
		len("ID"),
		len("QUALITY"),
		len("RESOLUTION"),
		len("CODEC"),
		len("FORMAT"),
		len("SIZE"),
	}
	for _, row := range rows {
		widths[0] = maxInt(widths[0], len(row.id))
		widths[1] = maxInt(widths[1], len(row.quality))
		widths[2] = maxInt(widths[2], len(row.resolution))
		widths[3] = maxInt(widths[3], len(row.codec))
		widths[4] = maxInt(widths[4], len(row.format))
		widths[5] = maxInt(widths[5], len(row.size))
	}

	fmt.Fprintf(os.Stdout, "%-*s  %-*s  %-*s  %-*s  %-*s  %-*s\n",
		widths[0], "ID",
		widths[1], "QUALITY",
		widths[2], "RESOLUTION",
		widths[3], "CODEC",
		widths[4], "FORMAT",
		widths[5], "SIZE",
	)
	fmt.Println(strings.Repeat("-", widths[0]+widths[1]+widths[2]+widths[3]+widths[4]+widths[5]+10))
	for _, row := range rows {
		fmt.Fprintf(os.Stdout, "%-*s  %-*s  %-*s  %-*s  %-*s  %-*s\n",
			widths[0], row.id,
			widths[1], row.quality,
			widths[2], row.resolution,
			widths[3], row.codec,
			widths[4], row.format,
			widths[5], row.size,
		)
	}
	return nil
}

func printSimulation(info *extractor.MediaInfo, itemIndex, totalItems int) error {
	_, stream := download.SelectBestStream(info.Streams, formatSpec)
	if len(stream.URLs) == 0 && stream.Format == "" {
		return fmt.Errorf("no formats available: %s", info.Title)
	}

	outFilename := applyTemplate(outputTemplate, info, stream)
	if totalItems > 1 {
		simulatef("Item %d of %d: %s", itemIndex, totalItems, info.Title)
	}
	simulatef("Would download: %s", outFilename)
	simulatef("Selected: %s | %s | %s | %s", firstNonEmpty(stream.Quality, "unknown"), resolutionFromQuality(stream.Quality), codecFromStream(stream), firstNonEmpty(stream.Format, "unknown"))
	return nil
}

type formatRow struct {
	id         string
	quality    string
	resolution string
	codec      string
	format     string
	size       string
}

func formatRows(info *extractor.MediaInfo) []formatRow {
	if info == nil || len(info.Streams) == 0 {
		return nil
	}

	keys := make([]string, 0, len(info.Streams))
	for k := range info.Streams {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri := formatRank(info.Streams[keys[i]], keys[i])
		rj := formatRank(info.Streams[keys[j]], keys[j])
		if ri != rj {
			return ri > rj
		}
		return keys[i] < keys[j]
	})

	rows := make([]formatRow, 0, len(keys))
	for _, id := range keys {
		stream := info.Streams[id]
		rows = append(rows, formatRow{
			id:         id,
			quality:    firstNonEmpty(stream.Quality, "unknown"),
			resolution: resolutionFromQuality(stream.Quality),
			codec:      codecFromStream(stream),
			format:     firstNonEmpty(stream.Format, "unknown"),
			size:       humanSize(stream.Size),
		})
	}
	return rows
}

func formatRank(stream extractor.Stream, id string) int {
	q := strings.ToLower(strings.TrimSpace(firstNonEmpty(stream.Quality, id)))
	switch q {
	case "source":
		return 5000
	case "best":
		return 4500
	case "default":
		return 4000
	case "hd", "high":
		return 3500
	case "sd", "medium":
		return 3000
	case "low":
		return 1000
	}
	if m := qualityResolutionRe.FindStringSubmatch(q); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

func resolutionFromQuality(quality string) string {
	q := strings.ToLower(strings.TrimSpace(quality))
	switch {
	case strings.Contains(q, "2160"), strings.Contains(q, "4k"):
		return "3840x2160"
	case strings.Contains(q, "1440"), strings.Contains(q, "2k"):
		return "2560x1440"
	case strings.Contains(q, "1080"):
		return "1920x1080"
	case strings.Contains(q, "720"):
		return "1280x720"
	case strings.Contains(q, "480"):
		return "854x480"
	case strings.Contains(q, "360"):
		return "640x360"
	case strings.Contains(q, "240"):
		return "426x240"
	case strings.Contains(q, "144"):
		return "256x144"
	default:
		return "unknown"
	}
}

func codecFromStream(stream extractor.Stream) string {
	format := strings.ToLower(strings.TrimSpace(stream.Format))
	firstURL := ""
	if len(stream.URLs) > 0 {
		firstURL = strings.ToLower(strings.TrimSpace(stream.URLs[0]))
	}

	switch {
	case format == "dash" || strings.Contains(firstURL, ".mpd"):
		return "h264+aac"
	case format == "m3u8" || strings.Contains(firstURL, ".m3u8"):
		return "avc"
	case format == "mp4" || strings.Contains(firstURL, ".mp4") || format == "flv" || strings.Contains(firstURL, ".flv"):
		return "h264"
	case format == "m4a" || format == "aac" || strings.Contains(firstURL, ".m4a") || strings.Contains(firstURL, ".aac"):
		return "aac"
	case stream.AudioURL != "":
		return "h264+aac"
	default:
		return "unknown"
	}
}

func humanSize(size int64) string {
	if size <= 0 {
		return "unknown"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d%s", size, units[unit])
	}
	return fmt.Sprintf("%.1f%s", math.Round(value*10)/10, units[unit])
}

func sizeStringForPath(path string, fallback int64) string {
	if st, err := os.Stat(path); err == nil {
		return humanSize(st.Size())
	}
	return humanSize(fallback)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
