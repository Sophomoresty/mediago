package htknow

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"
)

func (x *htCtx) answerSource(c course, columnID, productID, productType, name string) source {
	detail, answerDetail, answerLog := x.answerDetail(c, columnID, productID, firstNonEmpty(productType, "9"))
	answerLog = x.ensureAnswerLog(productID, answerLog)
	tagDetail := x.answerTagDetail(productID, answerDetail, answerLog)
	numList := x.answerNumList(productID, answerLog)
	questions, answerLog := x.answerQuestionsWithFallback(productID, answerDetail, answerLog)
	answerHTML := buildAnswerHTML(name, detail, answerDetail, answerLog, tagDetail, numList, questions)
	return source{
		name:       firstNonEmpty(name, productID, "答题"),
		kind:       "答题HTML",
		answerHTML: answerHTML,
		extra: map[string]any{
			"answer_detail":     answerDetail,
			"answer_log_detail": answerLog,
			"answer_tag_detail": tagDetail,
			"answer_num_list":   numList,
			"answer_questions":  questions,
			"product_id":        productID,
			"product_type":      firstNonEmpty(productType, "9"),
			"column_id":         columnID,
		},
	}
}

func (x *htCtx) answerDetail(c course, columnID, productID, productType string) (map[string]any, map[string]any, map[string]any) {
	payload := map[string]any{
		"column_id":       columnID,
		"product_type":    firstNonEmpty(productType, "9"),
		"product_id":      productID,
		"app_name":        "pc_view",
		"version":         "v1",
		"custom_id":       x.customID,
		"user_id":         x.userID,
		"product_version": "v1",
	}
	if c.typ == "系列课" {
		payload["big_series_id"] = c.id
	}
	if c.mainProductID != "" {
		payload["main_product_id"] = c.mainProductID
	}
	root, err := x.postJSON(pcVideoInfoURL, payload)
	if err != nil {
		return map[string]any{}, map[string]any{}, map[string]any{}
	}
	result := mapAt(root, "result")
	detail := firstMap(mapAt(result, "article_detail"), mapAt(result, "detail"))
	answerDetail := firstMap(mapFrom(detail["answer_detail"]), mapFrom(result["answer_detail"]))
	answerLog := firstMap(mapFrom(detail["answer_log_detail"]), mapFrom(result["answer_log_detail"]))
	return detail, answerDetail, answerLog
}

func (x *htCtx) requestAnswerJSON(endpoint string, data map[string]any) (map[string]any, error) {
	payload := map[string]any{
		"version":         "v1",
		"app_name":        "pc_view",
		"user_id":         x.userID,
		"custom_id":       x.customID,
		"product_version": "v1",
	}
	for k, v := range data {
		payload[k] = v
	}
	return x.postJSON(endpoint, payload)
}

func (x *htCtx) ensureAnswerLog(productID string, answerLog map[string]any) map[string]any {
	if str(answerLog["id"]) != "" {
		return copyMap(answerLog)
	}
	root, err := x.requestAnswerJSON(answerCreatePaperURL, map[string]any{"product_id": productID})
	if err != nil || intVal(root["code"]) != 200 {
		return copyMap(answerLog)
	}
	result := mapFrom(root["result"])
	if log := mapFrom(result["answer_log"]); len(log) > 0 {
		return log
	}
	if id := firstNonEmpty(str(result["id"]), str(result["answer_log_id"])); id != "" {
		result["id"] = id
		return result
	}
	return copyMap(answerLog)
}

func (x *htCtx) answerTagDetail(productID string, answerDetail, answerLog map[string]any) map[string]any {
	questLibraryID := str(answerDetail["quest_library_id"])
	answerLogID := str(answerLog["id"])
	if productID == "" || questLibraryID == "" || answerLogID == "" {
		return map[string]any{}
	}
	root, err := x.requestAnswerJSON(answerTagURL, map[string]any{
		"quest_library_id": questLibraryID,
		"type":             firstNonEmpty(str(answerDetail["type"]), "3"),
		"answer_log":       answerLogID,
		"product_id":       productID,
	})
	if err != nil || intVal(root["code"]) != 200 {
		return map[string]any{}
	}
	return mapFrom(root["result"])
}

func (x *htCtx) answerNumList(productID string, answerLog map[string]any) []any {
	answerLogID := str(answerLog["id"])
	if productID == "" || answerLogID == "" {
		return nil
	}
	root, err := x.requestAnswerJSON(answerNumURL, map[string]any{
		"product_id": productID,
		"type":       1,
		"answer_log": answerLogID,
	})
	if err != nil || intVal(root["code"]) != 200 {
		return nil
	}
	return listAny(root["result"])
}

func (x *htCtx) answerQuestionsWithFallback(productID string, answerDetail, answerLog map[string]any) ([]map[string]any, map[string]any) {
	questions := x.answerQuestionList(productID, answerDetail, answerLog)
	if len(questions) > 0 || str(answerLog["id"]) != "" {
		return questions, answerLog
	}
	answerLog = x.ensureAnswerLog(productID, answerLog)
	return x.answerQuestionList(productID, answerDetail, answerLog), answerLog
}

func (x *htCtx) answerQuestionList(productID string, answerDetail, answerLog map[string]any) []map[string]any {
	answerLogID := str(answerLog["id"])
	if productID == "" || answerLogID == "" {
		return nil
	}
	tagsID := str(answerDetail["tags_id"])
	total := intFromAny(answerDetail["quest_total"])
	maxPages := 200
	if total > 0 {
		maxPages = maxInt(20, total/10+2)
	}

	var out []map[string]any
	seen := map[string]bool{}
	for page := 1; page <= maxPages; page++ {
		root, err := x.requestAnswerJSON(answerListURL, map[string]any{
			"product_id": productID,
			"quest_type": 1,
			"page":       page,
			"tags_id":    tagsID,
			"type":       1,
			"answer_log": answerLogID,
		})
		if err != nil || intVal(root["code"]) != 200 {
			break
		}
		result := root["result"]
		items := answerResultList(result)
		if len(items) == 0 {
			break
		}
		added := 0
		for _, item := range items {
			key := answerItemKey(item)
			if key == "" {
				key = fmt.Sprintf("page:%d:index:%d", page, added)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, item)
			added++
		}
		if added == 0 {
			break
		}
		if t := answerResultTotal(result); t > 0 {
			total = t
		}
		if total > 0 && len(out) >= total {
			break
		}
	}
	return out
}

func buildAnswerHTML(videoName string, detail, answerDetail, answerLog, tagDetail map[string]any, numList []any, questions []map[string]any) string {
	title := firstNonEmpty(videoName, str(detail["title"]), str(detail["name"]), "海豚知道答题")
	overview := renderAnswerOverview(answerDetail, answerLog, tagDetail, numList, questions)
	intro := renderAnswerIntro(detail)
	questionHTML := `<div class="empty">没有获取到题目列表，可能是账号权限或接口限制。</div>`
	if len(questions) > 0 {
		var parts []string
		for i, item := range questions {
			parts = append(parts, renderAnswerQuestion(item, i+1))
		}
		questionHTML = strings.Join(parts, "")
	}
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + escapeText(title) + `</title>
<style>
*{box-sizing:border-box}body{margin:0;background:#f6f7fb;color:#1f2533;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",Arial,sans-serif;line-height:1.7}.answer-page{max-width:1120px;margin:0 auto;padding:24px}.answer-header{background:linear-gradient(135deg,#3d61ff,#6b8cff);color:#fff;border-radius:18px;padding:26px 30px}.answer-header h1{margin:0;font-size:24px}.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:14px;margin:18px 0}.stat,.tag-panel,.number-panel,.intro,.question-card{background:#fff;border:1px solid #edf0f7;border-radius:16px;box-shadow:0 10px 28px rgba(31,37,51,.06)}.stat{padding:16px 18px}.stat span{display:block;color:#7c8597;font-size:13px}.stat strong{display:block;margin-top:4px;font-size:22px;color:#3d61ff}.tag-panel,.number-panel,.intro{padding:18px;margin-bottom:18px}.panel-title{font-weight:700;font-size:18px;margin-bottom:12px}.tags,.numbers{display:flex;flex-wrap:wrap;gap:10px}.tag{border-radius:999px;background:#f1f4ff;color:#3d61ff;padding:8px 12px}.num{display:inline-flex;width:34px;height:34px;border-radius:50%;align-items:center;justify-content:center;background:#f4f6fb;color:#6a7283;font-weight:600}.num.done{background:#e9f7ef;color:#17a45b}.intro{color:#434b5f}.question-card{padding:22px;margin-bottom:18px;page-break-inside:avoid}.question-head{display:flex;gap:10px;align-items:center;margin-bottom:14px}.question-index{font-weight:800;font-size:18px}.question-type{border-radius:999px;background:#eef2ff;color:#3d61ff;padding:3px 10px;font-size:13px}.question-title{font-size:17px;font-weight:650;margin-bottom:16px;word-break:break-word}.question-title img,.intro img{max-width:100%;height:auto}.options{display:flex;flex-direction:column;gap:10px;margin:14px 0}.option{display:flex;gap:12px;border:1px solid #edf0f7;background:#fafbff;border-radius:12px;padding:12px 14px}.option.correct{border-color:#67c23a;background:#f0f9eb}.option-key{flex:0 0 auto;min-width:26px;height:26px;border-radius:50%;display:inline-flex;align-items:center;justify-content:center;background:#e8ecf7;color:#3d61ff;font-weight:700}.option.correct .option-key{background:#67c23a;color:#fff}.answer-row{display:flex;gap:14px;align-items:flex-start;border-top:1px dashed #e1e6f0;margin-top:14px;padding-top:14px}.answer-row span{flex:0 0 72px;color:#7c8597}.answer-row strong{color:#17a45b}.answer-row.user strong{color:#3d61ff}.explanation{margin-top:16px;border-radius:12px;background:#fff8ec;border:1px solid #ffe2b8;padding:14px}.explanation-title{font-weight:700;color:#b76e00;margin-bottom:8px}.explanation-image,.question-image,.question-video,.question-audio{max-width:100%;margin-top:10px;border-radius:10px}.muted,.empty{color:#8992a6}pre{white-space:pre-wrap;word-break:break-word}@media print{body{background:#fff}.answer-page{padding:0}.answer-header,.stat,.tag-panel,.number-panel,.intro,.question-card{box-shadow:none}}
</style>
</head>
<body><div class="answer-page"><header class="answer-header"><h1>` + escapeText(title) + `</h1><p>海豚知道 · 答题离线版</p></header>` + overview + intro + `<main>` + questionHTML + `</main></div></body></html>`
}

func renderAnswerIntro(detail map[string]any) string {
	keys := []string{"pay_content", "free_content", "main_point", "oriented_people", "learn_result", "author_desc", "other_desc"}
	var parts []string
	for _, key := range keys {
		if text := str(detail[key]); text != "" {
			parts = append(parts, htmlBlock(text))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return `<section class="intro">` + strings.Join(parts, "") + `</section>`
}

func renderAnswerOverview(answerDetail, answerLog, tagDetail map[string]any, numList []any, questions []map[string]any) string {
	total := firstNonEmpty(str(answerDetail["quest_total"]), str(answerLog["quest_num"]), fmt.Sprint(len(questions)))
	learned := firstNonEmpty(str(answerLog["answer_quest"]), str(answerLog["learn"]), "0")
	last := firstNonEmpty(str(answerLog["last_num"]), str(answerLog["lastNum"]), "0")
	out := `<div class="stats"><div class="stat"><span>题目总数</span><strong>` + escapeText(total) + `</strong></div><div class="stat"><span>已学习</span><strong>` + escapeText(learned) + `</strong></div><div class="stat"><span>上次进度</span><strong>` + escapeText(last) + `</strong></div></div>`
	if tags := listAny(tagDetail["list"]); len(tags) > 0 {
		var parts []string
		for _, item := range tags {
			m := mapFrom(item)
			name := firstNonEmpty(str(m["tag_name"]), str(m["name"]), str(m["title"]))
			learn := firstNonEmpty(str(m["learn"]), str(m["percent"]), "0")
			if name != "" {
				parts = append(parts, `<span class="tag"><b>`+escapeText(name)+`</b><em>已学习 `+escapeText(learn)+`%</em></span>`)
			}
		}
		if len(parts) > 0 {
			out += `<div class="tag-panel"><div class="panel-title">专项练习</div><div class="tags">` + strings.Join(parts, "") + `</div></div>`
		}
	}
	if len(numList) > 0 {
		var parts []string
		for i, item := range numList {
			m := mapFrom(item)
			num := firstNonEmpty(str(m["sort"]), str(m["quest_num"]), str(m["sequence"]), fmt.Sprint(i+1))
			cls := "num"
			if truthy(m["is_pass"]) {
				cls = "num done"
			}
			parts = append(parts, `<span class="`+cls+`">`+escapeText(num)+`</span>`)
		}
		out += `<div class="number-panel"><div class="panel-title">答题卡</div><div class="numbers">` + strings.Join(parts, "") + `</div></div>`
	}
	return out
}

func renderAnswerQuestion(item map[string]any, index int) string {
	if len(item) == 0 {
		return `<section class="question-card"><pre></pre></section>`
	}
	num := firstNonEmpty(str(item["quest_number"]), str(item["quest_num"]), str(item["sequence"]), str(item["sort"]), fmt.Sprint(index))
	title := firstNonEmpty(str(item["title"]), str(item["question"]), str(item["quest_title"]), str(item["quest_stem"]), str(item["stem"]), str(item["content"]), str(item["name"]))
	if title == "" {
		title = `<span class="muted">暂无题干</span>`
	} else {
		title = htmlBlock(title)
	}
	var parts []string
	parts = append(parts, `<section class="question-card">`)
	parts = append(parts, `<div class="question-head"><span class="question-index">第`+escapeText(num)+`题</span><span class="question-type">`+escapeText(answerQuestionType(item))+`</span></div>`)
	parts = append(parts, `<div class="question-title">`+title+`</div>`)
	parts = append(parts, renderAnswerMedia(item))
	parts = append(parts, renderAnswerOptions(item))
	if ans := firstNonEmpty(str(item["correct_answers"]), str(item["correct_answer"]), str(item["answer"])); ans != "" {
		parts = append(parts, `<div class="answer-row"><span>正确答案</span><strong>`+escapeText(ans)+`</strong></div>`)
	}
	if ans := firstNonEmpty(str(item["user_answer"]), str(item["answer_text"])); ans != "" {
		parts = append(parts, `<div class="answer-row user"><span>我的答案</span><strong>`+escapeText(ans)+`</strong></div>`)
	}
	explain := firstNonEmpty(str(item["answer_explanation"]), str(item["analysis"]), str(item["explain"]), str(item["解析"]))
	explainImg := firstNonEmpty(str(item["answer_explanation_url"]), str(item["analysis_img"]))
	if explain != "" || explainImg != "" {
		parts = append(parts, `<div class="explanation"><div class="explanation-title">题目解析</div>`)
		if explain != "" {
			parts = append(parts, `<div class="explanation-content">`+htmlBlock(explain)+`</div>`)
		}
		if explainImg != "" {
			parts = append(parts, `<img class="explanation-image" src="`+escapeAttr(explainImg)+`">`)
		}
		parts = append(parts, `</div>`)
	}
	parts = append(parts, `</section>`)
	return strings.Join(parts, "")
}

func answerQuestionType(item map[string]any) string {
	typ := intFromAny(firstNonEmpty(str(item["type"]), str(item["quest_type"])))
	names := map[int]string{1: "单选题", 2: "多选题", 3: "判断题", 4: "填空题", 5: "简答题", 6: "主观题"}
	if name := names[typ]; name != "" {
		return name
	}
	return "题目"
}

func renderAnswerOptions(item map[string]any) string {
	options := listAny(item["options"])
	if len(options) == 0 {
		return ""
	}
	correct := answerStringSet(item["answer"], item["correct_answers"], item["correct_answer"])
	var parts []string
	for _, raw := range options {
		opt := mapFrom(raw)
		key := firstNonEmpty(str(opt["key"]), str(opt["option"]), str(opt["label"]), str(opt["value"]))
		content := firstNonEmpty(str(opt["content"]), str(opt["title"]), str(opt["name"]), str(opt["text"]), str(raw))
		cls := "option"
		if truthy(opt["is_pass"]) || correct[key] {
			cls = "option correct"
		}
		parts = append(parts, `<div class="`+cls+`"><span class="option-key">`+escapeText(key)+`</span><div class="option-content">`+htmlBlock(content)+`</div></div>`)
	}
	return `<div class="options">` + strings.Join(parts, "") + `</div>`
}

func renderAnswerMedia(item map[string]any) string {
	var parts []string
	if u := firstNonEmpty(str(item["question_img"]), str(item["image"]), str(item["img_url"]), str(item["picture"])); u != "" {
		parts = append(parts, `<img class="question-image" src="`+escapeAttr(u)+`">`)
	}
	if u := firstNonEmpty(str(item["video_url"]), str(item["video"]), str(item["answer_video_url"])); u != "" {
		parts = append(parts, `<video class="question-video" src="`+escapeAttr(u)+`" controls></video>`)
	}
	if u := firstNonEmpty(str(item["audio_url"]), str(item["audio"])); u != "" {
		parts = append(parts, `<audio class="question-audio" src="`+escapeAttr(u)+`" controls></audio>`)
	}
	return strings.Join(parts, "")
}

func answerResultList(v any) []map[string]any {
	if arr := listMap(v); len(arr) > 0 {
		return arr
	}
	m := mapFrom(v)
	for _, key := range []string{"list", "data", "records", "items", "rows", "quest_list", "question_list"} {
		if arr := listMap(m[key]); len(arr) > 0 {
			return arr
		}
	}
	if len(m) > 0 && answerItemKey(m) != "" {
		return []map[string]any{m}
	}
	return nil
}

func answerResultTotal(v any) int {
	m := mapFrom(v)
	for _, key := range []string{"total", "quest_total", "count", "total_count"} {
		if n := intFromAny(m[key]); n > 0 {
			return n
		}
	}
	return 0
}

func answerItemKey(item map[string]any) string {
	for _, key := range []string{"id", "quest_id", "question_id", "qid", "sort", "quest_num", "sequence"} {
		if v := str(item[key]); v != "" {
			return key + ":" + v
		}
	}
	return ""
}

func answerStringSet(values ...any) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		for _, item := range listAny(v) {
			s := str(item)
			if strings.Contains(s, ",") {
				for _, part := range strings.Split(s, ",") {
					if p := strings.TrimSpace(part); p != "" {
						out[p] = true
					}
				}
				continue
			}
			if s != "" {
				out[s] = true
			}
		}
	}
	return out
}

func htmlDataURL(s string) string {
	return "data:text/html;charset=utf-8," + url.PathEscape(s)
}

func htmlBlock(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") {
		return s
	}
	return escapeText(s)
}

func escapeText(v any) string { return html.EscapeString(str(v)) }
func escapeAttr(v any) string { return html.EscapeString(str(v)) }

func firstMap(values ...map[string]any) map[string]any {
	for _, v := range values {
		if len(v) > 0 {
			return v
		}
	}
	return map[string]any{}
}

func mapFrom(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func listMap(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m := mapFrom(item); len(m) > 0 {
			out = append(out, m)
		}
	}
	return out
}

func listAny(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []map[string]any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			out = append(out, item)
		}
		return out
	case nil:
		return nil
	default:
		if str(t) == "" {
			return nil
		}
		return []any{t}
	}
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	case float64:
		return t != 0
	case int:
		return t != 0
	}
	return false
}
