# MediGo

A CLI tool for downloading videos from **94 Chinese educational and media platforms**. Single binary, cross-platform. yt-dlp style interface, aligned with decompiled Python source logic.

## Install

```bash
go install github.com/nichuanfang/medigo/cmd/medigo@latest
```

### Build from source

```bash
git clone https://github.com/nichuanfang/medigo.git
cd medigo
make build
```

### Requirements

- **ffmpeg** (optional): Required for HLS/DASH streams.

## Usage

```bash
# Download a video
medigo https://www.bilibili.com/video/BV1GJ411x7h7

# With cookies (for paid/locked content)
medigo --cookies cookies.txt https://www.icourse163.org/course/ZJICM-1449623161

# Read cookies from browser
medigo --cookies-from-browser chrome https://ke.fenbi.com/win/v3/lectures/123

# List available formats
medigo -F https://www.bilibili.com/video/BV1GJ411x7h7

# Dump info as JSON (no download)
medigo -j https://tv.cctv.com/2024/01/01/VIDE1234567890.shtml

# Download entire course/playlist
medigo --yes-playlist --cookies cookies.txt "https://www.icourse163.org/course/ZJICM-1449623161"

# Custom output template
medigo -o "%(site)s/%(title)s.%(ext)s" URL

# With proxy
medigo --proxy socks5://127.0.0.1:1080 URL

# Simulate (show info without downloading)
medigo --simulate URL

# Write subtitle files
medigo --write-subs --cookies cookies.txt URL

# Concurrent fragment downloads
medigo -N 20 URL
```

## Flags

| Flag | Description |
|------|-------------|
| `-f, --format` | Format selection (best/worst/1080p/720p/480p) |
| `-o, --output` | Output filename template (`%(title)s.%(ext)s`) |
| `--cookies` | Netscape cookie file path |
| `--cookies-from-browser` | Read cookies from browser (chrome/edge/firefox) |
| `-F, --list-formats` | List available formats and exit |
| `-j, --dump-json` | Dump info JSON to stdout and exit |
| `--write-info-json` | Write .info.json alongside download |
| `--no-overwrites` | Do not overwrite existing files |
| `-N, --concurrent-fragments` | Number of concurrent fragment downloads (default 10) |
| `--yes-playlist` | Download all items in a playlist/course |
| `--merge-output-format` | Merge output container (mp4/mkv/webm) |
| `--no-progress` | Suppress progress bar |
| `--proxy` | HTTP/SOCKS proxy URL |
| `--simulate` | Show extracted info without downloading |
| `--write-subs` | Write subtitle files alongside download |
| `--list-extractors` | List all supported sites |

## Supported Platforms (94)

Bilibili, Douyin, CCTV, Chaoxing, iCourse163, Xuetang, Zhihuishu, imooc, DingTalk, Feishu, Fenbi, Huatu, Gaodun, Jianshe99, Med66, Hqwx, Wangxiao, Wangxiao233, Dongao, Eoffcn, Kaoyanvip, Yikaobang, Xueersi, Yangcong, Yixiaoerguo, Speiyou, Gaotu, Koolearn, Cto51, Huke88, Magedu, Itbaizhan, Luffycity, Tmooc, Mashibing, Xiaoetech, Xiaoeapp, Youzan, Qlchat, Lizhiweike, Renrenjiang, Sanjieke, Duanshu, Lexueyun, Meeting, Classin, CCTalk, Baijiayunxiao, Keqq, Smartedu, Icourses, Icve, Cnmooc, Open163, Unipus, Ahu, Nmkjxy, Aishangke, Caixuetang, Chaoge, Ckjr, Enetedu, Gongxuanwang, Haiyangknow, Haozaixian, Houda, Houdu, Htknow, Jinbangshidai, Jingtongxue, Kaimingzhixue, Kuke, Ledu, Mddclass, Minshi, Orangevip, Plaso, Qihang, Shanxiang, Sier, Wallstreets, Wendao, Wowtiku, Xiwang, Xsteach, Xuelang, Yizhiknow, Youdao, Youyuan, Zhaozhao, Zhengbao, Zlketang

## Architecture

```
medigo URL
  → extractor.Match(url)           # URL regex → select extractor
  → extractor.Extract(url, opts)   # Call API chain → MediaInfo
  → download.SelectBestStream()    # -f format selection
  → engine.Download(info, stream)  # HLS/DASH/direct download
```

- `internal/extractor/<site>/` — per-site extractors (92 packages)
- `internal/extractor/shared/` — shared platform helpers (csslcloud, polyv, bokecc, baijiayun, aliyun)
- `internal/download/` — download engine (HLS, DASH, direct, concurrent segments)
- `internal/cookie/` — Netscape + browser cookie support
- `internal/util/` — HTTP client, crypto (AES/RSA/MD5), filename sanitization

## Development

```bash
# Build
go build ./...

# Run all tests
go test ./...

# Verify extractor alignment (no stubs)
python3 scripts/verify_full_alignment.py

# Run E2E tests
bash scripts/e2e_test.sh

# Generate golden file test scaffolding
python3 scripts/gen_golden_tests.py
```

## License

MIT
