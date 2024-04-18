package main

import (
	"math"

	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
)

var (
	specBearerToken = sdk.Spec{
		Name:        "bearer-token",
		Description: "bearer token to auth with",
		Type:        cty.String,
		Required:    true,
	}
	specUserAgent = sdk.Spec{
		Name:        "user-agent",
		Description: "user agent to make requests as",
		Type:        cty.String,
		Required:    true,
	}
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "ifunny",
		Resources: []*sdk.Resource{
			{
				Kinds:           sdk.PRODUCER,
				Name:            "ifunny-feed",
				ProvideProducer: produceFeed,
				Spec: sdk.SpecMap{
					"bearer-token": &specBearerToken,
					"user-agent":   &specUserAgent,
					"feed": &sdk.Spec{
						Name:        "feed",
						Description: "feed to pull content from",
						Type:        cty.String,
						Required:    false,
						Default:     cty.StringVal(""),
					},
					"timeline": &sdk.Spec{
						Name:        "timeline",
						Description: "id of user to pull content from",
						Type:        cty.String,
						Required:    false,
						Default:     cty.StringVal(""),
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
				Kinds:           sdk.PRODUCER,
				Name:            "ifunny-comments",
				ProvideProducer: produceComments,
				Spec: sdk.SpecMap{
					"bearer-token": &specBearerToken,
					"user-agent":   &specUserAgent,
					"content": &sdk.Spec{
						Name:        "content",
						Description: "Content item to iter comments from",
						Required:    true,
						Type:        cty.String,
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
				Spec: sdk.SpecMap{
					"bearer-token": &specBearerToken,
					"user-agent":   &specUserAgent,
				},
			},
			{
				Kinds:              sdk.TRANSFORMER,
				Name:               "ifunny-lookup-user",
				ProvideTransformer: lookupUser,
				Spec: sdk.SpecMap{
					"bearer-token": &specBearerToken,
					"user-agent":   &specUserAgent,
				},
			},
		},
	}
}
