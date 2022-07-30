package ifunny

import (
	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/zclconf/go-cty/cty"
)

const IFUNNY_API_ROOT = "https://api.ifunny.mobi/v4"

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "ifunny",
		Resources: []*sdk.Resource{
			&sdk.Resource{
				Kinds:           sdk.PRODUCER,
				Name:            "ifunny-feed",
				ProvideProducer: produceFeed,
				Spec: hcldec.ObjectSpec{
					"exit-on-error": sdk.SpecExitOnError(true),
					"per-minute":    sdk.SpecPerMinute(15),
					"feed": &hcldec.AttrSpec{
						Name:     "feed",
						Type:     cty.String,
						Required: true,
					},
					"bearer-token": &hcldec.AttrSpec{
						Name:     "bearer-token",
						Type:     cty.String,
						Required: true,
					},
					"user-agent": &hcldec.AttrSpec{
						Name:     "user-agent",
						Type:     cty.String,
						Required: true,
					},
					"api-root": &hcldec.DefaultSpec{
						Primary: &hcldec.AttrSpec{
							Name:     "api-root",
							Type:     cty.String,
							Required: false,
						},
						Default: &hcldec.LiteralSpec{
							Value: cty.StringVal(IFUNNY_API_ROOT),
						},
					},
					"stop-after": &hcldec.DefaultSpec{
						Primary: &hcldec.AttrSpec{
							Name:     "stop-after",
							Type:     cty.Number,
							Required: false,
						},
						Default: &hcldec.LiteralSpec{
							Value: cty.NumberIntVal(128),
						},
					},
				},
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
