// Package followupdraft prepares a validated follow-up draft decision.
//
// It owns one follow-up generation attempt, its prompt, structured completion
// parsing, semantic retry, and deterministic generated-text validation. It
// does not own follow-up eligibility policy, fresh-state checks, persistence,
// approval, delivery, HH access, or Candidate mutation.
package followupdraft
