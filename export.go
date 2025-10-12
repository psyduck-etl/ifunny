package main

import (
	"math"

	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"
)

var (
	specBearerToken = &sdk.Spec{
		Name:        "bearer-token",
		Description: "bearer token to auth with",
		Type:        cty.String,
		Required:    true,
	}
	specUserAgent = &sdk.Spec{
		Name:        "user-agent",
		Description: "user agent to make requests as",
		Type:        cty.String,
		Required:    true,
	}
	specStopAfter = &sdk.Spec{
		Name:        "stop-after",
		Description: "stop producing after n content",
		Type:        cty.Number,
		Default:     cty.NumberIntVal(math.MaxUint8),
	}
)

func main() {
	plugin := &sdk.Plugin{
		Name: "ifunny",
		Resources: []*sdk.Resource{
			{
				Name:            "ifunny-feed",
				Kinds:           sdk.PRODUCER,
				ProvideProducer: FeedProducer,
				Spec: []*sdk.Spec{
					specBearerToken,
					specUserAgent,
					specStopAfter,
					{
						Name:        "feed",
						Description: "feed to pull content from",
						Type:        cty.String,
						Default:     cty.StringVal(""),
					},
					{
						Name:        "timeline",
						Description: "id of user to pull content from",
						Type:        cty.String,
						Default:     cty.StringVal(""),
					},
				},
			},
			{
				Name:            "ifunny-explore",
				Kinds:           sdk.PRODUCER,
				ProvideProducer: ExploreProducer,
				Spec: []*sdk.Spec{
					specBearerToken,
					specUserAgent,
					specStopAfter,
					{
						Name:        "compilation",
						Description: "Explore compilation to pull from",
						Required:    true,
						Type:        cty.String,
					},
					{
						Name:        "kind",
						Description: "Kind of content to explore, one of: [content, user, chat]",
						Required:    true,
						Type:        cty.String,
					},
				},
			},
			{
				Name:            "ifunny-comments",
				Kinds:           sdk.PRODUCER,
				ProvideProducer: CommentsProducer,
				Spec: []*sdk.Spec{
					specBearerToken,
					specUserAgent,
					{
						Name:        "content",
						Description: "Content item to iter comments from",
						Required:    true,
						Type:        cty.String,
					},
				},
			},
			{
				Name:               "ifunny-content-author",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: ContentAuthorTransformer,
			},
			{
				Name:               "ifunny-lookup-content",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: LookupContentTransformer,
				Spec: []*sdk.Spec{
					specBearerToken,
					specUserAgent,
				},
			},
			{
				Name:               "ifunny-lookup-user",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: LookupUserTransformer,
				Spec: []*sdk.Spec{
					specBearerToken,
					specUserAgent,
				},
			},
		},
	}

	// Run as gRPC client process
	sdk.RunAsClientProcess(plugin)
}
