package ifunny

import (
	"github.com/gastrodon/psyduck/sdk"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "ifunny",
		ProvideProducer: map[string]sdk.ProducerProvider{
			"ifunny-feed": produceFeed,
		},
		ProvideConsumer: map[string]sdk.ConsumerProvider{},
		ProvideTransformer: map[string]sdk.TransformerProvider{
			"ifunny-content-author": getContentAuthor,
			"ifunny-id":             getItemID,
		},
	}
}
