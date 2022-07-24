package psyduck

import (
	"github.com/gastrodon/psyduck/library/psyduck/consume"
	"github.com/gastrodon/psyduck/library/psyduck/produce"
	"github.com/gastrodon/psyduck/library/psyduck/transform"
	"github.com/gastrodon/psyduck/model"
)

func Plugin() *model.Plugin {
	return &model.Plugin{
		Name: "psyduck",

		ProvideProducer: map[string]model.ProducerProvider{
			"psyduck-constant": produce.Constant,
		},
		ProvideConsumer: map[string]model.ConsumerProvider{
			"psyduck-trash": consume.Trash,
		},
		ProvideTransformer: map[string]model.TransformerProvider{
			"psyduck-inspect": transform.Inspect,
		},
	}
}
