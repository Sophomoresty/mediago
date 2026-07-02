<p align="center">
  <img src="assets/banner.png" alt="MediaGo" width="600">
</p>

<p align="center">
  <strong>Media downloader for Chinese internet. 107 sites, single binary, written in Go.</strong>
</p>

<p align="center">
  <a href="#install">Install</a> •
  <a href="#usage">Usage</a> •
  <a href="#supported-platforms">Platforms</a> •
  <a href="#contributing">Contributing</a>
</p>

<p align="center">
  <a href="https://linux.do"><img src="https://img.shields.io/badge/LINUX%20DO-Community-blue?style=flat-square" alt="LINUX DO Community"></a>
  <a href="https://github.com/Sophomoresty/mediago/releases"><img src="https://img.shields.io/github/v/release/Sophomoresty/mediago?style=flat-square" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Unlicense-green?style=flat-square" alt="License"></a>
</p>

---

## Install

### Download binary (recommended)

Grab the latest release for your platform:

```bash
# Linux
curl -L https://github.com/Sophomoresty/mediago/releases/latest/download/mediago_linux_amd64.tar.gz | tar xz
sudo mv mediago_linux_amd64 /usr/local/bin/mediago

# macOS (Apple Silicon)
curl -L https://github.com/Sophomoresty/mediago/releases/latest/download/mediago_macos_arm64.tar.gz | tar xz
sudo mv mediago_macos_arm64 /usr/local/bin/mediago

# Windows — download .zip from releases page
```

### Build from source

```bash
git clone https://github.com/Sophomoresty/mediago.git
cd mediago && make build
```

### Optional dependencies

- **ffmpeg** — required for HLS/DASH streams and format merging

## Usage

### Quick start

```bash
# Free content (no login needed): Douyin, Bilibili, CCTV, Smartedu
mediago "https://www.douyin.com/video/7444926556498498867"
mediago "https://www.bilibili.com/video/BV1GJ411x7h7"

# Paid/premium content: export cookies then download
mediago --cookies-from-browser chrome "https://www.icourse163.org/learn/kaopei-1472545161?tid=1474145441"

# Download entire course/playlist
mediago --yes-playlist --cookies cookies.txt "https://www.icourse163.org/learn/kaopei-1472545161?tid=1474145441"

# Just peek (no download)
mediago --simulate URL
mediago -F URL          # list formats
mediago -j URL          # dump JSON info
```

### Getting cookies

Most paid platforms require login cookies. Two options:

```bash
# Option 1: auto-read from browser (easiest)
mediago --cookies-from-browser chrome URL
mediago --cookies-from-browser edge URL
mediago --cookies-from-browser firefox URL

# Option 2: export Netscape cookie file manually
# Install "Get cookies.txt LOCALLY" browser extension, export for the target site,
# then pass the file:
mediago --cookies ~/cookies.txt URL
```

### Common examples

```bash
# Download a video
mediago https://www.bilibili.com/video/BV1GJ411x7h7

# With cookies (for paid/locked content)
mediago --cookies cookies.txt URL

# Read cookies from browser
mediago --cookies-from-browser chrome URL

# List available formats
mediago -F URL

# Dump info as JSON (no download)
mediago -j URL

# Simulate (show info without downloading)
mediago --simulate URL

# Download entire course/playlist
mediago --yes-playlist --cookies cookies.txt URL

# Custom output template
mediago -o "%(site)s/%(title)s.%(ext)s" URL

# With proxy
mediago --proxy socks5://127.0.0.1:1080 URL

# Concurrent fragment downloads
mediago -N 20 URL
```

## Options

```
General:
  -h, --help                      show help
      --version                   show version
      --list-extractors           list all supported sites
      --simulate                  show extracted info without downloading

Download:
  -f, --format STRING             format selection (best/worst/1080p/720p/480p)
  -o, --output STRING             output filename template (default "%(title)s.%(ext)s")
  -N, --concurrent-fragments INT  concurrent fragment downloads (default 10)
      --no-overwrites             skip existing files
      --yes-playlist              download all items in a playlist/course
      --merge-output-format STR   output container for merged streams (mp4/mkv/webm)
      --no-progress               suppress progress bar
      --proxy STRING              HTTP/SOCKS5 proxy URL

Authentication:
      --cookies STRING            Netscape cookie file path
      --cookies-from-browser STR  read cookies from browser (chrome/edge/firefox)

Output:
  -F, --list-formats              list available formats and exit
  -j, --dump-json                 dump info JSON to stdout
      --write-info-json           write .info.json file alongside download
      --write-subs                write subtitle files alongside download
```

## Output template

The `-o` flag supports these variables:

| Variable | Description |
|----------|-------------|
| `%(title)s` | Video/course title |
| `%(ext)s` | File extension |
| `%(site)s` | Site name |
| `%(artist)s` | Uploader/author |
| `%(quality)s` | Selected quality |

## Supported platforms

<details>
<summary><strong>107 extractors</strong> (click to expand)</summary>

Bilibili (video/cheese/gongfang/bangumi), Douyin, CCTV, Chaoxing, iCourse163 (mooc/app/youdao/textbook), Xuetang, Zhihuishu (course/live/school/smart), iMOOC, DingTalk, Feishu, Fenbi, Huatu, Gaodun, Jianshe99, Med66, Hqwx, Wangxiao, Wangxiao233, Dongao, Eoffcn, Kaoyanvip, Yikaobang, Xueersi, Yangcong, Yixiaoerguo, Speiyou, Gaotu, Koolearn, Cto51, Huke88, Magedu, Itbaizhan, Luffycity, Tmooc, Mashibing, Xiaoetech, Xiaoeapp, Youzan, Qlchat, Lizhiweike, Renrenjiang, Sanjieke, Duanshu, Lexueyun, Meeting, Classin, CCTalk, Baijiayunxiao, Keqq, Smartedu, Icourses, ICVE (ai/mooc/course/profession/v2/qun/weike/material), Cnmooc, Open163, Unipus, Ahu, Nmkjxy, Aishangke, Caixuetang, Chaoge, Ckjr, Enetedu, Gongxuanwang, Haiyangknow, Haozaixian, Houda, Houdu, Htknow, Jinbangshidai, Jingtongxue, Kaimingzhixue, Kuke, Ledu, Mddclass, Minshi, Orangevip, Plaso, Qihang, Shanxiang, Sier, Wallstreets, Wendao, Wowtiku, Xiwang, Xsteach, Xuelang, Yizhiknow, Youdao, Youyuan, Zhaozhao, Zhengbao, Zlketang

</details>

## Architecture

```
mediago URL
  → extractor.Match(url)           # URL pattern → select extractor
  → extractor.Extract(url, opts)   # API chain → MediaInfo
  → download.SelectBestStream()    # format selection
  → engine.Download(info, stream)  # HLS/DASH/direct with retry + cancel
```

- `internal/extractor/<site>/` — per-site extractors
- `internal/extractor/shared/` — platform helpers (CSSLCloud, Polyv, BokeCC, Baijiayun, Aliyun VOD)
- `internal/download/` — download engine (HLS master+variant, DASH mux, concurrent segments, AES-128)
- `internal/cookie/` — Netscape file + browser cookie extraction
- `internal/util/` — HTTP client with retry, crypto (AES/RSA/MD5/SHA1)

## Development

```bash
make build          # build binary
make test           # run tests
go vet ./...        # static analysis

# Verify source alignment
python3 scripts/verify_source_alignment.py
```

## Contributing

Pull requests welcome. Please ensure `go build ./...`, `go vet ./...`, and `go test ./...` pass before submitting.

## Roadmap

Development is tracked via [GitHub Issues](https://github.com/Sophomoresty/mediago/issues). Key milestones:

- **v0.2** — End-to-end testing for all 107 sites, release binaries, yt-dlp CLI parity (output templates, archive, format selection)
- **v0.3** — AliyunVoDEncryption full chain, whiteboard/canvas replay rendering, post-processing (subtitle embed, audio extract)
- **v0.4** — New platforms (Douyin paid, Kuaishou, Xiaohongshu, WeChat Channels), config file support

## Known issues

- Some sites require login cookies to access paid/premium content — use `--cookies` or `--cookies-from-browser`
- Rate limiting on some platforms may cause intermittent failures — retry or use `--proxy`

## Disclaimer

This project is released under [The Unlicense](LICENSE) into the public domain and is intended **for educational and research purposes only**. Users must comply with applicable laws and the terms of service of respective platforms. This tool should only be used to download content that you have legitimate access to or have purchased. The developers assume no liability for any misuse of this software.

## License

[The Unlicense](LICENSE) — released into the public domain.

---

## Acknowledgements

Development agent capabilities powered by [GenericAgent](https://github.com/lsdefine/GenericAgent).

### 🚩 Links

[![GenericAgent](https://img.shields.io/badge/Agent_Framework-GenericAgent-orange?style=for-the-badge&logo=github)](https://github.com/lsdefine/GenericAgent)
[![LinuxDo](https://img.shields.io/badge/Community-LinuxDo-blue?style=for-the-badge)](https://linux.do/)
