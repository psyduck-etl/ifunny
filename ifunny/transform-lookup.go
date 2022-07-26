package ifunny

import (
	"encoding/json"

	"github.com/gastrodon/psyduck/sdk"
)

func lookup(looker func(string) interface{}) sdk.Transformer {
	return func(data []byte) []byte {
		who := new(Identity)
		if err := json.Unmarshal(data, who); err != nil {
			panic(err)
		}

		foundBytes, err := json.Marshal(looker(who.ID))
		if err != nil {
			panic(err)
		}

		return foundBytes
	}
}

func lookupContent(parse func(interface{}) error) sdk.Transformer {
	config := mustConfig(parse)

	return lookup(func(id string) interface{} {
		return getContent(config, id)
	})

}

func lookupUser(parse func(interface{}) error) sdk.Transformer {
	config := mustConfig(parse)

	return lookup(func(id string) interface{} {
		return getUser(config, id)
	})
}
