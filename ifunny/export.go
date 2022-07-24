package ifunny

import (
	"github.com/gastrodon/psyduck/model"
)

func Plugin() *model.Plugin {
	return &model.Plugin{
		Name: "ifunny",
		ProvideProducer: map[string]model.ProducerProvider{
			"ifunny-feed": produceFeed,
		},
		ProvideConsumer:    map[string]model.ConsumerProvider{},
		ProvideTransformer: map[string]model.TransformerProvider{},
	}
}
