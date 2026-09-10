// Package applicationanswer prepares safe answers to application questions.
//
// The service owns application-question prompt construction, structured
// decision parsing, semantic regeneration, and deterministic candidate/story
// validation. It accepts detached snapshots and never reads or writes HH,
// persists drafts or clarifications, mutates Candidate knowledge, or approves
// or sends an answer.
package applicationanswer
