// Package candidatesemanticsearch owns read-only semantic retrieval for a
// canonical Candidate. It embeds the caller's query, performs a scoped
// semantic repository search, and accepts only results that still match the
// current deterministic semantic document policy.
//
// Retrieval results are relevance hints, not Candidate facts. This package
// never mutates Candidate state, promotes truth, or repairs the derived index.
package candidatesemanticsearch
