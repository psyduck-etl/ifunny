package ifunny

import (
	"encoding/json"

	"io"
	"net/http"
)

func mustIFunnyRequest(config *IFunnyConfig, method, path string, body io.Reader) *http.Request {
	request, err := http.NewRequest(method, config.APIRoot+path, body)
	if err != nil {
		panic(err)
	}

	request.Header.Add("authorization", "Bearer "+config.BearerToken)
	request.Header.Add("user-agent", config.UserAgent)
	request.Header.Add("ifunny-project-id", "ifunny")
	return request
}

func getFeedPage(config *IFunnyConfig, nextPage string) FeedPage {
	request := mustIFunnyRequest(config, "GET", "/feeds/"+config.Feed, nil)
	request.Header.Add("accept", "video/mp4, image/jpeg")

	query := request.URL.Query()
	query.Add("next", nextPage)
	request.URL.RawQuery = query.Encode()

	response, err := http.DefaultClient.Do(request)
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

func getContent(config *IFunnyConfig, id string) Content {
	request := mustIFunnyRequest(config, "GET", "/content/"+id, nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	wrap := new(ContentWrap)
	if err := json.Unmarshal(bodyBytes, wrap); err != nil {
		panic(err)
	}

	return wrap.Data
}

func getUser(config *IFunnyConfig, id string) User {
	url := config.APIRoot + "/users/" + id

	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}

	request.Header.Add("authorization", "Bearer "+config.BearerToken)
	request.Header.Add("user-agent", config.UserAgent)
	request.Header.Add("ifunny-project-id", "ifunny")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	wrap := new(UserWrap)
	if err := json.Unmarshal(bodyBytes, wrap); err != nil {
		panic(err)
	}

	return wrap.Data
}
