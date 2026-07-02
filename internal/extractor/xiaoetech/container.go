package xiaoetech

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/Sophomoresty/mediago/internal/util"
)

// checkTrainingAuth validates training camp cookie via the xe.training.teas
// auth endpoint (source: Xiaoetech_Base.__check_course_cookie). This is a
// best-effort check; failure is non-fatal.
func checkTrainingAuth(c *util.Client, jar http.CookieJar, ctx xetCtx) {
	if ctx.appID == "" {
		return
	}
	api := fmt.Sprintf(trainingAuthURL, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault))
	_, _ = c.GetString(api, headers(jar, referer(ctx)))
}

func expandContainerItem(c *util.Client, jar http.CookieJar, ctx xetCtx, it xetItem) []xetItem {
	typ := normType(firstNonEmpty(it.typ, ctx.typ))
	if !isContainerType(typ) || it.id == "" {
		return nil
	}
	out := []xetItem{}
	switch typ {
	case "train":
		out = append(out, trainChildren(c, jar, ctx, it)...)
		out = append(out, fileChildren(c, jar, ctx, it, "50", "25")...)
	case "clock":
		out = append(out, clockChildren(c, jar, ctx, it)...)
	case "member":
		out = append(out, columnChildren(c, jar, ctx, it, infoURL, pcInfoURL)...)
		if len(out) == 0 {
			out = append(out, columnChildren(c, jar, ctx, it, memberInfoURL, pcMemberInfoURL)...)
		}
		out = append(out, fileChildren(c, jar, ctx, it, "5")...)
	default:
		out = append(out, columnChildren(c, jar, ctx, it, infoURL, pcInfoURL)...)
		out = append(out, fileChildren(c, jar, ctx, it, resourceTypeNumber(typ, val(it.raw, "resource_type")))...)
	}
	return uniqueItems(out)
}

func isContainerType(typ string) bool {
	switch normType(typ) {
	case "column", "bigcolumn", "member", "ecourse", "train", "clock":
		return true
	default:
		return false
	}
}

func columnChildren(c *util.Client, jar http.CookieJar, ctx xetCtx, parent xetItem, h5Tpl, pcTpl string) []xetItem {
	out := []xetItem{}
	for page := 1; page <= 98; page++ {
		root := postXETJSON(c, jar, ctx, h5Tpl, pcTpl, map[string]string{
			"sort":       "desc",
			"page_size":  "100",
			"page_index": fmt.Sprint(page),
			"column_id":  parent.id,
		})
		list := listUnder(root["data"], "list")
		if len(list) == 0 {
			break
		}
		for _, m := range list {
			child := itemFromMap(m)
			if child.raw == nil {
				child.raw = m
			}
			child.appID = firstNonEmpty(child.appID, ctx.appID)
			child.userID = firstNonEmpty(child.userID, ctx.userID)
			child.typ = normType(firstNonEmpty(child.typ, val(m, "resource_type")))
			child.title = firstNonEmpty(child.title, val(m, "resource_title"), val(m, "title"), val(m, "name"))
			child.raw["_parent_id"] = parent.id
			if child.id != "" && child.typ != "" {
				out = append(out, child)
			}
		}
		if len(list) < 100 {
			break
		}
	}
	if len(out) > 1 {
		sort.SliceStable(out, func(i, j int) bool {
			a, b := val(out[i].raw, "start_at"), val(out[j].raw, "start_at")
			if a == b {
				return i < j
			}
			return a < b
		})
	}
	return out
}

func trainChildren(c *util.Client, jar http.CookieJar, ctx xetCtx, parent xetItem) []xetItem {
	// Check training auth (source: Xiaoetech_Base.__check_course_cookie via
	// xe.training.teas/wework/is_auth).
	checkTrainingAuth(c, jar, ctx)
	terms := postXETJSON(c, jar, ctx, termURL, pcTermURL, map[string]string{"term_id": parent.id})
	out := []xetItem{}
	for termIndex, term := range mapsFromAny(terms["data"]) {
		nodeID := firstNonEmpty(val(term, "id"), val(term, "node_id"))
		if nodeID == "" {
			continue
		}
		termTitle := firstNonEmpty(val(term, "title"), val(term, "name"), fmt.Sprintf("term_%d", termIndex+1))
		nodes := postXETJSON(c, jar, ctx, nodeURL, pcNodeURL, map[string]string{"term_id": parent.id, "node_id": nodeID})
		for _, m := range mapsFromAny(nodes["data"]) {
			child := itemFromMap(m)
			if child.raw == nil {
				child.raw = m
			}
			child.id = firstNonEmpty(child.id, val(m, "resource_id"), val(m, "id"))
			child.typ = normType(firstNonEmpty(child.typ, val(m, "resource_type"), val(m, "type")))
			title := firstNonEmpty(child.title, val(m, "resource_title"), val(m, "title"), val(m, "name"))
			child.title = cleanXETTitle(firstNonEmpty(termTitle, parent.title) + "--" + title)
			child.appID = firstNonEmpty(child.appID, ctx.appID)
			child.userID = firstNonEmpty(child.userID, ctx.userID)
			child.raw["_parent_id"] = parent.id
			if child.typ == "clock" {
				child.raw["_clock_in_train"] = true
			}
			if child.id != "" && child.typ != "" {
				out = append(out, child)
			}
		}
	}
	return out
}

func fileChildren(c *util.Client, jar http.CookieJar, ctx xetCtx, parent xetItem, typeCandidates ...string) []xetItem {
	out := []xetItem{}
	candidates := uniqueStrings(typeCandidates)
	if len(candidates) == 0 {
		candidates = []string{resourceTypeNumber(normType(parent.typ), val(parent.raw, "resource_type"))}
	}
	for _, rt := range candidates {
		if rt == "" {
			continue
		}
		root := postXETJSON(c, jar, ctx, fileURL, pcFileURL, map[string]string{"resource_type": rt, "resource_id": parent.id})
		for i, m := range mapsFromAny(root["data"]) {
			u := firstNonEmpty(val(m, "url"), val(m, "file_url"), val(m, "download_url"), val(m, "downloadUrl"), firstMediaURL(m))
			if u == "" {
				continue
			}
			raw := copyMap(m)
			raw["url"] = normalizeURL(u)
			raw["resource_type"] = rt
			raw["_parent_id"] = parent.id
			title := firstNonEmpty(val(m, "title"), val(m, "file_name"), val(m, "name"), fmt.Sprintf("%s_file_%d", parent.id, i+1))
			out = append(out, xetItem{
				id:     firstNonEmpty(val(m, "id"), val(m, "file_id"), normalizeURL(u)),
				title:  cleanXETTitle(title),
				typ:    "file",
				appID:  ctx.appID,
				userID: ctx.userID,
				raw:    raw,
			})
		}
	}
	return uniqueItems(out)
}

func clockChildren(c *util.Client, jar http.CookieJar, ctx xetCtx, parent xetItem) []xetItem {
	if val(parent.raw, "_clock_in_train") == "true" {
		return trainClockChildren(c, jar, ctx, parent)
	}
	if parent.title == "" || parent.title == parent.id {
		parent.title = clockTitle(c, jar, ctx, parent.id)
	}
	out := []xetItem{}
	root := postJSONDetail(c, jar, ctx, clockTreeURL, map[string]any{
		"app_id":      ctx.appID,
		"activity_id": parent.id,
	})
	chapters := listUnder(root["data"], "list")
	if len(chapters) == 0 {
		chapters = mapsFromAny(root["data"])
	}
	for idx, chapter := range chapters {
		taskID := firstNonEmpty(val(chapter, "chapter_id"), val(chapter, "task_id"), val(chapter, "id"))
		if taskID == "" {
			continue
		}
		title := firstNonEmpty(val(chapter, "chapter_title"), val(chapter, "task_title"), val(chapter, "title"), fmt.Sprintf("clock_%d", idx+1))
		out = append(out, clockChapterChildren(c, jar, ctx, parent, taskID, title)...)
	}
	return uniqueItems(out)
}

func clockTitle(c *util.Client, jar http.CookieJar, ctx xetCtx, activityID string) string {
	root := postJSONDetail(c, jar, ctx, clockIntroURL, map[string]any{"activity_id": activityID})
	return firstNonEmpty(val(root["data"], "title"), val(root["data"], "activity_title"), val(root["data"], "name"))
}

func clockChapterChildren(c *util.Client, jar http.CookieJar, ctx xetCtx, parent xetItem, taskID, chapterTitle string) []xetItem {
	root := postJSONDetail(c, jar, ctx, clockChapterURL, map[string]any{
		"task_id":     taskID,
		"activity_id": parent.id,
		"app_id":      ctx.appID,
	})
	return clockContentItems(parent, firstNonEmpty(chapterTitle, parent.title), taskID, root["data"])
}

func trainClockChildren(c *util.Client, jar http.CookieJar, ctx xetCtx, parent xetItem) []xetItem {
	api := trainClockURL
	payload := map[string]any{"theme_id": parent.id, "app_version": "h5"}
	if ctx.pc {
		api = trainPCClockURL
		payload = map[string]any{"work_id": parent.id, "app_version": "h5"}
	}
	root := postJSONDetail(c, jar, ctx, api, payload)
	return clockContentItems(parent, parent.title, parent.id, root["data"])
}

func clockContentItems(parent xetItem, titlePrefix, sourceID string, data any) []xetItem {
	items := []xetItem{}
	content := clockOrgContent(data)
	for idx, m := range content {
		typ := val(m, "type")
		raw := copyMap(m)
		raw["_parent_id"] = parent.id
		raw["_clock_id"] = sourceID
		switch typ {
		case "3":
			u := normalizeURL(firstNonEmpty(val(m, "video_url"), val(m, "videoUrl"), firstMediaURL(m)))
			if u == "" {
				continue
			}
			raw["url"] = u
			title := firstNonEmpty(val(m, "video_name"), fmt.Sprintf("clock_video_%d", idx+1))
			if titlePrefix != "" {
				title = titlePrefix + "--" + strings.TrimSuffix(title, ".mp4")
			}
			items = append(items, xetItem{
				id:     firstNonEmpty(val(m, "video_id"), val(m, "id"), u),
				title:  cleanXETTitle(title),
				typ:    "video",
				appID:  parent.appID,
				userID: parent.userID,
				raw:    raw,
			})
		case "5":
			u := normalizeURL(firstNonEmpty(val(m, "audio_url"), val(m, "audioUrl"), firstMediaURL(m)))
			if u == "" {
				continue
			}
			raw["url"] = u
			title := firstNonEmpty(val(m, "audio_name"), fmt.Sprintf("clock_audio_%d", idx+1))
			if titlePrefix != "" {
				title = titlePrefix + "--" + strings.TrimSuffix(title, ".mp3")
			}
			items = append(items, xetItem{
				id:     firstNonEmpty(val(m, "audio_id"), val(m, "id"), u),
				title:  cleanXETTitle(title),
				typ:    "audio",
				appID:  parent.appID,
				userID: parent.userID,
				raw:    raw,
			})
		case "1":
			html := firstNonEmpty(val(m, "descrb"), val(m, "description"), val(m, "content"))
			if html == "" {
				continue
			}
			raw["url"] = "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
			title := firstNonEmpty(fmt.Sprintf("clock_text_%d", idx+1))
			if titlePrefix != "" {
				title = titlePrefix + "--图文"
			}
			items = append(items, xetItem{
				id:     firstNonEmpty(val(m, "id"), sourceID, parent.id) + fmt.Sprintf("_text_%d", idx+1),
				title:  cleanXETTitle(title),
				typ:    "text",
				appID:  parent.appID,
				userID: parent.userID,
				raw:    raw,
			})
		}
	}
	return uniqueItems(items)
}

func clockOrgContent(v any) []map[string]any {
	for _, m := range mapsUnder(v) {
		if raw, ok := m["org_content"]; ok {
			if out := decodeClockOrgContent(raw); len(out) > 0 {
				return out
			}
		}
	}
	return decodeClockOrgContent(v)
}

func decodeClockOrgContent(v any) []map[string]any {
	switch t := v.(type) {
	case []any:
		out := []map[string]any{}
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return t
	case map[string]any:
		if list := listUnder(t, "list"); len(list) > 0 {
			return list
		}
		return []map[string]any{t}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		var decoded any
		if json.Unmarshal([]byte(s), &decoded) != nil {
			return nil
		}
		return decodeClockOrgContent(decoded)
	default:
		return nil
	}
}

func postXETJSON(c *util.Client, jar http.CookieJar, ctx xetCtx, h5Tpl, pcTpl string, plainForm map[string]string) map[string]any {
	api := ""
	form := plainForm
	if ctx.pc && pcTpl != "" && ctx.domain != "" {
		api = fmt.Sprintf(pcTpl, ctx.domain)
	} else if ctx.appID != "" && h5Tpl != "" {
		api = fmt.Sprintf(h5Tpl, ctx.appID, firstNonEmpty(ctx.xetDomain, xetDomainDefault))
		form = wrapBizData(plainForm)
	}
	if api == "" {
		return nil
	}
	body, err := c.PostForm(api, form, headers(jar, referer(ctx)))
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return nil
	}
	return root
}

func wrapBizData(form map[string]string) map[string]string {
	out := make(map[string]string, len(form))
	for k, v := range form {
		if strings.HasPrefix(k, "bizData[") {
			out[k] = v
			continue
		}
		out["bizData["+k+"]"] = v
	}
	return out
}

func resourceTypeNumber(typ, fallback string) string {
	if n := strings.TrimSpace(fallback); n != "" {
		if normType(n) != n {
			return n
		}
	}
	switch normType(typ) {
	case "text":
		return "1"
	case "audio":
		return "2"
	case "video":
		return "3"
	case "live":
		return "4"
	case "clock":
		return "16"
	case "member":
		return "5"
	case "column":
		return "6"
	case "bigcolumn":
		return "8"
	case "book":
		return "20"
	case "train":
		return "25"
	case "ecourse":
		return "50"
	case "document", "file":
		return "51"
	default:
		return fallback
	}
}

func mapsFromAny(v any) []map[string]any {
	switch t := v.(type) {
	case []any:
		out := []map[string]any{}
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		if list := listUnder(t, "list"); len(list) > 0 {
			return list
		}
		out := []map[string]any{}
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		if len(out) > 0 {
			return out
		}
		return []map[string]any{t}
	default:
		return nil
	}
}

func copyMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func uniqueItems(in []xetItem) []xetItem {
	out := []xetItem{}
	seen := map[string]bool{}
	for _, it := range in {
		key := firstNonEmpty(it.id, firstMediaURL(it.raw)) + "|" + normType(it.typ)
		if key == "|" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out
}

func uniqueStrings(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func cleanXETTitle(s string) string {
	return regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`).ReplaceAllString(strings.TrimSpace(s), "_")
}
