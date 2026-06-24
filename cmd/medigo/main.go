package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nichuanfang/medigo/internal/cookie"
	"github.com/nichuanfang/medigo/internal/download"
	"github.com/nichuanfang/medigo/internal/extractor"

	_ "github.com/nichuanfang/medigo/internal/extractor/ahu"
	_ "github.com/nichuanfang/medigo/internal/extractor/aishangke"
	_ "github.com/nichuanfang/medigo/internal/extractor/baijiayunxiao"
	_ "github.com/nichuanfang/medigo/internal/extractor/bilibili"
	_ "github.com/nichuanfang/medigo/internal/extractor/caixuetang"
	_ "github.com/nichuanfang/medigo/internal/extractor/cctalk"
	_ "github.com/nichuanfang/medigo/internal/extractor/cctv"
	_ "github.com/nichuanfang/medigo/internal/extractor/chaoge"
	_ "github.com/nichuanfang/medigo/internal/extractor/chaoxing"
	_ "github.com/nichuanfang/medigo/internal/extractor/ckjr"
	_ "github.com/nichuanfang/medigo/internal/extractor/classin"
	_ "github.com/nichuanfang/medigo/internal/extractor/cnmooc"
	_ "github.com/nichuanfang/medigo/internal/extractor/cto51"
	_ "github.com/nichuanfang/medigo/internal/extractor/dingtalk"
	_ "github.com/nichuanfang/medigo/internal/extractor/dongao"
	_ "github.com/nichuanfang/medigo/internal/extractor/douyin"
	_ "github.com/nichuanfang/medigo/internal/extractor/duanshu"
	_ "github.com/nichuanfang/medigo/internal/extractor/enetedu"
	_ "github.com/nichuanfang/medigo/internal/extractor/eoffcn"
	_ "github.com/nichuanfang/medigo/internal/extractor/feishu"
	_ "github.com/nichuanfang/medigo/internal/extractor/fenbi"
	_ "github.com/nichuanfang/medigo/internal/extractor/gaodun"
	_ "github.com/nichuanfang/medigo/internal/extractor/gaotu"
	_ "github.com/nichuanfang/medigo/internal/extractor/gongxuanwang"
	_ "github.com/nichuanfang/medigo/internal/extractor/haiyangknow"
	_ "github.com/nichuanfang/medigo/internal/extractor/haozaixian"
	_ "github.com/nichuanfang/medigo/internal/extractor/houda"
	_ "github.com/nichuanfang/medigo/internal/extractor/houdu"
	_ "github.com/nichuanfang/medigo/internal/extractor/hqwx"
	_ "github.com/nichuanfang/medigo/internal/extractor/htknow"
	_ "github.com/nichuanfang/medigo/internal/extractor/huatu"
	_ "github.com/nichuanfang/medigo/internal/extractor/huke88"
	_ "github.com/nichuanfang/medigo/internal/extractor/icourse163"
	_ "github.com/nichuanfang/medigo/internal/extractor/icourses"
	_ "github.com/nichuanfang/medigo/internal/extractor/icve"
	_ "github.com/nichuanfang/medigo/internal/extractor/imooc"
	_ "github.com/nichuanfang/medigo/internal/extractor/itbaizhan"
	_ "github.com/nichuanfang/medigo/internal/extractor/jianshe99"
	_ "github.com/nichuanfang/medigo/internal/extractor/jinbangshidai"
	_ "github.com/nichuanfang/medigo/internal/extractor/jingtongxue"
	_ "github.com/nichuanfang/medigo/internal/extractor/kaimingzhixue"
	_ "github.com/nichuanfang/medigo/internal/extractor/kaoyanvip"
	_ "github.com/nichuanfang/medigo/internal/extractor/keqq"
	_ "github.com/nichuanfang/medigo/internal/extractor/koolearn"
	_ "github.com/nichuanfang/medigo/internal/extractor/kuke"
	_ "github.com/nichuanfang/medigo/internal/extractor/ledu"
	_ "github.com/nichuanfang/medigo/internal/extractor/lexueyun"
	_ "github.com/nichuanfang/medigo/internal/extractor/lizhiweike"
	_ "github.com/nichuanfang/medigo/internal/extractor/luffycity"
	_ "github.com/nichuanfang/medigo/internal/extractor/magedu"
	_ "github.com/nichuanfang/medigo/internal/extractor/mashibing"
	_ "github.com/nichuanfang/medigo/internal/extractor/mddclass"
	_ "github.com/nichuanfang/medigo/internal/extractor/med66"
	_ "github.com/nichuanfang/medigo/internal/extractor/meeting"
	_ "github.com/nichuanfang/medigo/internal/extractor/minshi"
	_ "github.com/nichuanfang/medigo/internal/extractor/nmkjxy"
	_ "github.com/nichuanfang/medigo/internal/extractor/open163"
	_ "github.com/nichuanfang/medigo/internal/extractor/orangevip"
	_ "github.com/nichuanfang/medigo/internal/extractor/plaso"
	_ "github.com/nichuanfang/medigo/internal/extractor/qihang"
	_ "github.com/nichuanfang/medigo/internal/extractor/qlchat"
	_ "github.com/nichuanfang/medigo/internal/extractor/renrenjiang"
	_ "github.com/nichuanfang/medigo/internal/extractor/sanjieke"
	_ "github.com/nichuanfang/medigo/internal/extractor/shanxiang"
	_ "github.com/nichuanfang/medigo/internal/extractor/sier"
	_ "github.com/nichuanfang/medigo/internal/extractor/sites"
	_ "github.com/nichuanfang/medigo/internal/extractor/smartedu"
	_ "github.com/nichuanfang/medigo/internal/extractor/speiyou"
	_ "github.com/nichuanfang/medigo/internal/extractor/tmooc"
	_ "github.com/nichuanfang/medigo/internal/extractor/unipus"
	_ "github.com/nichuanfang/medigo/internal/extractor/wallstreets"
	_ "github.com/nichuanfang/medigo/internal/extractor/wangxiao"
	_ "github.com/nichuanfang/medigo/internal/extractor/wangxiao233"
	_ "github.com/nichuanfang/medigo/internal/extractor/wendao"
	_ "github.com/nichuanfang/medigo/internal/extractor/wowtiku"
	_ "github.com/nichuanfang/medigo/internal/extractor/xiaoeapp"
	_ "github.com/nichuanfang/medigo/internal/extractor/xiaoetech"
	_ "github.com/nichuanfang/medigo/internal/extractor/xiwang"
	_ "github.com/nichuanfang/medigo/internal/extractor/xsteach"
	_ "github.com/nichuanfang/medigo/internal/extractor/xueersi"
	_ "github.com/nichuanfang/medigo/internal/extractor/xuelang"
	_ "github.com/nichuanfang/medigo/internal/extractor/xuetang"
	_ "github.com/nichuanfang/medigo/internal/extractor/yangcong"
	_ "github.com/nichuanfang/medigo/internal/extractor/yikaobang"
	_ "github.com/nichuanfang/medigo/internal/extractor/yixiaoerguo"
	_ "github.com/nichuanfang/medigo/internal/extractor/yizhiknow"
	_ "github.com/nichuanfang/medigo/internal/extractor/youdao"
	_ "github.com/nichuanfang/medigo/internal/extractor/youyuan"
	_ "github.com/nichuanfang/medigo/internal/extractor/youzan"
	_ "github.com/nichuanfang/medigo/internal/extractor/zhaozhao"
	_ "github.com/nichuanfang/medigo/internal/extractor/zhengbao"
	_ "github.com/nichuanfang/medigo/internal/extractor/zhihuishu"
	_ "github.com/nichuanfang/medigo/internal/extractor/zlketang"
)

var version = "0.1.0"

var (
	formatSpec     string
	outputTemplate string
	cookieFile     string
	cookieBrowser  string
	listFormats    bool
	dumpJSON       bool
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
	rootCmd := &cobra.Command{
		Use:   "medigo [flags] URL [URL...]",
		Short: "Download media from 92 Chinese platforms",
		Long: `MediGo - download videos from Chinese educational and media platforms.
Similar to yt-dlp but focused on Chinese internet platforms.`,
		RunE:              runMain,
		Args:              cobra.ArbitraryArgs,
		DisableAutoGenTag: true,
		SilenceUsage:      true,
	}

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
			fmt.Printf("medigo %s\n", version)
		},
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runMain(cmd *cobra.Command, args []string) error {
	if listExtractors {
		return printExtractors()
	}

	if len(args) == 0 {
		return cmd.Help()
	}

	for _, url := range args {
		if err := processURL(url); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		}
	}
	return nil
}

func processURL(url string) error {
	ext, err := extractor.Match(url)
	if err != nil {
		return fmt.Errorf("unsupported URL: %s\nUse --list-extractors to see supported sites.", url)
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

	if dumpJSON {
		return printJSON(info)
	}

	if info.IsPlaylist() {
		fmt.Printf("[info] playlist: %s (%d items)\n", info.Title, len(info.Entries))
		if listFormats {
			fmt.Println("[info] use a single-item URL with -F to inspect formats")
			return nil
		}
		for i, entry := range info.Entries {
			if entry == nil {
				continue
			}
			if err := downloadOne(entry); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR [%d/%d %s]: %v\n", i+1, len(info.Entries), entry.Title, err)
			}
		}
		return nil
	}

	return downloadOne(info)
}

func downloadOne(info *extractor.MediaInfo) error {
	if listFormats {
		return printFormats(info)
	}

	_, stream := download.SelectBestStream(info.Streams, formatSpec)
	if len(stream.URLs) == 0 && stream.Format == "" {
		return fmt.Errorf("no formats available: %s", info.Title)
	}

	outFilename := applyTemplate(outputTemplate, info, stream)

	engine := download.New(download.Opts{
		Concurrency: concurrency,
		OutputDir:   outputDirFromTemplate(outFilename),
		Overwrite:   !noOverwrites,
		Retries:     3,
	})

	info.Title = baseFromTemplate(outFilename)

	outPath, err := engine.Download(info, stream)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	if writeInfoJSON {
		writeInfoJSONFile(outPath, info)
	}
	if writeSubs {
		if subs, err := engine.DownloadSubtitles(info, outPath); err != nil {
			return fmt.Errorf("download subtitles: %w", err)
		} else {
			for _, sub := range subs {
				fmt.Printf("[subtitle] %s\n", sub)
			}
		}
	}

	fmt.Printf("[download] %s\n", outPath)
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

func printFormats(info *extractor.MediaInfo) error {
	fmt.Printf("[info] Available formats for: %s\n", info.Title)
	fmt.Printf("%-10s %-10s %-8s %-12s\n", "ID", "QUALITY", "FORMAT", "SIZE")
	fmt.Println(strings.Repeat("-", 44))
	for id, s := range info.Streams {
		size := "unknown"
		if s.Size > 0 {
			if s.Size > 1024*1024 {
				size = fmt.Sprintf("%.1fMiB", float64(s.Size)/(1024*1024))
			} else {
				size = fmt.Sprintf("%.1fKiB", float64(s.Size)/1024)
			}
		}
		fmt.Printf("%-10s %-10s %-8s %-12s\n", id, s.Quality, s.Format, size)
	}
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
