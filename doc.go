// Package main is the iFunny data-source plugin for the psyduck-etl host.
// It compiles as a -buildmode=plugin shared object; the host loads it and
// looks up [Plugin] for the registered producers and transformers.
//
// The plugin exposes iFunny's content graph — posts, comments, users, and
// public chat channels — as psyduck resources that a discovery pipeline can
// chain into itself: profiles yield posts, posts yield comments, posts and
// comments yield the users who interacted with them, and those users yield
// more profiles. See the README at
// https://github.com/psyduck-etl/ifunny for the full graph and tag-census
// notes.
//
// # Registered resources
//
// Producers:
//
//   - ifunny-feed          — [produceFeed]
//   - ifunny-timeline      — [produceTimeline]
//   - ifunny-explore       — [produceExplore]
//   - ifunny-comments      — [produceComments]
//   - ifunny-replies       — [produceReplies]
//   - ifunny-smiles        — [produceSmiles]
//   - ifunny-republishers  — [produceRepublishers]
//   - ifunny-subscribers   — [produceSubscribers]
//   - ifunny-subscriptions — [produceSubscriptions]
//   - ifunny-channels      — [produceChannels]
//   - ifunny-chat-history  — [produceChatHistory]
//   - ifunny-chat-listen   — [produceChatListen]
//   - ifunny-chat-invites  — [produceChatInvites]
//
// Transformers:
//
//   - ifunny-author  — [authorTransformer]
//   - ifunny-tags    — [tagsTransformer]
//   - ifunny-content — [contentTransformer]
//   - ifunny-user    — [userTransformer]
//   - ifunny-channel — [channelTransformer]
//
// # Authentication
//
// Every API-backed resource — producers and transformers alike — takes a
// user-agent block plus exactly one of auth-basic (anonymous — a literal
// primed token, "generate", or "generate-cache") or auth-bearer (logged-in
// user's OAuth token; required for the chat resources). See [authConfig]
// and [userAgentConfig] for the full surface.
//
// # Transformer accept / emit matrix
//
// Every transformer takes two encoding fields (default "json"):
//
//   - accept — encoding of records the transformer decodes on input.
//     "json" is a rich object trusted only insofar as we find it useful:
//     if the field we need is present we use it, otherwise we fall back
//     to fetching the source entity by its own terminal ref. "string" is
//     a bare terminal ref of the source; a fetch is always required to
//     obtain any intermediates.
//   - emit — encoding of records the transformer encodes on output.
//     "json" is a fully-hydrated target — always fetched fresh; incoming
//     rich objects are never re-emitted verbatim. "string" is the
//     target's terminal ref, no hydration.
//
// The accept×emit matrix is solved at bind time. Bind-time errors:
// ifunny-tags with emit = "string" (no terminal ref for a tag list), and
// same-entity identity resources with accept = emit = "string"
// (ifunny-content, ifunny-channel, ifunny-user by-id — nothing to do).
// ifunny-user by-nick sparse→sparse is *not* an error because nick → id
// is a real fetch.
//
// # ELI5 example
//
//	plugin "ifunny" {
//	  source = "https://github.com/psyduck-etl/ifunny"
//	}
//
//	produce "ifunny-feed" "featured" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	  feed       = "featured"
//	  stop-after = 20
//	}
//
//	transform "ifunny-author" "author" {
//	  auth-bearer = env.IFUNNY_BEARER
//	  user-agent {
//	    device         = "android"
//	    device-version = "14"
//	  }
//	}
//
//	consume "trash" "trash" {}
//
//	pipeline "feed-to-authors" {
//	  produce   = [produce.ifunny-feed.featured]
//	  transform = [transform.ifunny-author.author]
//	  consume   = [consume.trash.trash]
//	}
package main
