package ifunny

import (
	"encoding/json"

	"github.com/gastrodon/psyduck/sdk"
)

func produceFeed(parse sdk.Parser, specParse sdk.SpecParser) (sdk.Producer, error) {
	config := mustConfig(parse)

	return func() (chan []byte, chan error) {
		data := make(chan []byte, 32)
		errors := make(chan error)

		nextPage := ""
		pageSize := 0
		pageIndex := 0
		produced := 0

		page, err := getFeedPage(config, nextPage)
		if err != nil {
			errors <- err
		}

		next := func() ([]byte, bool, error) {
			if config.StopAfter != 0 && config.StopAfter <= produced {
				return nil, false, nil
			}

			if pageIndex == pageSize {
				page, err = getFeedPage(config, nextPage)
				if err != nil {
					return nil, false, err
				}

				nextPage = page.Paging.Cursors.Next
				pageSize = len(page.Items)
				pageIndex = 0
			}

			pageItemBytes, err := json.Marshal(page.Items[pageIndex])
			if err != nil {
				return nil, false, err
			}

			produced++
			pageIndex++
			return pageItemBytes, true, nil
		}

		go func() {
			sdk.ProduceChunk(next, specParse, data, errors)
			close(data)
			close(errors)
		}()

		return data, errors
	}, nil
}
