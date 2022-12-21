package main

import (
	"github.com/gastrodon/psyduck/sdk"
	"github.com/zclconf/go-cty/cty"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "scyther",
		Resources: []*sdk.Resource{
			{
				Name:            "scyther-queue",
				Kinds:           sdk.PRODUCER | sdk.CONSUMER,
				ProvideProducer: produceQueue,
				ProvideConsumer: consumeQueue,
				Spec: sdk.SpecMap{
					"url": &sdk.Spec{
						Name:        "url",
						Description: "scyther host url",
						Type:        sdk.String,
						Required:    true,
					},
					"queue": &sdk.Spec{
						Name:        "queue",
						Description: "queue to read/write + ensure exists",
						Type:        sdk.String,
						Required:    true,
					},
					"stop-if-exhausted": &sdk.Spec{
						Name:        "stop-if-exhausted",
						Description: "stop producing if our queue is empty",
						Type:        sdk.Bool,
						Required:    false,
						Default:     cty.BoolVal(false),
					},
					"delay-if-exhausted": &sdk.Spec{
						Name:        "delay-if-exhausted",
						Description: "how long to wait before retrying an empty queue",
						Type:        sdk.Integer,
						Required:    false,
						Default:     cty.NumberIntVal(1_000),
					},
				},
			},
		},
	}
}
