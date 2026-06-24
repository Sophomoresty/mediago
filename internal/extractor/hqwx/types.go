package hqwx

type listResp struct {
	Success bool `json:"success"`
	Code    *int `json:"code"`
	Status  struct {
		Code *int `json:"code"`
	} `json:"status"`
	Data struct {
		DataList []map[string]any `json:"dataList"`
	} `json:"data"`
}

type arrayResp struct {
	Success bool             `json:"success"`
	Code    *int             `json:"code"`
	Status  responseStatus   `json:"status"`
	Data    []map[string]any `json:"data"`
}

type objectResp struct {
	Success bool           `json:"success"`
	Code    *int           `json:"code"`
	Status  responseStatus `json:"status"`
	Data    map[string]any `json:"data"`
}

type responseStatus struct {
	Code *int `json:"code"`
}

type hqwxItem struct {
	Kind          string
	Name          string
	URL           string
	ResourceID    string
	PlaybackID    string
	SubtitleResID string
	LessonID      string
	Raw           map[string]any
}
