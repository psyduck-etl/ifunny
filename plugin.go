package main

import "github.com/psyduck-etl/sdk"

// main is unused: the host loads this package as a -buildmode=plugin shared
// object and calls Plugin directly. It exists only so a plain `go build`
// (the portable CI check) can link the main package.
func main() {}

// Plugin is the ABI entrypoint the psyduck host looks up in the compiled
// plugin object. It returns the full set of iFunny content-discovery
// resources assembled under the SDK v0.5.0 in-process plugin.
func Plugin() sdk.Plugin {
	return sdk.NewInProc("ifunny",
		// --- content producers ---
		&sdk.Resource{
			Name:            "ifunny-feed",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceFeed,
			Spec: specs(
				&sdk.Spec{
					Name:        "feed",
					Description: "feed to pull content from, e.g. featured or collective",
					Type:        sdk.TypeString,
					Required:    true,
				},
				encodingSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-timeline",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceTimeline,
			Spec: specs(
				&sdk.Spec{
					Name:        "by-id",
					Description: "user id whose timeline to pull; mutually exclusive with by-nick",
					Type:        sdk.TypeString,
					Default:     "",
				},
				&sdk.Spec{
					Name:        "by-nick",
					Description: "user nick whose timeline to pull; mutually exclusive with by-id",
					Type:        sdk.TypeString,
					Default:     "",
				},
				encodingSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-explore",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceExplore,
			Spec: specs(
				&sdk.Spec{
					Name:        "compilation",
					Description: "explore compilation to pull from, e.g. content_top_today",
					Type:        sdk.TypeString,
					Required:    true,
				},
				&sdk.Spec{
					Name:        "kind",
					Description: "kind of entity the compilation yields, one of: content, user, chat",
					Type:        sdk.TypeString,
					Required:    true,
				},
				encodingSpec(),
			),
		},

		// --- comment producers ---
		&sdk.Resource{
			Name:            "ifunny-comments",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceComments,
			Spec: specs(
				&sdk.Spec{
					Name:        "content",
					Description: "content id to pull comments from",
					Type:        sdk.TypeString,
					Required:    true,
				},
				encodingSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-replies",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceReplies,
			Spec: specs(
				&sdk.Spec{
					Name:        "content",
					Description: "content id the comment lives on",
					Type:        sdk.TypeString,
					Required:    true,
				},
				&sdk.Spec{
					Name:        "comment",
					Description: "comment id to pull replies from",
					Type:        sdk.TypeString,
					Required:    true,
				},
				encodingSpec(),
			),
		},

		// --- user producers ---
		&sdk.Resource{
			Name:            "ifunny-smiles",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceSmiles,
			Spec: specs(
				&sdk.Spec{
					Name:        "content",
					Description: "content id to pull smiling users from",
					Type:        sdk.TypeString,
					Required:    true,
				},
				encodingSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-republishers",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceRepublishers,
			Spec: specs(
				&sdk.Spec{
					Name:        "content",
					Description: "content id to pull republishing users from",
					Type:        sdk.TypeString,
					Required:    true,
				},
				encodingSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-subscribers",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceSubscribers,
			Spec: specs(
				&sdk.Spec{
					Name:        "user",
					Description: "user id to pull subscribers from",
					Type:        sdk.TypeString,
					Required:    true,
				},
				encodingSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-subscriptions",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceSubscriptions,
			Spec: specs(
				&sdk.Spec{
					Name:        "user",
					Description: "user id to pull subscriptions from",
					Type:        sdk.TypeString,
					Required:    true,
				},
				encodingSpec(),
			),
		},

		// --- chat producers ---
		&sdk.Resource{
			Name:            "ifunny-channels",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceChannels,
			Spec: specs(
				&sdk.Spec{
					Name:        "query",
					Description: "search query for open channels; empty yields trending channels",
					Type:        sdk.TypeString,
					Default:     "",
				},
				encodingSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-chat-history",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceChatHistory,
			Spec: specs(
				&sdk.Spec{
					Name:        "channel",
					Description: "channel name to pull message history from",
					Type:        sdk.TypeString,
					Required:    true,
				},
				encodingSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-chat-listen",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceChatListen,
			Spec: specs(
				&sdk.Spec{
					Name:        "channel",
					Description: "channel name to listen to live events on",
					Type:        sdk.TypeString,
					Required:    true,
				},
				&sdk.Spec{
					Name:        "stop-after",
					Description: "stop after n live events; 0 listens until the process exits",
					Type:        sdk.TypeInt,
					Default:     0,
				},
				encodingSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-chat-invites",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceChatInvites,
			Spec: specs(
				&sdk.Spec{
					Name:        "stop-after",
					Description: "stop after n received invites; 0 listens until the process exits",
					Type:        sdk.TypeInt,
					Default:     0,
				},
				encodingSpec(),
			),
		},

		// --- transformers ---
		&sdk.Resource{
			Name:               "ifunny-author",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: authorTransformer,
			Spec:               specs(acceptSpec(), emitSpec()),
		},
		&sdk.Resource{
			Name:               "ifunny-tags",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: tagsTransformer,
			Spec:               specs(acceptSpec(), emitSpec()),
		},
		&sdk.Resource{
			Name:               "ifunny-content",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: contentTransformer,
			Spec:               specs(acceptSpec(), emitSpec()),
		},
		&sdk.Resource{
			Name:               "ifunny-user",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: userTransformer,
			Spec: specs(
				&sdk.Spec{
					Name:        "by-id",
					Description: "look up by the input's id field; mutually exclusive with by-nick",
					Type:        sdk.TypeBool,
					Default:     false,
				},
				&sdk.Spec{
					Name:        "by-nick",
					Description: "look up by the input's nick field; mutually exclusive with by-id",
					Type:        sdk.TypeBool,
					Default:     false,
				},
				acceptSpec(),
				emitSpec(),
			),
		},
		&sdk.Resource{
			Name:               "ifunny-channel",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: channelTransformer,
			Spec:               specs(acceptSpec(), emitSpec()),
		},
	)
}
