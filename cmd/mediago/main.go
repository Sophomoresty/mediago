package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

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

var version = "0.1.0"

var (
	formatSpec     string
	outputTemplate string
	cookieFile     string
	cookieBrowser  string
	listFormats    bool
	dumpJSON       bool
	simulate       bool
	writeInfoJSON  bool
	writeSubs      bool
	noOverwrites   bool
	concurrency    int
	listExtractors bool
	downloadAll    bool
	mergeOutputFmt string
	noProgress     bool
	proxy          string
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := &cobra.Command{
		Use:   "mediago [flags] URL [URL...]",
		Short: "Download media from 92 Chinese platforms",
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

	// Extractor listing (yt-dlp: --list-extractors)
	rootCmd.Flags().BoolVar(&listExtractors, "list-extractors", false, "list all supported sites and exit")

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
	if listExtractors {
		return printExtractors()
	}

	if len(args) == 0 {
		return cmd.Help()
	}

	if proxy != "" {
		if err := util.SetDefaultProxy(proxy); err != nil {
			return fmt.Errorf("invalid --proxy value: %w", err)
		}
	}

	failures := 0
	for _, url := range args {
		if err := processURL(ctx, url); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			errorf("%v", err)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d URLs failed", failures, len(args))
	}
	return nil
}

func processURL(ctx context.Context, url string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	ext, site, err := extractor.MatchWithSite(url)
	if err != nil {
		return fmt.Errorf("unsupported URL: %s\nUse --list-extractors to see supported sites.", url)
	}
	infof("Extracting: %s %s", site.Name, url)

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
		if !downloadAll {
			warnf("Downloading only the first item. Use --yes-playlist to download all.")
			if len(info.Entries) > 0 && info.Entries[0] != nil {
				infof("%s", info.Entries[0].Title)
				return downloadEntry(ctx, 0, 1, info.Entries[0])
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
		entryFailures := 0
		for i, entry := range info.Entries {
			if entry == nil {
				continue
			}
			if err := downloadEntry(ctx, i, len(info.Entries), entry); err != nil {
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
	return downloadOne(ctx, info)
}

func downloadEntry(ctx context.Context, itemIndex, totalItems int, info *extractor.MediaInfo) error {
	downloadf("%s", downloadItemMessage(itemIndex+1, totalItems, firstNonEmpty(info.Title, fmt.Sprintf("item-%d", itemIndex+1))))
	return downloadOneFn(ctx, info)
}

var downloadOneFn = downloadOne

func downloadOne(ctx context.Context, info *extractor.MediaInfo) error {
	if listFormats {
		return printFormats(info)
	}

	_, stream := download.SelectBestStream(info.Streams, formatSpec)
	if len(stream.URLs) == 0 && stream.Format == "" {
		return fmt.Errorf("no formats available: %s", info.Title)
	}

	outFilename := applyTemplate(outputTemplate, info, stream)

	engine := download.New(download.Opts{
		Concurrency:      concurrency,
		OutputDir:        outputDirFromTemplate(outFilename),
		Overwrite:        !noOverwrites,
		Retries:          3,
		NoProgress:       noProgress,
		Proxy:            proxy,
		Context:          ctx,
		MergeOutputFormat: mergeOutputFmt,
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
	if writeSubs {
		if subs, err := engine.DownloadSubtitles(info, outPath); err != nil {
			return fmt.Errorf("download subtitles: %w", err)
		} else {
			for _, sub := range subs {
				subtitlef("%s", sub)
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

	r := strings.NewReplacer(
		"%(title)s", info.Title,
		"%(ext)s", ext,
		"%(site)s", info.Site,
		"%(artist)s", info.Artist,
		"%(quality)s", stream.Quality,
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
