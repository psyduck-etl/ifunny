package main

type Content struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Tags       []string `json:"tags"`
	DateCreate int64    `json:"date_create"`
	Creator    User     `json:"creator"`
}

type ContentWrap struct {
	Data Content `json:"data"`
}
