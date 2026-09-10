// Package candidatemutation owns deterministic candidate-knowledge mutation
// semantics. It has no persistence, network, AI, or application dependencies.
// Storage adapters prepare domain values, apply the returned decisions, and
// persist them using their existing atomicity guarantees.
package candidatemutation
