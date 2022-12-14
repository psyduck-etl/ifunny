package ifunny

import (
	"encoding/json"
	"fmt"

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

func getFeedPage(config *IFunnyConfig, nextPage string) (*FeedPage, error) {
	method := "GET"
	if config.Feed == "collective" {
		// eye roll emoji
		method = "POST"
	}

	request := mustIFunnyRequest(config, method, "/feeds/"+config.Feed, nil)
	request.Header.Add("accept", "video/mp4, image/jpeg")

	query := request.URL.Query()
	query.Add("next", nextPage)
	request.URL.RawQuery = query.Encode()

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}

	if response.StatusCode != 200 {
		if nextPage != "" {
			return nil, fmt.Errorf("got %d getting the feed %s[%s]", response.StatusCode, config.Feed, nextPage)
		}

		return nil, fmt.Errorf("got %d getting the feed %s[<root>]", response.StatusCode, config.Feed)
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	page := new(FeedPageResponse)
	json.Unmarshal(bodyBytes, page)

	return &page.Data.Content, err
}

func getContent(config *IFunnyConfig, id string) (*Content, error) {
	request := mustIFunnyRequest(config, "GET", "/content/"+id, nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	wrap := new(ContentWrap)
	if err := json.Unmarshal(bodyBytes, wrap); err != nil {
		return nil, err
	}

	return &wrap.Data, nil
}

func getUser(config *IFunnyConfig, id string) (*User, error) {
	url := config.APIRoot + "/users/" + id

	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	request.Header.Add("authorization", "Bearer "+config.BearerToken)
	request.Header.Add("user-agent", config.UserAgent)
	request.Header.Add("ifunny-project-id", "ifunny")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	wrap := new(UserWrap)
	if err := json.Unmarshal(bodyBytes, wrap); err != nil {
		return nil, err
	}

	return &wrap.Data, nil
}
