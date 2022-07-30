package scyther

import (
	"github.com/gastrodon/psyduck/sdk"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "scyther",
		Resources: []*sdk.Resource{
			&sdk.Resource{
				Name:            "scyther-queue",
				Kinds:           sdk.PRODUCER | sdk.CONSUMER,
				ProvideProducer: produceQueue,
				ProvideConsumer: consumeQueue,
			},
		},
	}
}
