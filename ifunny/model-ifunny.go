package ifunny

type Content struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Tags       []string `json:"tags"`
	DateCreate int64    `json:"date_create"`
	Creator    User     `json:"creator"`
}

type User struct {
	ID   string `json:"id"`
	Nick string `json:"nick"`
}

type Identity struct {
	ID string `json:"id"`
}
