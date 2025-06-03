package model

type DepthResponse struct {
	Code int `json:"code"`
	Data struct {
		Depth struct {
			Asks      [][2]string `json:"asks"`
			Bids      [][2]string `json:"bids"`
			Checksum  int         `json:"checksum"`
			Last      string      `json:"last"`
			UpdatedAt int64       `json:"updated_at"`
		} `json:"depth"`
		IsFull bool   `json:"is_full"`
		Market string `json:"market"`
	} `json:"data"`
	Message string `json:"message"`
}
