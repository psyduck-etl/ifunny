package scyther

import (
	"github.com/gastrodon/psyduck/sdk"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/zclconf/go-cty/cty"
)

func Plugin() *sdk.Plugin {
	return &sdk.Plugin{
		Name: "scyther",
		Resources: []*sdk.Resource{
			&sdk.Resource{
				Name:            "scyther-queue",
				Kinds:           sdk.PRODUCER | sdk.CONSUMER,
				ProvideProducer: produceQueue,
				ProvideConsumer: consumeQueue,
				Spec: hcldec.ObjectSpec{
					"url": &hcldec.AttrSpec{
						Name:     "url",
						Type:     cty.String,
						Required: true,
					},
					"queue": &hcldec.AttrSpec{
						Name:     "queue",
						Type:     cty.String,
						Required: true,
					},
					"delay-if-exhausted": &hcldec.DefaultSpec{
						Primary: &hcldec.AttrSpec{
							Name:     "delay-if-exhausted",
							Type:     cty.Number,
							Required: false,
						},
						Default: &hcldec.LiteralSpec{
							Value: cty.NumberIntVal(500),
						},
					},
					"stop-if-exhausted": &hcldec.DefaultSpec{
						Primary: &hcldec.AttrSpec{
							Name:     "stop-if-exhausted",
							Type:     cty.Bool,
							Required: false,
						},
						Default: &hcldec.LiteralSpec{
							Value: cty.BoolVal(true),
						},
					},
				},
			},
		},
	}
}
