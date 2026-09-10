// Package hhreadsync owns the provider-independent policy for importing HH
// read snapshots into the local career aggregates.
//
// The package deliberately knows neither HTTP nor a persistence
// implementation. Callers provide the read-only HH source and the existing
// domain-specific persistence ports. Backend transaction and batch lifecycles
// remain in the composition root.
package hhreadsync
