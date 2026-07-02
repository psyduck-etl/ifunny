package main

import (
	"encoding/json"
	"net/url"
	"strconv"

	ifunny "github.com/open-ifunny/ifunny-go"
	"github.com/open-ifunny/ifunny-go/compose"
)

// pageSize matches the ifunny-go iterators' fixed request size.
const pageSize = 30

// pageInto walks a cursor-paginated iFunny endpoint whose items live under
// data.<key> and sends each item's raw JSON onto send. It is built on the
// client's public RequestJSON + compose.Request so the plugin can cover
// discovery endpoints the released ifunny-go client does not yet expose
// (content smilers/republishers, comment replies, user subscribers/
// subscriptions, timeline-by-nick). The upstream iterator versions of these
// are proposed against ifunny-go separately; once released they replace this.
func pageInto(client *ifunny.Client, key, path string, send chan<- []byte, errs chan<- error) {
	defer close(send)
	defer close(errs)

	page := compose.NoPage[string]()
	for {
		query := url.Values{"limit": []string{strconv.Itoa(pageSize)}}
		if page.Key != compose.NONE {
			query.Set(string(page.Key), page.Value)
		}

		resp := new(struct {
			Data map[string]struct {
				Items  []json.RawMessage `json:"items"`
				Paging ifunny.Cursor     `json:"paging"`
			} `json:"data"`
		})
		if err := client.RequestJSON(compose.Request{Method: "GET", Path: path, Query: query}, resp); err != nil {
			errs <- err
			return
		}

		items := resp.Data[key]
		for _, item := range items.Items {
			send <- item
		}

		if !items.Paging.HasNext {
			return
		}
		page = compose.Next(items.Paging.Cursors.Next)
	}
}
