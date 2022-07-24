package scyther

import (
	"github.com/gastrodon/psyduck/model"
)

func Plugin() *model.Plugin {
	return &model.Plugin{
		Name: "scyther",
		ProvideProducer: map[string]model.ProducerProvider{
			"scyther-read-queue": produceQueue,
		},
		ProvideConsumer: map[string]model.ConsumerProvider{
			"scyther-write-queue": consumeQueue,
		},
		ProvideTransformer: map[string]model.TransformerProvider{},
	}
}
