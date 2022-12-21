package main

type User struct {
	ID   string `json:"id"`
	Nick string `json:"nick"`
	Num  struct {
		Subscribers   int `json:"subscribers"`
		Subscriptions int `json:"subscriptions"`
	} `json:"num"`
}

type UserWrap struct {
	Data User `json:"data"`
}
