package unipus

import (
	"github.com/Sophomoresty/mediago/internal/extractor/shared"
	"github.com/Sophomoresty/mediago/internal/util"
)

func prepareSource(c *util.Client, rawURL, ref, cookie string) (string, string, map[string]any, map[string]any) {
	format := pickFormat(rawURL)
	extra := map[string]any{"source_url": rawURL}
	var streamExtra map[string]any
	if format != "m3u8" {
		return rawURL, format, nil, extra
	}
	result := shared.PrepareQiqiuyunM3U8(c, rawURL, shared.QiqiuyunM3U8Options{
		Referer: ref,
		Cookie:  cookie,
		Version: 1,
		Mode:    1,
	})
	if result.Text != "" {
		extra["m3u8_text"] = result.Text
		extra["source_type"] = "m3u8_text"
		if result.SourceURL != "" {
			extra["m3u8_url"] = result.SourceURL
		}
		if result.Meta != nil {
			extra["m3u8_meta"] = result.Meta
			streamExtra = map[string]any{"m3u8_meta": result.Meta}
		}
		return result.URL, "m3u8", streamExtra, extra
	}
	extra["source_type"] = "m3u8_url"
	return rawURL, "m3u8", nil, extra
}

func prepareQiqiuyunM3U8(c *util.Client, rawURL, ref, cookie string) (string, map[string]any) {
	result := shared.PrepareQiqiuyunM3U8(c, rawURL, shared.QiqiuyunM3U8Options{
		Referer: ref,
		Cookie:  cookie,
		Version: 1,
		Mode:    1,
	})
	return result.Text, result.Meta
}

func decodeQiqiuyunKey(content []byte, version int) []byte {
	return shared.DecodeQiqiuyunKey(content, version)
}
