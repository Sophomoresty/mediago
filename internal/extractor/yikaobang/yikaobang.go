// Package yikaobang implements an extractor for yikaobang.com.cn (医考帮) courses.
//
// The decompiled Python source (Mooc/Courses/Yikaobang/Yikaobang_Course.pyc)
// itself documents that the upstream tool does NOT have reliable course/play
// API samples for this site:
//
//	"医考帮当前仍缺少可靠的课程/播放接口样本，已保留统一结构骨架，暂不提供伪实现。"
//	(Yikaobang lacks reliable course/play API samples; only the skeleton
//	 is kept. No pseudo-implementation.)
//
// We honor that — register the URL pattern and probe https://www.yikaobang.com.cn/
// home page for liveness only, returning blocked rather than fabricating.
package yikaobang

import (
	"fmt"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const urlHome = "https://www.yikaobang.com.cn/"

var patterns = []string{`(?:[\w-]+\.)?yikaobang\.com\.cn/`}

func init() {
	extractor.Register(&Yikaobang{}, extractor.SiteInfo{Name: "Yikaobang", URL: "yikaobang.com.cn", NeedAuth: true})
}

type Yikaobang struct{}

func (y *Yikaobang) Patterns() []string { return patterns }

func (y *Yikaobang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	return nil, fmt.Errorf("yikaobang has no documented course/play API sample in the upstream Python source; not implemented (home: %s)", urlHome)
}
