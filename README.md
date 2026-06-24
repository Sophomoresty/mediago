# MediGo

A CLI tool for downloading videos from 92 Chinese educational and media platforms. Single binary, cross-platform.

## Install

Download the latest binary from [Releases](https://github.com/nichuanfang/medigo/releases), or build from source:

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

- **ffmpeg** (optional): Required for HLS/DASH streams. Install via your package manager.

## Usage

```bash
# Download a video
medigo https://www.bilibili.com/video/BV1GJ411x7h7

# Specify quality
medigo -q 1080p https://www.bilibili.com/video/BV1GJ411x7h7

# Use cookies from browser
medigo --cookies-from-browser chrome https://mooc1.chaoxing.com/course/123

# Use cookie file (Netscape format)
medigo --cookies cookies.txt https://www.icourse163.org/course/ZJU-93001

# List streams without downloading
medigo --list https://www.bilibili.com/video/BV1GJ411x7h7

# Output as JSON
medigo --json https://v.douyin.com/CeiJFhAo/

# Download all episodes
medigo --all https://www.bilibili.com/cheese/play/ss123

# Specify output directory
medigo -o ./downloads https://www.douyin.com/video/123456

# List supported sites
medigo sites
```

### Flags

| Flag | Description |
|------|-------------|
| `-q, --quality` | Preferred quality (best/1080p/720p/480p) |
| `-o, --output` | Output directory |
| `-c, --concurrency` | Download concurrency (default: 10) |
| `--cookies` | Netscape cookie file path |
| `--cookies-from-browser` | Read cookies from browser (chrome/edge/firefox) |
| `--list` | List available streams |
| `--all` | Download all chapters/episodes |
| `--json` | Output media info as JSON |
| `--write-subs` | Write subtitle files alongside the downloaded media |

## Supported Sites (92)

### No Auth Required
| Site | Domain |
|------|--------|
| Bilibili | bilibili.com |
| Douyin | douyin.com |
| CCTV | tv.cctv.com |
| Smartedu | smartedu.cn |
| Icourses | icourses.cn |
| Open163 | open.163.com |

### Auth Required (cookie needed)
| Site | Domain |
|------|--------|
| Chaoxing | chaoxing.com |
| icourse163 | icourse163.org |
| Xuetang | xuetangx.com |
| Zhihuishu | zhihuishu.com |
| imooc | imooc.com |
| DingTalk | dingtalk.com |
| Feishu | feishu.cn |
| Fenbi | fenbi.com |
| Huatu | huatu.com |
| Gaodun | gaodun.com |
| 51CTO | 51cto.com |
| Huke88 | huke88.com |
| Xueersi | xueersi.com |
| Koolearn | koolearn.com |
| Ke.qq | ke.qq.com |
| ... and 70+ more |

Run `medigo sites` for the full list.

## Architecture

```
medigo/
├── cmd/medigo/          # CLI entry point (cobra)
├── internal/
│   ├── config/          # Configuration (~/.config/medigo/)
│   ├── cookie/          # Cookie handling (file + browser)
│   ├── download/        # Download engine (direct/HLS/DASH)
│   ├── extractor/       # Extractor interface + registry
│   │   ├── bilibili/    # Bilibili (video/cheese/bangumi)
│   │   ├── douyin/      # Douyin (cookie-less, ttwid)
│   │   ├── cctv/        # CCTV (cntv API)
│   │   ├── chaoxing/    # Chaoxing (ananas API)
│   │   └── sites/       # 80+ generic extractors
│   └── util/            # HTTP client, crypto, filename
├── scripts/             # E2E tests
├── Makefile             # Cross-compilation
└── go.mod
```

## Cross-platform Build

```bash
make build-all
# Produces:
#   dist/medigo.exe     (Windows)
#   dist/medigo-linux   (Linux)
#   dist/medigo-mac     (macOS ARM64)
```

## License

MIT

---

Made with Go. Inspired by [lux](https://github.com/iawia002/lux).

[![Built with Go](https://img.shields.io/badge/Built%20with-Go-00ADD8?logo=go)](https://go.dev)
[![Linux.do](https://img.shields.io/badge/Linux.do-Community-blue)](https://linux.do)
