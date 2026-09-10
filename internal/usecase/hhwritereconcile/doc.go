// Package hhwritereconcile evaluates read-only evidence collected after an HH
// write attempt. It never exposes or invokes an HH mutation capability.
//
// A missing message is deliberately not treated as proof that the provider
// rejected the write. Callers receive a safe transition intent and decide how
// to persist it.
package hhwritereconcile
