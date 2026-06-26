// Package yikaobang implements the documented blocked state for yikaobang.com.cn.
package yikaobang

import (
	"fmt"

	"github.com/Sophomoresty/mediago/internal/extractor"
	"github.com/Sophomoresty/mediago/internal/util"
)

const (
	refererURL = "https://www.yikaobang.com.cn/"
	homeURL    = "https://www.yikaobang.com.cn/"
)

var patterns = []string{`(?:[\w-]+\.)?yikaobang\.com\.cn/`}

func init() {
	extractor.Register(&Yikaobang{}, extractor.SiteInfo{Name: "Yikaobang", URL: "yikaobang.com.cn", NeedAuth: true})
}

type Yikaobang struct{}

func (y *Yikaobang) Patterns() []string { return patterns }

func (y *Yikaobang) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	c := util.NewClient()
	if opts != nil && opts.Cookies != nil {
		c.SetCookieJar(opts.Cookies)
	}
	_, err := c.GetString(homeURL, map[string]string{"Referer": refererURL})
	if err != nil {
		return nil, fmt.Errorf("yikaobang home probe: %w", err)
	}
	return nil, fmt.Errorf("blocked: needs upstream API samples (sandbox has only home URL and explicit no pseudo-implementation marker)")
}
