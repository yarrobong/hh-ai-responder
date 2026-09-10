// Package employerreply prepares safe employer-facing reply decisions.
//
// It owns prompt construction, completion orchestration, structured business
// parsing, semantic regeneration, and deterministic reply validation. It does
// not read or write HH, persist drafts, mutate Candidate knowledge, or approve
// or send a reply.
package employerreply
