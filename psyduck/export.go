package psyduck

import (
	"github.com/gastrodon/psyduck-std/psyduck/consume"
	"github.com/gastrodon/psyduck-std/psyduck/produce"
	"github.com/gastrodon/psyduck-std/psyduck/transform"
	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/zclconf/go-cty/cty"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "psyduck",
		Resources: []*sdk.Resource{
			&sdk.Resource{
				Name:            "psyduck-constant",
				Kinds:           sdk.PRODUCER,
				ProvideProducer: produce.Constant,
				Spec: hcldec.ObjectSpec{
					"value": &hcldec.DefaultSpec{
						Primary: &hcldec.AttrSpec{
							Name:     "value",
							Type:     cty.String,
							Required: false,
						},
						Default: &hcldec.LiteralSpec{
							Value: cty.StringVal("0"),
						},
					},
				},
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
				Spec: hcldec.ObjectSpec{
					"be-string": &hcldec.DefaultSpec{
						Primary: &hcldec.AttrSpec{
							Name:     "be-string",
							Type:     cty.String,
							Required: false,
						},
						Default: &hcldec.LiteralSpec{
							Value: cty.BoolVal(true),
						},
					},
				},
			},
			&sdk.Resource{
				Name:               "psyduck-snippet",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Snippet,
				Spec: hcldec.ObjectSpec{
					"fields": &hcldec.AttrSpec{
						Name:     "fields",
						Type:     cty.List(cty.String),
						Required: true,
					},
				},
			},
			&sdk.Resource{
				Name:               "psyduck-zoom",
				Kinds:              sdk.TRANSFORMER,
				ProvideTransformer: transform.Zoom,
				Spec: hcldec.ObjectSpec{
					"fields": &hcldec.AttrSpec{
						Name:     "field",
						Type:     cty.String,
						Required: true,
					},
				},
			},
		},
	}
}
