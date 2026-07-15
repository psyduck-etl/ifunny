package main

import (
	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/rpc"
)

// main serves the plugin over gRPC to the psyduck host that launched this
// binary as a subprocess.
func main() { rpc.Serve(Plugin()) }

// Plugin returns the full set of iFunny content-discovery resources
// assembled as an in-process plugin, which main serves to the host.
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
				emitSpec(),
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
				emitSpec(),
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
				emitSpec(),
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
				emitSpec(),
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
				emitSpec(),
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
				emitSpec(),
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
				emitSpec(),
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
				emitSpec(),
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
				emitSpec(),
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
				emitSpec(),
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
				emitSpec(),
			),
		},
		&sdk.Resource{
			Name:            "ifunny-chat-invites",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: produceChatInvites,
			Spec:            specs(emitSpec()),
		},

		// --- transformers ---
		&sdk.Resource{
			Name:               "ifunny-author",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: authorTransformer,
			Spec: specs(
				&sdk.Spec{
					Name:        "source",
					Description: `which source entity type the transformer decodes, one of: "content", "comment", or "chat". "comment" and "chat" cannot use accept = "string" — no fetch-by-ref endpoint exists for them.`,
					Type:        sdk.TypeString,
					Required:    true,
				},
				&sdk.Spec{
					Name:        "emit-by",
					Description: `which author reference to emit and fetch by, one of: "id" (numeric user id, default) or "nick" (user nickname)`,
					Type:        sdk.TypeString,
					Default:     "id",
				},
				acceptSpec(),
				emitSpec(),
			),
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
					Name:        "by",
					Description: `which user reference to key on, one of: "id" (numeric user id, default) or "nick" (user nickname)`,
					Type:        sdk.TypeString,
					Default:     "id",
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

		// --- explode transformers ---
		&sdk.Resource{
			Name:               "ifunny-timeline-explode",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: timelineTransformer,
			Spec: specs(
				&sdk.Spec{
					Name:        "by",
					Description: `which user reference to key on, one of: "id" (numeric user id, default) or "nick" (user nickname)`,
					Type:        sdk.TypeString,
					Default:     "id",
				},
				&sdk.Spec{
					Name:        "limit",
					Description: "maximum number of content items to emit per input user (0 = no limit)",
					Type:        sdk.TypeInt,
					Default:     "0",
				},
				acceptSpec(),
				emitSpec(),
			),
		},
		&sdk.Resource{
			Name:               "ifunny-comments-explode",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: commentsTransformer,
			Spec: specs(
				&sdk.Spec{
					Name:        "max-depth",
					Description: "maximum depth of comment nesting to emit (0 = top-level only, 1 = replies, -1 = unlimited, default)",
					Type:        sdk.TypeInt,
					Default:     "-1",
				},
				acceptSpec(),
				emitSpec(),
			),
		},
		&sdk.Resource{
			Name:               "ifunny-interactions",
			Kinds:              sdk.TRANSFORMER,
			ProvideTransformer: interactionsTransformer,
			Spec: specs(
				&sdk.Spec{
					Name:        "interactions",
					Description: `list of user interactions to fan out, each of: "author", "smiles", "republishes", "comments"`,
					Type:        sdk.TypeList,
					Required:    true,
					ElemType: &sdk.Spec{
						Type: sdk.TypeString,
					},
				},
				acceptSpec(),
				emitSpec(),
			),
		},
	)
}
