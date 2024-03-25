package main

import (
	"encoding/json"

	"github.com/psyduck-etl/sdk"
)

func produceFeed(parse sdk.Parser, specParse sdk.SpecParser) (sdk.Producer, error) {
	config := mustConfig(parse)

	return func(send chan<- []byte, errs chan<- error) {
		nextPage := ""
		pageSize := 0
		pageIndex := 0
		produced := 0

		page, err := getFeedPage(config, nextPage)
		if err != nil {
			errs <- err
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

		if err := sdk.ProduceChunk(next, specParse, send); err != nil {
			errs <- err
		}

		close(send)
		close(errs)
	}, nil
}
