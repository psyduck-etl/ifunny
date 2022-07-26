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

		ProvideProducer: map[string]sdk.ProducerProvider{
			"psyduck-constant": produce.Constant,
		},
		ProvideConsumer: map[string]sdk.ConsumerProvider{
			"psyduck-trash": consume.Trash,
		},
		ProvideTransformer: map[string]sdk.TransformerProvider{
			"psyduck-inspect":      transform.Inspect,
			"psyduck-json":         transform.MarshalJSON,
			"psyduck-json-snippet": transform.JSONSnippet,
		},
	}
}
