package dto

type CaptchaResponse struct {
	Key     string `json:"key"`
	Content string `json:"content"`
	FlowID  string `json:"flow_id"`
}

type UpstreamCaptchaResponse struct {
	Key     string `json:"key"`
	Content string `json:"content"`
}
