package scyther

import (
	"github.com/gastrodon/psyduck/sdk"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "scyther",
		ProvideProducer: map[string]sdk.ProducerProvider{
			"scyther-pull": produceQueue,
		},
		ProvideConsumer: map[string]sdk.ConsumerProvider{
			"scyther-push": consumeQueue,
		},
		ProvideTransformer: map[string]sdk.TransformerProvider{},
	}
}
