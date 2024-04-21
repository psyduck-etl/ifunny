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
	specStopAfter = sdk.Spec{
		Name:        "stop-after",
		Description: "stop producing after n content",
		Type:        cty.Number,
		Required:    false,
		Default:     cty.NumberIntVal(math.MaxUint8),
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
					"stop-after":   &specStopAfter,
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
				},
			},
			{
				Kinds:           sdk.PRODUCER,
				Name:            "ifunny-explore",
				ProvideProducer: produceExplore,
				Spec: sdk.SpecMap{
					"bearer-token": &specBearerToken,
					"user-agent":   &specUserAgent,
					"stop-after":   &specStopAfter,
					"compilation": {
						Name:        "compilation",
						Description: "Explore compilation to pull from",
						Required:    true,
						Type:        cty.String,
					},
					"kind": &sdk.Spec{
						Name:        "kind",
						Description: "Kind of content to explore, one of: [content, user, chat]",
						Required:    true,
						Type:        cty.String,
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
