package ifunny

import (
	"encoding/json"

	"io"
	"net/http"
)

const API_ROOT = "https://api.ifunny.mobi/v4"

func getFeedPage(config *IFunnyConfig, nextPage string) FeedPage {
	url := config.APIRoot + "/feeds/" + config.Feed

	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}

	query := request.URL.Query()
	query.Add("next", nextPage)
	request.URL.RawQuery = query.Encode()

	request.Header.Add("authorization", "Bearer "+config.BearerToken)
	request.Header.Add("user-agent", config.UserAgent)
	request.Header.Add("ifunny-project-id", "ifunny")
	request.Header.Add("accept", "video/mp4, image/jpeg")

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		panic(err)
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	page := new(FeedPageResponse)
	json.Unmarshal(bodyBytes, page)

	return page.Data.Content
}
