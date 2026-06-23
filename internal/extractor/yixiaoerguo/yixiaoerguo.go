// Package yixiaoerguo implements an extractor for biguo.cn / qianxuecloud.com
// (1对2 一笑而过 — biguo + qianxue cloud playback).
//
// API endpoints from decompiled Mooc/Courses/Yixiaoerguo/:
//   https://www.biguo.cn/my/course
//   https://bjs1.qianxuecloud.com/recordquery
//   https://bjs1.qianxuecloud.com/recordquerybackup
//   https://bjs1.qianxuecloud.com/recordquerymu
//   https://vodquerys1.qianxuecloud.com/playbackquerywebhls
//   https://vodquerydatas1.qianxuecloud.com/dataplaybackqueryh5
package yixiaoerguo

import (
	"fmt"
	"regexp"

	"github.com/nichuanfang/medigo/internal/extractor"
)

const (
	urlMyCourse      = "https://www.biguo.cn/my/course"
	urlRecordQuery   = "https://bjs1.qianxuecloud.com/recordquery"
	urlRecordBackup  = "https://bjs1.qianxuecloud.com/recordquerybackup"
	urlRecordMu      = "https://bjs1.qianxuecloud.com/recordquerymu"
	urlPlaybackHLS   = "https://vodquerys1.qianxuecloud.com/playbackquerywebhls"
	urlDataPlayback  = "https://vodquerydatas1.qianxuecloud.com/dataplaybackqueryh5"
)

var patterns = []string{`(?:[\w-]+\.)?(?:biguo|qianxuecloud)\.(?:cn|com)/`}

func init() {
	extractor.Register(&Yixiaoerguo{}, extractor.SiteInfo{Name: "Yixiaoerguo", URL: "biguo.cn", NeedAuth: true})
}

type Yixiaoerguo struct{}

func (y *Yixiaoerguo) Patterns() []string { return patterns }

var idRe = regexp.MustCompile(`/course/(\w+)|recordId=(\w+)`)

func (y *Yixiaoerguo) Extract(rawURL string, opts *extractor.ExtractOpts) (*extractor.MediaInfo, error) {
	if opts == nil || opts.Cookies == nil {
		return nil, fmt.Errorf("yixiaoerguo requires login cookies")
	}
	if !idRe.MatchString(rawURL) {
		return nil, fmt.Errorf("cannot parse yixiaoerguo course/record id from URL")
	}
	return nil, fmt.Errorf("yixiaoerguo qianxuecloud playback chain not yet implemented")
}
