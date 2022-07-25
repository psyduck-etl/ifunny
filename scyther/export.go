package scyther

import (
	"github.com/gastrodon/psyduck/sdk"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "scyther",
		ProvideProducer: map[string]sdk.ProducerProvider{
			"scyther-read-queue": produceQueue,
		},
		ProvideConsumer: map[string]sdk.ConsumerProvider{
			"scyther-write-queue": consumeQueue,
		},
		ProvideTransformer: map[string]sdk.TransformerProvider{},
	}
}
