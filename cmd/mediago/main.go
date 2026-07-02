package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Sophomoresty/mediago/internal/cookie"
	"github.com/Sophomoresty/mediago/internal/download"
	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"

	_ "github.com/Sophomoresty/mediago/internal/extractor/ahu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/aishangke"
	_ "github.com/Sophomoresty/mediago/internal/extractor/baijiayunxiao"
	_ "github.com/Sophomoresty/mediago/internal/extractor/bilibili"
	_ "github.com/Sophomoresty/mediago/internal/extractor/caixuetang"
	_ "github.com/Sophomoresty/mediago/internal/extractor/cctalk"
	_ "github.com/Sophomoresty/mediago/internal/extractor/cctv"
	_ "github.com/Sophomoresty/mediago/internal/extractor/chaoge"
	_ "github.com/Sophomoresty/mediago/internal/extractor/chaoxing"
	_ "github.com/Sophomoresty/mediago/internal/extractor/ckjr"
	_ "github.com/Sophomoresty/mediago/internal/extractor/classin"
	_ "github.com/Sophomoresty/mediago/internal/extractor/cnmooc"
	_ "github.com/Sophomoresty/mediago/internal/extractor/cto51"
	_ "github.com/Sophomoresty/mediago/internal/extractor/dingtalk"
	_ "github.com/Sophomoresty/mediago/internal/extractor/dongao"
	_ "github.com/Sophomoresty/mediago/internal/extractor/douyin"
	_ "github.com/Sophomoresty/mediago/internal/extractor/duanshu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/enetedu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/eoffcn"
	_ "github.com/Sophomoresty/mediago/internal/extractor/feishu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/fenbi"
	_ "github.com/Sophomoresty/mediago/internal/extractor/gaodun"
	_ "github.com/Sophomoresty/mediago/internal/extractor/gaotu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/gongxuanwang"
	_ "github.com/Sophomoresty/mediago/internal/extractor/haiyangknow"
	_ "github.com/Sophomoresty/mediago/internal/extractor/haozaixian"
	_ "github.com/Sophomoresty/mediago/internal/extractor/houda"
	_ "github.com/Sophomoresty/mediago/internal/extractor/houdu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/hqwx"
	_ "github.com/Sophomoresty/mediago/internal/extractor/htknow"
	_ "github.com/Sophomoresty/mediago/internal/extractor/huatu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/huke88"
	_ "github.com/Sophomoresty/mediago/internal/extractor/icourse163"
	_ "github.com/Sophomoresty/mediago/internal/extractor/icourses"
	_ "github.com/Sophomoresty/mediago/internal/extractor/icve"
	_ "github.com/Sophomoresty/mediago/internal/extractor/imooc"
	_ "github.com/Sophomoresty/mediago/internal/extractor/itbaizhan"
	_ "github.com/Sophomoresty/mediago/internal/extractor/jianshe99"
	_ "github.com/Sophomoresty/mediago/internal/extractor/jinbangshidai"
	_ "github.com/Sophomoresty/mediago/internal/extractor/jingtongxue"
	_ "github.com/Sophomoresty/mediago/internal/extractor/kaimingzhixue"
	_ "github.com/Sophomoresty/mediago/internal/extractor/kaoyanvip"
	_ "github.com/Sophomoresty/mediago/internal/extractor/keqq"
	_ "github.com/Sophomoresty/mediago/internal/extractor/koolearn"
	_ "github.com/Sophomoresty/mediago/internal/extractor/kuke"
	_ "github.com/Sophomoresty/mediago/internal/extractor/ledu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/lexueyun"
	_ "github.com/Sophomoresty/mediago/internal/extractor/lizhiweike"
	_ "github.com/Sophomoresty/mediago/internal/extractor/luffycity"
	_ "github.com/Sophomoresty/mediago/internal/extractor/magedu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/mashibing"
	_ "github.com/Sophomoresty/mediago/internal/extractor/mddclass"
	_ "github.com/Sophomoresty/mediago/internal/extractor/med66"
	_ "github.com/Sophomoresty/mediago/internal/extractor/meeting"
	_ "github.com/Sophomoresty/mediago/internal/extractor/minshi"
	_ "github.com/Sophomoresty/mediago/internal/extractor/nmkjxy"
	_ "github.com/Sophomoresty/mediago/internal/extractor/open163"
	_ "github.com/Sophomoresty/mediago/internal/extractor/orangevip"
	_ "github.com/Sophomoresty/mediago/internal/extractor/plaso"
	_ "github.com/Sophomoresty/mediago/internal/extractor/qihang"
	_ "github.com/Sophomoresty/mediago/internal/extractor/qlchat"
	_ "github.com/Sophomoresty/mediago/internal/extractor/renrenjiang"
	_ "github.com/Sophomoresty/mediago/internal/extractor/sanjieke"
	_ "github.com/Sophomoresty/mediago/internal/extractor/shanxiang"
	_ "github.com/Sophomoresty/mediago/internal/extractor/sier"
	_ "github.com/Sophomoresty/mediago/internal/extractor/sites"
	_ "github.com/Sophomoresty/mediago/internal/extractor/smartedu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/speiyou"
	_ "github.com/Sophomoresty/mediago/internal/extractor/tmooc"
	_ "github.com/Sophomoresty/mediago/internal/extractor/unipus"
	_ "github.com/Sophomoresty/mediago/internal/extractor/wallstreets"
	_ "github.com/Sophomoresty/mediago/internal/extractor/wangxiao"
	_ "github.com/Sophomoresty/mediago/internal/extractor/wangxiao233"
	_ "github.com/Sophomoresty/mediago/internal/extractor/wendao"
	_ "github.com/Sophomoresty/mediago/internal/extractor/wowtiku"
	_ "github.com/Sophomoresty/mediago/internal/extractor/xiaoeapp"
	_ "github.com/Sophomoresty/mediago/internal/extractor/xiaoetech"
	_ "github.com/Sophomoresty/mediago/internal/extractor/xiwang"
	_ "github.com/Sophomoresty/mediago/internal/extractor/xsteach"
	_ "github.com/Sophomoresty/mediago/internal/extractor/xueersi"
	_ "github.com/Sophomoresty/mediago/internal/extractor/xuelang"
	_ "github.com/Sophomoresty/mediago/internal/extractor/xuetang"
	_ "github.com/Sophomoresty/mediago/internal/extractor/yangcong"
	_ "github.com/Sophomoresty/mediago/internal/extractor/yikaobang"
	_ "github.com/Sophomoresty/mediago/internal/extractor/yixiaoerguo"
	_ "github.com/Sophomoresty/mediago/internal/extractor/yizhiknow"
	_ "github.com/Sophomoresty/mediago/internal/extractor/youdao"
	_ "github.com/Sophomoresty/mediago/internal/extractor/youyuan"
	_ "github.com/Sophomoresty/mediago/internal/extractor/youzan"
	_ "github.com/Sophomoresty/mediago/internal/extractor/zhaozhao"
	_ "github.com/Sophomoresty/mediago/internal/extractor/zhengbao"
	_ "github.com/Sophomoresty/mediago/internal/extractor/zhihuishu"
	_ "github.com/Sophomoresty/mediago/internal/extractor/zlketang"
)

var version = "dev"

var (
	formatSpec      string
	outputTemplate  string
	cookieFile      string
	cookieBrowser   string
	listFormats     bool
	dumpJSON        bool
	simulate        bool
	writeInfoJSON   bool
	writeSubs       bool
	noOverwrites    bool
	concurrency     int
	listExtractors  bool
	downloadAll     bool
	mergeOutputFmt  string
	noProgress      bool
	proxy           string
	batchFile       string
	downloadArchive string
	limitRate       string
	verbose         bool
	quiet           bool
	updateCheck     bool
	playlistItems   string
	matchFilter     string
	embedSubs       bool
	embedThumbnail  bool
	embedMetadata   bool
	recodeVideo     string
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := &cobra.Command{
		Use:   "mediago [flags] URL [URL...]",
		Short: "Download media from 107 Chinese platforms",
		Long: `MediGo - download videos from Chinese educational and media platforms.
Similar to yt-dlp but focused on Chinese internet platforms.`,
		RunE:              runMain,
		Args:              cobra.ArbitraryArgs,
		DisableAutoGenTag: true,
		SilenceUsage:      true,
		SilenceErrors:     true,
	}
	rootCmd.SetContext(ctx)
	rootCmd.Version = version
	rootCmd.SetVersionTemplate("mediago {{.Version}}\n")

	// Format selection (yt-dlp: -f, --format)
	rootCmd.Flags().StringVarP(&formatSpec, "format", "f", "best", "format selection (best/worst/1080p/720p/480p)")

	// Output (yt-dlp: -o, --output)
	rootCmd.Flags().StringVarP(&outputTemplate, "output", "o", "%(title)s.%(ext)s", "output filename template")

	// Cookie options (same as yt-dlp)
	rootCmd.Flags().StringVar(&cookieFile, "cookies", "", "Netscape cookie file path")
	rootCmd.Flags().StringVar(&cookieBrowser, "cookies-from-browser", "", "read cookies from browser (chrome/edge/firefox)")

	// Info/listing (yt-dlp: -F, -j, --write-info-json)
	rootCmd.Flags().BoolVarP(&listFormats, "list-formats", "F", false, "list available formats and exit")
	rootCmd.Flags().BoolVarP(&dumpJSON, "dump-json", "j", false, "dump info JSON to stdout and exit")
	rootCmd.Flags().BoolVar(&simulate, "simulate", false, "show extracted info without downloading")
	rootCmd.Flags().BoolVar(&writeInfoJSON, "write-info-json", false, "write .info.json file alongside download")
	rootCmd.Flags().BoolVar(&writeSubs, "write-subs", false, "write subtitle files alongside download")

	// Download options
	rootCmd.Flags().BoolVar(&noOverwrites, "no-overwrites", false, "do not overwrite existing files")
	rootCmd.Flags().IntVarP(&concurrency, "concurrent-fragments", "N", 10, "number of concurrent fragment downloads")
	rootCmd.Flags().BoolVar(&downloadAll, "yes-playlist", false, "download all items in a playlist/course")
	rootCmd.Flags().StringVar(&mergeOutputFmt, "merge-output-format", "mp4", "merge output container (mp4/mkv/webm)")
	rootCmd.Flags().BoolVar(&noProgress, "no-progress", false, "suppress progress bar")
	rootCmd.Flags().StringVar(&proxy, "proxy", "", "HTTP/SOCKS proxy URL")

	// Batch & archive (yt-dlp: -a, --download-archive)
	rootCmd.Flags().StringVarP(&batchFile, "batch-file", "a", "", "file containing URLs to download (one per line)")
	rootCmd.Flags().StringVar(&downloadArchive, "download-archive", "", "file to record downloaded items; skip already-recorded ones")

	// Extractor listing (yt-dlp: --list-extractors)
	rootCmd.Flags().BoolVar(&listExtractors, "list-extractors", false, "list all supported sites and exit")

	// Rate limiting
	rootCmd.Flags().StringVar(&limitRate, "limit-rate", "", "download rate limit (e.g. 1M, 500K)")

	// Verbose/debug
	rootCmd.Flags().BoolVar(&verbose, "verbose", false, "enable verbose/debug output")

	// Quiet mode
	rootCmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress info and warning output")

	// Version check
	rootCmd.Flags().BoolVar(&updateCheck, "update-check", false, "check for newer version on GitHub")

	// Playlist items selection
	rootCmd.Flags().StringVar(&playlistItems, "playlist-items", "", "playlist items to download (e.g. 1-5,10,15-20)")

	// Match filter
	rootCmd.Flags().StringVar(&matchFilter, "match-filter", "", "filter entries by field (e.g. \"duration>60\", \"title~=math\")")

	// Post-processing
	rootCmd.Flags().BoolVar(&embedSubs, "embed-subs", false, "embed subtitles into the video file (requires ffmpeg)")
	rootCmd.Flags().BoolVar(&embedThumbnail, "embed-thumbnail", false, "embed thumbnail as cover art (requires ffmpeg)")
	rootCmd.Flags().BoolVar(&embedMetadata, "embed-metadata", false, "embed title/artist/date metadata (requires ffmpeg)")
	rootCmd.Flags().StringVar(&recodeVideo, "recode-video", "", "re-encode video to format (mp4/mkv/webm, requires ffmpeg)")

	// Version
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("mediago %s\n", version)
		},
	})

	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, context.Canceled) {
			interruptedf()
			os.Exit(130)
		}
		errorf("%v", err)
		os.Exit(1)
	}
}

func runMain(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Apply quiet mode
	if quiet {
		setQuiet(true)
	}

	if updateCheck {
		return checkForUpdate()
	}

	if listExtractors {
		return printExtractors()
	}

	// Read batch file if specified
	urls := append([]string{}, args...)
	if batchFile != "" {
		batchURLs, err := readBatchFile(batchFile)
		if err != nil {
			return fmt.Errorf("failed to read batch file: %w", err)
		}
		urls = append(urls, batchURLs...)
	}

	if len(urls) == 0 {
		return cmd.Help()
	}

	if proxy != "" {
		if err := util.SetDefaultProxy(proxy); err != nil {
			return fmt.Errorf("invalid --proxy value: %w", err)
		}
	}

	// Load download archive
	var archive *download.Archive
	if downloadArchive != "" {
		var err error
		archive, err = download.LoadArchive(downloadArchive)
		if err != nil {
			return fmt.Errorf("failed to load download archive: %w", err)
		}
	}

	failures := 0
	for _, url := range urls {
		if err := processURL(ctx, url, archive); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			errorf("%v", err)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d URLs failed", failures, len(urls))
	}
	return nil
}

func readBatchFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var urls []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, scanner.Err()
}

func processURL(ctx context.Context, url string, archive *download.Archive) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	ext, site, err := extractor.MatchWithSite(url)
	if err != nil {
		return fmt.Errorf("unsupported URL: %s\nUse --list-extractors to see supported sites.", url)
	}
	infof("Extracting: %s %s", site.Name, url)
	if verbose {
		fmt.Fprintf(os.Stderr, "[debug] Matched extractor: %s (URL: %s)\n", site.Name, url)
	}

	store := cookie.NewStore()
	if cookieFile != "" {
		if err := store.LoadFromFile(cookieFile); err != nil {
			return fmt.Errorf("failed to load cookies: %w", err)
		}
	}
	if cookieBrowser != "" {
		if err := store.LoadFromBrowser(cookieBrowser); err != nil {
			return fmt.Errorf("failed to read browser cookies: %w", err)
		}
	}

	opts := &extractor.ExtractOpts{
		Cookies:  store.Jar(),
		Quality:  formatSpec,
		ListOnly: listFormats,
	}

	info, err := ext.Extract(url, opts)
	if err != nil {
		return fmt.Errorf("[%s] %w", url, err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if dumpJSON {
		return printJSON(info)
	}

	if info.IsPlaylist() {
		infof("Playlist: %s (%d items)", info.Title, len(info.Entries))

		// Apply playlist-items filter
		if playlistItems != "" {
			indices := parsePlaylistItems(playlistItems, len(info.Entries))
			filtered := make([]*extractor.MediaInfo, 0)
			for _, idx := range indices {
				if idx >= 0 && idx < len(info.Entries) {
					filtered = append(filtered, info.Entries[idx])
				}
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "[debug] Playlist items filter: %d -> %d entries\n", len(info.Entries), len(filtered))
			}
			info.Entries = filtered
		}

		// Apply match-filter
		if matchFilter != "" {
			filtered := make([]*extractor.MediaInfo, 0)
			for i, entry := range info.Entries {
				if entry == nil {
					continue
				}
				if matchesFilter(entry, i, matchFilter) {
					filtered = append(filtered, entry)
				}
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "[debug] Match filter: %d -> %d entries\n", len(info.Entries), len(filtered))
			}
			info.Entries = filtered
		}

		// Populate playlist_index in Extra for template use
		for i, entry := range info.Entries {
			if entry == nil {
				continue
			}
			if entry.Extra == nil {
				entry.Extra = make(map[string]any)
			}
			entry.Extra["playlist_index"] = i + 1
		}

		if !downloadAll {
			warnf("Downloading only the first item. Use --yes-playlist to download all.")
			if len(info.Entries) > 0 && info.Entries[0] != nil {
				infof("%s", info.Entries[0].Title)
				return downloadEntry(ctx, 0, 1, info.Entries[0], archive)
			}
			return fmt.Errorf("playlist is empty")
		}
		if listFormats {
			warnf("use a single-item URL with -F to inspect formats")
			return nil
		}
		if simulate {
			for i, entry := range info.Entries {
				if entry == nil {
					continue
				}
				if err := printSimulation(entry, i+1, len(info.Entries)); err != nil {
					return err
				}
			}
			return nil
		}
		// Parallel extraction: re-extract entries that need it (API calls are the slow part)
		extractOpts := &extractor.ExtractOpts{
			Cookies:  store.Jar(),
			Quality:  formatSpec,
			ListOnly: false,
		}
		entries := parallelExtractEntries(ctx, info.Entries, extractOpts)
		entryFailures := 0
		for i, entry := range entries {
			if entry == nil {
				continue
			}
			if err := downloadEntry(ctx, i, len(info.Entries), entry, archive); err != nil {
				if errors.Is(err, context.Canceled) {
					return err
				}
				errorf("[%d/%d %s]: %v", i+1, len(info.Entries), firstNonEmpty(entry.Title, fmt.Sprintf("item-%d", i+1)), err)
				entryFailures++
			}
		}
		if entryFailures > 0 {
			return fmt.Errorf("%d of %d playlist items failed", entryFailures, len(info.Entries))
		}
		return nil
	}

	infof("%s", info.Title)
	if simulate {
		return printSimulation(info, 0, 0)
	}
	return downloadOneWithArchive(ctx, info, archive)
}

func downloadEntry(ctx context.Context, itemIndex, totalItems int, info *extractor.MediaInfo, archive *download.Archive) error {
	downloadf("%s", downloadItemMessage(itemIndex+1, totalItems, firstNonEmpty(info.Title, fmt.Sprintf("item-%d", itemIndex+1))))
	return downloadOneWithArchiveFn(ctx, info, archive)
}

var downloadOneWithArchiveFn = downloadOneWithArchive

func downloadOneWithArchive(ctx context.Context, info *extractor.MediaInfo, archive *download.Archive) error {
	// Check archive before downloading
	if archive != nil {
		id := download.MakeArchiveID(info)
		if archive.Contains(id) {
			infof("Already in archive, skipping: %s", info.Title)
			return nil
		}
	}

	if err := downloadOne(ctx, info); err != nil {
		return err
	}

	// Record in archive after successful download
	if archive != nil {
		id := download.MakeArchiveID(info)
		if err := archive.Add(id); err != nil {
			warnf("Failed to update download archive: %v", err)
		}
	}
	return nil
}

func downloadOne(ctx context.Context, info *extractor.MediaInfo) error {
	if listFormats {
		return printFormats(info)
	}

	// Use format selector for advanced format syntax
	selector := download.ParseFormatSelector(formatSpec)
	_, stream := selector.Select(info.Streams)
	if len(stream.URLs) == 0 && stream.Format == "" {
		return fmt.Errorf("no formats available: %s", info.Title)
	}

	outFilename := applyTemplate(outputTemplate, info, stream)

	engine := download.New(download.Opts{
		Concurrency:       concurrency,
		OutputDir:         outputDirFromTemplate(outFilename),
		Overwrite:         !noOverwrites,
		Retries:           3,
		NoProgress:        noProgress,
		Proxy:             proxy,
		Context:           ctx,
		MergeOutputFormat:  mergeOutputFmt,
		LimitRate:         limitRate,
		Verbose:           verbose,
	})

	info.Title = baseFromTemplate(outFilename)

	if strings.EqualFold(stream.Format, "dash") && engine.HasFFmpeg() {
		mergerf("Merging formats into %s", outFilename)
	}
	outPath, err := engine.Download(info, stream)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	downloadf("100%% of %s", sizeStringForPath(outPath, stream.Size))
	if writeInfoJSON {
		writeInfoJSONFile(outPath, info)
	}

	var subtitlePaths []string
	if writeSubs || embedSubs {
		if subs, err := engine.DownloadSubtitles(info, outPath); err != nil {
			return fmt.Errorf("download subtitles: %w", err)
		} else {
			subtitlePaths = subs
			for _, sub := range subs {
				subtitlef("%s", sub)
			}
		}
	}

	// Post-processing
	ppOpts := download.PostProcessOpts{
		EmbedSubs:      embedSubs,
		EmbedThumbnail: embedThumbnail,
		EmbedMetadata:  embedMetadata,
		RecodeVideo:    recodeVideo,
	}
	if ppOpts.EmbedSubs || ppOpts.EmbedThumbnail || ppOpts.EmbedMetadata || ppOpts.RecodeVideo != "" {
		if !engine.HasFFmpeg() {
			warnf("ffmpeg not found, skipping post-processing")
		} else {
			finalPath, ppErr := engine.PostProcess(outPath, info, subtitlePaths, ppOpts)
			if ppErr != nil {
				return fmt.Errorf("post-processing failed: %w", ppErr)
			}
			if finalPath != outPath {
				infof("Output: %s", finalPath)
			}
		}
	}

	return nil
}

func printJSON(info *extractor.MediaInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func printExtractors() error {
	sites := extractor.ListSites()
	for _, s := range sites {
		auth := ""
		if s.NeedAuth {
			auth = " (auth)"
		}
		fmt.Printf("%s: %s%s\n", s.Name, s.URL, auth)
	}
	fmt.Printf("\n%d extractors\n", len(sites))
	return nil
}

func applyTemplate(tmpl string, info *extractor.MediaInfo, stream extractor.Stream) string {
	ext := stream.Format
	if ext == "m3u8" || ext == "dash" {
		ext = mergeOutputFmt
	}
	if ext == "" {
		ext = "mp4"
	}

	// Extract extra fields with defaults
	id := ""
	playlistIndex := ""
	uploadDate := ""
	duration := ""
	channel := ""
	autonumber := ""
	if info.Extra != nil {
		if v, ok := info.Extra["id"].(string); ok {
			id = v
		}
		if v, ok := info.Extra["playlist_index"].(int); ok {
			playlistIndex = fmt.Sprintf("%02d", v)
		}
		if v, ok := info.Extra["upload_date"].(string); ok {
			uploadDate = v
		}
		if v, ok := info.Extra["duration"].(string); ok {
			duration = v
		}
		if v, ok := info.Extra["channel"].(string); ok {
			channel = v
		}
		if v, ok := info.Extra["autonumber"].(int); ok {
			autonumber = fmt.Sprintf("%05d", v)
		}
	}

	r := strings.NewReplacer(
		"%(title)s", util.SanitizeFilename(info.Title),
		"%(ext)s", ext,
		"%(site)s", util.SanitizeFilename(info.Site),
		"%(artist)s", util.SanitizeFilename(info.Artist),
		"%(quality)s", stream.Quality,
		"%(id)s", id,
		"%(playlist_index)s", playlistIndex,
		"%(upload_date)s", uploadDate,
		"%(duration)s", duration,
		"%(channel)s", util.SanitizeFilename(channel),
		"%(autonumber)s", autonumber,
	)
	return r.Replace(tmpl)
}

func outputDirFromTemplate(filename string) string {
	dir := "."
	if idx := strings.LastIndex(filename, "/"); idx > 0 {
		dir = filename[:idx]
	}
	return dir
}

func baseFromTemplate(filename string) string {
	if idx := strings.LastIndex(filename, "/"); idx >= 0 {
		filename = filename[idx+1:]
	}
	if idx := strings.LastIndex(filename, "."); idx > 0 {
		filename = filename[:idx]
	}
	return filename
}

func writeInfoJSONFile(videoPath string, info *extractor.MediaInfo) {
	jsonPath := videoPath + ".info.json"
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(jsonPath, data, 0o644)
}

// checkForUpdate queries GitHub API for the latest release and compares with current version.
func checkForUpdate() error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/Sophomoresty/mediago/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("failed to parse release info: %w", err)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(version, "v")

	if latest != current {
		fmt.Fprintf(os.Stderr, "Update available: %s -> %s\nDownload: %s\n", current, latest, release.HTMLURL)
	} else {
		fmt.Fprintf(os.Stderr, "mediago %s is up to date.\n", current)
	}
	return nil
}

// parsePlaylistItems parses a playlist items string like "1-5,10,15-20" into zero-based indices.
func parsePlaylistItems(s string, total int) []int {
	var result []int
	seen := make(map[int]bool)
	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, "-"); idx > 0 {
			startStr := part[:idx]
			endStr := part[idx+1:]
			start := 0
			end := 0
			fmt.Sscanf(startStr, "%d", &start)
			fmt.Sscanf(endStr, "%d", &end)
			if start < 1 {
				start = 1
			}
			if end > total {
				end = total
			}
			for i := start; i <= end; i++ {
				zeroIdx := i - 1
				if !seen[zeroIdx] {
					seen[zeroIdx] = true
					result = append(result, zeroIdx)
				}
			}
		} else {
			val := 0
			fmt.Sscanf(part, "%d", &val)
			zeroIdx := val - 1
			if zeroIdx >= 0 && zeroIdx < total && !seen[zeroIdx] {
				seen[zeroIdx] = true
				result = append(result, zeroIdx)
			}
		}
	}
	return result
}

// matchesFilter checks if a media entry matches the filter expression.
// Supported: "duration>60", "title~=math", "index<10"
func matchesFilter(info *extractor.MediaInfo, index int, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}

	// Parse: field op value
	var field, op, value string
	// Try two-char ops first
	for _, twoOp := range []string{"~=", "!~=", ">=", "<=", "!="} {
		if idx := strings.Index(filter, twoOp); idx > 0 {
			field = strings.TrimSpace(filter[:idx])
			op = twoOp
			value = strings.TrimSpace(filter[idx+len(twoOp):])
			break
		}
	}
	// Try single-char ops
	if op == "" {
		for _, singleOp := range []string{">", "<", "="} {
			if idx := strings.Index(filter, singleOp); idx > 0 {
				field = strings.TrimSpace(filter[:idx])
				op = singleOp
				value = strings.TrimSpace(filter[idx+len(singleOp):])
				break
			}
		}
	}

	if field == "" || op == "" {
		return true // can't parse, pass through
	}

	switch strings.ToLower(field) {
	case "title":
		return matchStringOp(info.Title, op, value)
	case "duration":
		var dur int
		if info.Extra != nil {
			if d, ok := info.Extra["duration"].(float64); ok {
				dur = int(d)
			} else if d, ok := info.Extra["duration"].(int); ok {
				dur = d
			}
		}
		return matchIntOp(dur, op, value)
	case "index":
		return matchIntOp(index+1, op, value)
	default:
		return true
	}
}

func matchStringOp(fieldVal, op, target string) bool {
	switch op {
	case "~=":
		return strings.Contains(strings.ToLower(fieldVal), strings.ToLower(target))
	case "!~=":
		return !strings.Contains(strings.ToLower(fieldVal), strings.ToLower(target))
	case "=", "==":
		return strings.EqualFold(fieldVal, target)
	case "!=":
		return !strings.EqualFold(fieldVal, target)
	default:
		return true
	}
}

func matchIntOp(fieldVal int, op, target string) bool {
	var targetVal int
	fmt.Sscanf(target, "%d", &targetVal)
	switch op {
	case ">":
		return fieldVal > targetVal
	case "<":
		return fieldVal < targetVal
	case ">=":
		return fieldVal >= targetVal
	case "<=":
		return fieldVal <= targetVal
	case "=", "==":
		return fieldVal == targetVal
	case "!=":
		return fieldVal != targetVal
	default:
		return true
	}
}
