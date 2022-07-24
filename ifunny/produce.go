package ifunny

import (
	"github.com/gastrodon/psyduck/model"
	"github.com/gastrodon/psyduck/sdk"
)

func produceFeed(parse func(interface{}) error) model.Producer {
	config := mustConfig(parse)

	return func(signal chan string) chan interface{} {
		data := make(chan interface{}, 32)

		go func() {
			produced := 0
			nextPage := ""

			for {
				page := getFeedPage(config, nextPage)
				nextPage = page.Paging.Cursors.Next
				pageSize := len(page.Items)
				pageIndex := 0

				next := func() (interface{}, bool) {
					if pageIndex == pageSize || produced+pageIndex == config.StopAfter {
						return nil, false
					}

					item := page.Items[pageIndex]
					pageIndex++
					return item, true
				}

				sdk.ProduceChunk(next, parse, data, signal)
				produced += pageSize
				if config.StopAfter != 0 && produced > config.StopAfter {
					break
				}
			}
		}()

		return data
	}
}
