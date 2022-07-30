package psyduck

import (
	"github.com/gastrodon/psyduck-std/psyduck/consume"
	"github.com/gastrodon/psyduck-std/psyduck/produce"
	"github.com/gastrodon/psyduck-std/psyduck/transform"
	"github.com/gastrodon/psyduck/sdk"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "psyduck",
		Resources: []*sdk.Resource{
			&sdk.Resource{
				Name:            "psyduck-constant",
				Kinds:           sdk.PRODUCER,
				ProvideProducer: produce.Constant,
			},
			&sdk.Resource{
				Name:            "psyduck-trash",
				Kinds:           sdk.CONSUMER,
				ProvideConsumer: consume.Trash,
			},
			&sdk.Resource{
				Name:               "psyduck-inspect",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Inspect,
			},
			&sdk.Resource{
				Name:               "psyduck-snippet",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Snippet,
			},
			&sdk.Resource{
				Name:               "psyduck-zoom",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Zoom,
			},
		},
	}
}
