package scyther

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Message struct {
	Message string `json:"message"`
}

func queueURL(config *scytherConfig) string {
	return fmt.Sprintf("%s/queues/%s", config.URL, config.Queue)
}

func ensureQueue(config *scytherConfig) {
	body := map[string]string{"name": config.Queue}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}

	response, err := http.Post(config.URL+"/queues", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		panic(err)
	}

	response.Body.Close()
}

func getQueueHead(config *scytherConfig) (string, bool) {
	response, err := http.Get(queueURL(config) + "/head")
	if err != nil {
		panic(err)
	}

	defer response.Body.Close()
	if response.StatusCode == 404 {
		return "", false
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	message := new(Message)
	if err := json.Unmarshal(bodyBytes, message); err != nil {
		panic(err)
	}

	return message.Message, true
}

func putQueueHead(config *scytherConfig, each []byte) {
	body := bytes.NewReader(each)
	request, err := http.NewRequest("PUT", queueURL(config), body)
	if err != nil {
		panic(err)
	}

	client := &http.Client{}
	if _, err := client.Do(request); err != nil {
		panic(err)
	}
}
