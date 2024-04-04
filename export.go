package main

import (
	"math"

	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
)

type IFunnyConfig struct {
	BearerToken string `psy:"bearer-token"`
	UserAgent   string `psy:"user-agent"`

	Feed      string `psy:"feed"`
	StopAfter int    `psy:"stop-after"`
}

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "ifunny",
		Resources: []*sdk.Resource{
			{
				Kinds:           sdk.PRODUCER,
				Name:            "ifunny-feed",
				ProvideProducer: produceFeed,
				Spec: sdk.SpecMap{
					"feed": &sdk.Spec{
						Name:        "feed",
						Description: "feed to pull content from",
						Type:        cty.String,
						Required:    true,
					},
					"bearer-token": &sdk.Spec{
						Name:        "bearer-token",
						Description: "bearer token to auth with",
						Type:        cty.String,
						Required:    true,
					},
					"user-agent": &sdk.Spec{
						Name:        "user-agent",
						Description: "user agent to make requests as",
						Type:        cty.String,
						Required:    true,
					},
					"stop-after": &sdk.Spec{
						Name:        "stop-after",
						Description: "stop producing after n content",
						Type:        cty.Number,
						Required:    false,
						Default:     cty.NumberIntVal(math.MaxUint8),
					},
				},
			},
			{
				Kinds:              sdk.TRANSFORMER,
				Name:               "ifunny-content-author",
				ProvideTransformer: getContentAuthor,
			},
			{
				Kinds:              sdk.TRANSFORMER,
				Name:               "ifunny-lookup-content",
				ProvideTransformer: lookupContent,
			},
			{
				Kinds:              sdk.TRANSFORMER,
				Name:               "ifunny-lookup-user",
				ProvideTransformer: lookupUser,
			},
		},
	}
}
