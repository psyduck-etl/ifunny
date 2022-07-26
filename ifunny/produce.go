package ifunny

import (
	"encoding/json"

	"github.com/gastrodon/psyduck/sdk"
)

func produceFeed(parse func(interface{}) error) (sdk.Producer, error) {
	config := mustConfig(parse)

	return func(signal chan string) (chan []byte, chan error) {
		data := make(chan []byte, 32)
		errors := make(chan error)

		go func() {
			produced := 0
			nextPage := ""

			for {
				page, err := getFeedPage(config, nextPage)
				if err != nil {
					errors <- err
					return
				}

				nextPage = page.Paging.Cursors.Next
				pageSize := len(page.Items)
				pageIndex := 0

				next := func() ([]byte, bool, error) {
					if pageIndex == pageSize {
						return nil, false, nil
					}

					pageItemBytes, err := json.Marshal(page.Items[pageIndex])
					if err != nil {
						return nil, false, err
					}

					pageIndex++
					return pageItemBytes, true, nil
				}

				sdk.ProduceChunk(next, parse, data, errors, signal)
				produced += pageSize

				if config.StopAfter != 0 && produced > config.StopAfter {
					return
				}
			}
		}()

		return data, errors
	}, nil
}
