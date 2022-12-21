package main

import (
	"github.com/gastrodon/psyduck/sdk"
	"github.com/zclconf/go-cty/cty"
)

const IFUNNY_API_ROOT = "https://api.ifunny.mobi/v4"

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
						Type:        sdk.String,
						Required:    true,
					},
					"bearer-token": &sdk.Spec{
						Name:        "bearer-token",
						Description: "bearer token to auth with",
						Type:        sdk.String,
						Required:    true,
					},
					"user-agent": &sdk.Spec{
						Name:        "user-agent",
						Description: "user agent to make requests as",
						Type:        sdk.String,
						Required:    true,
					},
					"api-root": &sdk.Spec{
						Name:        "api-root",
						Description: "root of the iFunny API",
						Type:        sdk.String,
						Required:    false,
						Default:     cty.StringVal(IFUNNY_API_ROOT),
					},
					"stop-after": &sdk.Spec{
						Name:        "stop-after",
						Description: "stop producing after n content",
						Type:        sdk.Integer,
						Required:    false,
						Default:     cty.NumberIntVal(128),
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
