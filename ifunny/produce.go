package ifunny

import (
	"encoding/json"

	"github.com/gastrodon/psyduck/sdk"
)

func produceFeed(parse func(interface{}) error) sdk.Producer {
	config := mustConfig(parse)

	return func(signal chan string) chan []byte {
		data := make(chan []byte, 32)

		go func() {
			produced := 0
			nextPage := ""

			for {
				page := getFeedPage(config, nextPage)
				nextPage = page.Paging.Cursors.Next
				pageSize := len(page.Items)
				pageIndex := 0

				next := func() ([]byte, bool) {
					if pageIndex == pageSize {
						return nil, false
					}

					pageItemBytes, err := json.Marshal(page.Items[pageIndex])
					if err != nil {
						panic(err)
					}

					pageIndex++
					return pageItemBytes, true
				}

				sdk.ProduceChunk(next, parse, data, signal)
				produced += pageSize

				if config.StopAfter != 0 && produced > config.StopAfter {
					return
				}
			}
		}()

		return data
	}
}
