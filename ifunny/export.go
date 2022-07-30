package ifunny

import (
	"github.com/gastrodon/psyduck/sdk"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "ifunny",
		Resources: []*sdk.Resource{
			&sdk.Resource{
				Kinds:           sdk.PRODUCER,
				Name:            "ifunny-feed",
				ProvideProducer: produceFeed,
			},
			&sdk.Resource{
				Kinds:              sdk.TRANSFORMER,
				Name:               "ifunny-id",
				ProvideTransformer: getItemID,
			},
			&sdk.Resource{
				Kinds:              sdk.TRANSFORMER,
				Name:               "ifunny-content-author",
				ProvideTransformer: getContentAuthor,
			},
			&sdk.Resource{
				Kinds:              sdk.TRANSFORMER,
				Name:               "ifunny-lookup-content",
				ProvideTransformer: lookupContent,
			},
			&sdk.Resource{
				Kinds:              sdk.TRANSFORMER,
				Name:               "ifunny-lookup-user",
				ProvideTransformer: lookupUser,
			},
		},
	}
}
