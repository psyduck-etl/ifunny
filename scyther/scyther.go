package main

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

func queueURL(config *ScytherConfig) string {
	return fmt.Sprintf("%s/queues/%s", config.URL, config.Queue)
}

func ensureQueue(config *ScytherConfig) error {
	body := map[string]string{"name": config.Queue}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return err
	}

	response, err := http.Post(config.URL+"/queues", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	response.Body.Close()
	return nil
}

func getQueueHead(config *ScytherConfig) ([]byte, bool, error) {
	response, err := http.Get(queueURL(config) + "/head")
	if err != nil {
		return nil, false, err
	}

	defer response.Body.Close()
	if response.StatusCode == 404 {
		return nil, false, fmt.Errorf("no such queue %s", config.Queue)
	}

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, false, err
	}

	message := new(Message)
	if err := json.Unmarshal(bodyBytes, message); err != nil {
		return nil, false, err
	}

	return []byte(message.Message), message.Message != "", nil
}

func putQueueHead(config *ScytherConfig, each []byte) error {
	body := bytes.NewReader(each)
	request, err := http.NewRequest("PUT", queueURL(config), body)
	if err != nil {
		return err
	}

	client := &http.Client{}
	if _, err := client.Do(request); err != nil {
		return err
	}

	return nil
}
