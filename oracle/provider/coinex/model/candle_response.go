package model

type CandleResponse struct {
	Code    int                  `json:"code"`
	Data    []DataCandleResponse `json:"data"`
	Message string               `json:"message"`
}

type DataCandleResponse struct {
	Close     string `json:"close"`
	CreatedAt int64  `json:"created_at"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Market    string `json:"market"`
	Open      string `json:"open"`
	Value     string `json:"value"`
	Volume    string `json:"volume"`
}
