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
//   - ifunny-author         — [authorTransformer]
//   - ifunny-tags           — [tagsTransformer]
//   - ifunny-lookup-content — [lookupContent]
//   - ifunny-lookup-user    — [lookupUser]
//   - ifunny-lookup-channel — [lookupChannel]
//
// # Authentication
//
// Every API-backed resource takes a user-agent block plus exactly one of
// auth-basic (anonymous — a literal primed token, "generate", or
// "generate-cache") or auth-bearer (logged-in user's OAuth token; required
// for the chat resources). See [authConfig] and [userAgentConfig] for the
// full surface.
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
//	transform "ifunny-author" "author" {}
//
//	consume "trash" "trash" {}
//
//	pipeline "feed-to-authors" {
//	  produce   = [produce.ifunny-feed.featured]
//	  transform = [transform.ifunny-author.author]
//	  consume   = [consume.trash.trash]
//	}
package main
