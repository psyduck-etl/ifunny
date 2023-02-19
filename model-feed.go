package main

type FeedPaging struct {
	Cursors struct {
		Prev string `json:"prev"`
		Next string `json:"next"`
	} `json:"cursors"`
}

type FeedPage struct {
	Items  []Content  `json:"items"`
	Paging FeedPaging `json:"paging"`
}

type FeedPageResponse struct {
	Data struct {
		Content FeedPage `json:"content"`
	} `json:"data"`
}
