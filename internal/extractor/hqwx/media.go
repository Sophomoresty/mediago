package hqwx

import (
	"fmt"
	"strings"
)

func pickMediaURL(mediaInfos map[string]any) string {
	for _, key := range []string{"fhd_m3u8", "hd_m3u8", "sd_m3u8", "fhdUrl", "hdUrl", "sdUrl", "url"} {
		if s := str(mediaInfos[key]); s != "" {
			return s
		}
	}
	return ""
}

func pickVideoURL(resourceInfo map[string]any) string {
	for _, key := range []string{"hdurl", "mdurl", "sdurl", "hdUrl", "mdUrl", "sdUrl", "hd_url", "md_url", "sd_url"} {
		if s := str(resourceInfo[key]); s != "" {
			return s
		}
	}
	if s := pickMediaURL(asMap(resourceInfo["mediaInfos"])); s != "" {
		return s
	}
	return firstNonEmpty(str(resourceInfo["downloadUrl"]), str(resourceInfo["download_url"]))
}

func isVideoResourceInfo(item map[string]any) bool {
	return pickVideoURL(item) != ""
}

func pickSubtitleResID(items ...map[string]any) string {
	for _, item := range items {
		if len(item) == 0 {
			continue
		}
		for _, key := range []string{"resId", "resourceVideoId", "videoResId", "resource_id", "resourceId"} {
			if s := intString(item[key]); s != "" {
				return s
			}
		}
		if isVideoResourceInfo(item) {
			if s := intString(item["relationId"]); s != "" {
				return s
			}
			if s := intString(item["id"]); s != "" {
				return s
			}
		}
	}
	return ""
}

func isVideoTask(item map[string]any) bool {
	name := strings.ToLower(str(item["objName"]))
	for _, bad := range []string{"测评", "测试", "考试", "测验", "作业", "练习", "exam", "test", "quiz", "考卷"} {
		if strings.Contains(name, bad) {
			return false
		}
	}
	if str(item["resourceId"]) != "" {
		return true
	}
	live := asMap(item["resourceLive"])
	return str(live["playbackResIds"]) != ""
}

func isVideoLesson(lesson map[string]any) bool {
	if str(lesson["relationType"]) == "test_paper" {
		return false
	}
	if str(lesson["hdUrl"]) != "" {
		return true
	}
	return len(listMaps(asMap(lesson["liveDetail"])["videoInfos"])) > 0
}

func makeMaterialItem(lesson map[string]any, prefix string) *hqwxItem {
	fileURL := firstNonEmpty(str(lesson["materialDownloadUrl"]), str(lesson["materialUrl"]))
	fileName := firstNonEmpty(str(lesson["materialFileName"]), str(lesson["materialName"]))
	for _, key := range []string{"materialFile", "materialInfo", "additionFile"} {
		child := asMap(lesson[key])
		if len(child) == 0 {
			continue
		}
		fileURL = firstNonEmpty(fileURL, str(child["url"]), str(child["downloadUrl"]))
		fileName = firstNonEmpty(fileName, str(child["name"]), str(child["fileName"]), str(child["title"]))
	}
	html := asMap(lesson["htmlFileResourceDto"])
	if len(html) > 0 {
		fileURL = firstNonEmpty(fileURL, str(html["htmlResourceUrl"]))
		fileName = firstNonEmpty(fileName, str(html["htmlResourceName"]), str(lesson["name"]))
	}
	if fileURL == "" {
		return nil
	}
	fileName = firstNonEmpty(fileName, str(lesson["name"]), "material")
	return &hqwxItem{Kind: "file", Name: cleanName(fmt.Sprintf("%s--%s", prefix, fileName)), URL: fileURL, Raw: lesson}
}

func appendMaterialItem(items *[]hqwxItem, lesson map[string]any, prefix string, seen map[string]bool) bool {
	item := makeMaterialItem(lesson, prefix)
	if item == nil || seen[item.URL] {
		return false
	}
	seen[item.URL] = true
	*items = append(*items, *item)
	return true
}

func appendVideoItem(items *[]hqwxItem, name, url string, raw map[string]any, extra ...map[string]any) {
	if name == "" {
		name = "hqwx_video"
	}
	item := hqwxItem{Kind: "video", Name: cleanName(name), URL: url, Raw: raw, SubtitleResID: pickSubtitleResID(append([]map[string]any{raw}, extra...)...)}
	if item.URL == "" {
		item.ResourceID = firstNonEmpty(str(raw["resource_id"]), str(raw["resourceId"]), str(raw["relationId"]))
		item.PlaybackID = firstNonEmpty(str(raw["playback_id"]), str(raw["playbackResIds"]))
		live := asMap(raw["resourceLive"])
		item.PlaybackID = firstNonEmpty(item.PlaybackID, str(live["playbackResIds"]))
	}
	item.LessonID = str(raw["id"])
	*items = append(*items, item)
}
