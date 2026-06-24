package kuke

import (
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/nichuanfang/medigo/internal/extractor"
	"github.com/nichuanfang/medigo/internal/extractor/shared"
	"github.com/nichuanfang/medigo/internal/util"
)

func kukeBuildItems(detail map[string]any, gid, subTitle string) []kukeItem {
	roots := records(detail["goodsCourseNodeList"])
	var items []kukeItem
	counters := map[string]int{"video": 0, "file": 0}
	var leaf func(map[string]any, int, int, string, string)
	leaf = func(node map[string]any, ch, sec int, chTitle, secTitle string) {
		title := strings.TrimSpace(firstText(node["title"]))
		if title == "" {
			return
		}
		nt := intOf(node["nodeType"])
		if nt == 3 && firstText(node["polyvVideoId"]) != "" {
			counters["video"]++
			name := fmt.Sprintf("[%d.%d]--%s", ch, counters["video"], title)
			if secTitle != "" {
				name = fmt.Sprintf("[%d.%d.%d]--%s", ch, sec, counters["video"], title)
			}
			items = append(items, kukeItem{Kind: "video", Name: name, Chapter: firstText(subTitle, secTitle, chTitle), NodeID: firstText(node["id"]), GoodsMasterID: gid, PolyvVideoID: firstText(node["polyvVideoId"]), Duration: intOf(node["videoDuration"])})
		}
		fileURL := firstText(node["resourceUrl"], node["fileUrl"], node["attachUrl"], node["sourceUrl"])
		if nt != 1 && nt != 3 && fileURL != "" {
			counters["file"]++
			name := fmt.Sprintf("(%d.%d)--%s", ch, counters["file"], title)
			if secTitle != "" {
				name = fmt.Sprintf("(%d.%d.%d)--%s", ch, sec, counters["file"], title)
			}
			items = append(items, kukeItem{Kind: "file", Name: name, Chapter: firstText(subTitle, secTitle, chTitle), FileURL: fileURL, FileFmt: firstText(node["extension"], node["fileExt"])})
		}
	}
	var walk func([]map[string]any, int, int, int, string, string)
	walk = func(nodes []map[string]any, depth, ch, sec int, chTitle, secTitle string) {
		for i, node := range nodes {
			title, children := strings.TrimSpace(firstText(node["title"])), records(node["children"])
			if intOf(node["nodeType"]) == 1 {
				if depth == 0 {
					ct := fmt.Sprintf("{%d}--%s", i+1, title)
					if len(children) > 0 {
						walk(children, 1, i+1, 1, ct, "")
					}
				} else {
					st := fmt.Sprintf("{%d}--%s", i+1, title)
					if len(children) > 0 {
						walk(children, depth+1, ch, i+1, chTitle, st)
					} else {
						leaf(node, ch, i+1, chTitle, st)
					}
				}
				continue
			}
			if chTitle == "" {
				chTitle = "{1}--未分类"
			}
			leaf(node, ch, sec, chTitle, secTitle)
			if len(children) > 0 {
				walk(children, depth, ch, sec, chTitle, secTitle)
			}
		}
	}
	walk(roots, 0, 1, 1, "", "")
	return items
}

func kukeBuildVideoEntry(c *util.Client, headers map[string]string, item kukeItem) (*extractor.MediaInfo, error) {
	playSafe, vid := kukeFetchPolyvNodeInfo(c, headers, item)
	vid = firstText(vid, item.PolyvVideoID)
	if vid == "" {
		return nil, fmt.Errorf("kuke: empty polyv video id")
	}
	secureVid := kukeSecureVID(vid)
	sec, err := shared.PolyvResolveSecure(c, secureVid, headers)
	manifest, token := "", playSafe
	if err == nil {
		token = firstText(token, sec.Data.Playsafe.Token)
		manifest, err = shared.PolyvPickBestManifest(sec)
	}
	if err != nil || manifest == "" {
		manifest, token, err = kukeFetchPolyvJS(c, secureVid, headers, token)
	}
	if err != nil || manifest == "" {
		return nil, fmt.Errorf("kuke polyv %s: %w", secureVid, err)
	}
	stream := extractor.Stream{Quality: "best", URLs: []string{manifest}, Format: "m3u8", NeedMerge: true, Headers: map[string]string{"Referer": "https://www.kuke99.com/"}}
	if strings.HasPrefix(manifest, "http") && token != "" {
		if text, e := c.GetString(manifest, headers); e == nil && strings.HasPrefix(strings.TrimSpace(text), "#EXTM3U") {
			if rewritten, e := shared.PolyvRewriteM3U8Keys(c, text, token, "https://www.kuke99.com/"); e == nil {
				stream.URLs = []string{rewritten}
			}
		}
	}
	return &extractor.MediaInfo{Site: "kuke", Title: item.Name, Streams: map[string]extractor.Stream{"best": stream}, Extra: map[string]any{"node_id": item.NodeID, "polyv_video_id": item.PolyvVideoID, "goods_master_id": item.GoodsMasterID, "chapter": item.Chapter, "duration": item.Duration}}, nil
}

func kukeFetchPolyvNodeInfo(c *util.Client, headers map[string]string, item kukeItem) (playSafe, videoID string) {
	data, err := kukeSignedPost(c, urlPolyvNodeInfo, map[string]any{"goodsType": 1, "videoId": "", "orgId": kukePolyvOrgID, "goodsMasterId": item.GoodsMasterID, "nodeId": item.NodeID}, headers, headers["kk-token"])
	if err != nil {
		return "", ""
	}
	var node kukeNodeInfoData
	_ = json.Unmarshal(data, &node)
	return firstText(node.PlaySafe, node.Token), node.VideoID
}

func kukeFetchPolyvJS(c *util.Client, vid string, headers map[string]string, token string) (string, string, error) {
	body, err := c.GetString(fmt.Sprintf(urlPolyvSecureJS, url.PathEscape(vid)), headers)
	if err != nil {
		return "", token, err
	}
	var out struct {
		HLS      []string `json:"hls"`
		Title    string   `json:"title"`
		PlaySafe string   `json:"playSafe"`
		Token    string   `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return "", token, err
	}
	if len(out.HLS) == 0 {
		return "", firstText(token, out.PlaySafe, out.Token), fmt.Errorf("empty hls")
	}
	return normalizeM3U8(out.HLS[len(out.HLS)-1]), firstText(token, out.PlaySafe, out.Token), nil
}

func kukeBuildFileEntry(item kukeItem) *extractor.MediaInfo {
	if item.FileURL == "" || item.Name == "" {
		return nil
	}
	fmtName := strings.TrimPrefix(strings.ToLower(item.FileFmt), ".")
	if fmtName == "" {
		fmtName = "dat"
	}
	return &extractor.MediaInfo{Site: "kuke", Title: item.Name, Streams: map[string]extractor.Stream{"best": {Quality: "best", URLs: []string{item.FileURL}, Format: fmtName, Headers: map[string]string{"Referer": "https://www.kuke99.com/"}}}, Extra: map[string]any{"chapter": item.Chapter}}
}

func kukeParseIDs(raw string) (cid, buyID string) {
	if m := kukeCourseRe.FindStringSubmatch(raw); len(m) > 0 {
		cid = firstText(m[1], m[2], m[3], m[4])
		buyID = firstText(m[5])
	}
	if u, err := url.Parse(raw); err == nil {
		q := u.Query()
		cid = firstText(q.Get("goodsMasterId"), q.Get("courseId"), cid)
		buyID = firstText(q.Get("userBuyUnitGoodsId"), buyID)
		if strings.Contains(u.Path, "/learn-center/live-detail") {
			cid = firstText(q.Get("id"), cid)
		}
	}
	return
}

func kukeIsSvip(course map[string]any) bool {
	return intOf(mapAny(course["content"])["goodsType"]) == 5
}

func kukeTitleFromDetail(d map[string]any) string {
	return firstText(d["goodsName"], d["courseName"], d["title"], d["goodsTitle"], deepText(d, "goodsInfo", "goodsName"), deepText(d, "goodsInfo", "courseName"), deepText(d, "goodsBaseInfo", "goodsName"), deepText(d, "goodsMasterInfo", "goodsName"), deepText(d, "goods", "goodsName"))
}

func kukeSecureVID(videoID string) string {
	base := strings.Split(videoID, "_")[0]
	if base == "" || videoID == "" {
		return videoID
	}
	return base + "_" + videoID[:1]
}

func normalizeM3U8(s string) string {
	if strings.HasPrefix(s, "http") || strings.HasPrefix(strings.TrimSpace(s), "#EXTM3U") {
		return s
	}
	if strings.HasPrefix(s, "/") {
		return shared.PolyvHLSPlayBase + s
	}
	return shared.PolyvHLSPlayBase + "/" + s
}

func kukeCookieString(jar http.CookieJar) string {
	if jar == nil {
		return ""
	}
	seen, parts := map[string]bool{}, []string{}
	for _, raw := range []string{"https://www.kuke99.com/", "https://kuke99.com/"} {
		u, _ := url.Parse(raw)
		for _, ck := range jar.Cookies(u) {
			if !seen[ck.Name] {
				seen[ck.Name] = true
				parts = append(parts, ck.Name+"="+ck.Value)
			}
		}
	}
	return strings.Join(parts, "; ")
}

func kukeCookieValue(cookie, name string) string {
	for _, part := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == name {
			return kv[1]
		}
	}
	return ""
}

func kukeRandHex(n int) string {
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}

func mapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func records(v any) []map[string]any {
	switch x := v.(type) {
	case []map[string]any:
		return x
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, it := range x {
			if m, ok := it.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		for _, k := range []string{"goodsCourseNodeList", "courseList", "list", "items", "children", "data"} {
			if r := records(x[k]); len(r) > 0 {
				return r
			}
		}
	}
	return nil
}

func firstText(vals ...any) string {
	for _, v := range vals {
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

func scalarString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case int, int64, float64, float32, bool:
		return fmt.Sprint(x), true
	default:
		return "", false
	}
}

func intOf(v any) int {
	s, _ := scalarString(v)
	if s == "" {
		s = firstText(v)
	}
	f, _ := strconv.ParseFloat(s, 64)
	return int(f)
}

func deepText(m map[string]any, keys ...string) string {
	cur := any(m)
	for _, k := range keys {
		mm := mapAny(cur)
		cur = mm[k]
	}
	return firstText(cur)
}
