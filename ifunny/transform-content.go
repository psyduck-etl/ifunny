package ifunny

import (
	"fmt"

	"github.com/gastrodon/psyduck/sdk"
)

func getContentAuthor(parse func(interface{}) error) sdk.Transformer {
	return func(contentRaw interface{}) interface{} {
		content, ok := contentRaw.(Content)
		if !ok {
			panic(fmt.Errorf("%#v isn't a Content", contentRaw))
		}

		return content.Creator
	}
}

func getItemID(parse func(interface{}) error) sdk.Transformer {
	return func(data interface{}) interface{} {
		haver, ok := data.(IDHaver)
		if !ok {
			panic(fmt.Errorf("%#v isn't an IDHaver", data))
		}

		return haver.GetID()
	}
}
