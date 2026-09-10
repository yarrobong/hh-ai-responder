// Package application owns the career-domain job application aggregate,
// application statuses, events, and intrinsic lifecycle values.
//
// It deliberately does not own persistence, HH negotiation transport,
// conversations, matching AI, reconciliation orchestration, or Dashboard
// concerns. The package may depend on vacancy value types when they are
// persisted as part of an application snapshot.
package application
