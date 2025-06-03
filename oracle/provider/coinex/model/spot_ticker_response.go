package model

type SpotTickerResponse struct {
	Code int `json:"code"`
	Data []struct {
		Market     string `json:"market"`
		Last       string `json:"last"`
		Open       string `json:"open"`
		Close      string `json:"close"`
		High       string `json:"high"`
		Low        string `json:"low"`
		Volume     string `json:"volume"`
		VolumeSell string `json:"volume_sell"`
		VolumeBuy  string `json:"volume_buy"`
		Value      string `json:"value"`
		Period     int    `json:"period"`
	} `json:"data"`
	Message string `json:"message"`
}
