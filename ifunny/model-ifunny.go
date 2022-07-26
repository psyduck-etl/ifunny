package ifunny

type IDHaver interface {
	GetID() string
}

type Content struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Tags       []string `json:"tags"`
	DateCreate int64    `json:"date_create"`
	Creator    User     `json:"creator"`
}

func (it Content) GetID() string {
	return it.ID
}

type User struct {
	ID   string `json:"id"`
	Nick string `json:"nick"`
	Num  struct {
		Subscribers   int `json:"subscribers"`
		Subscriptions int `json:"subscriptions"`
	} `json:"num"`
}

func (it User) GetID() string {
	return it.ID
}
