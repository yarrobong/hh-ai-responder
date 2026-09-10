// Package writeapproval owns the deterministic local evidence boundary for
// controlled HH draft actions.
//
// It creates and validates approval evidence, compares a reviewed artifact
// with the current local snapshot, and issues the existing one-shot nonce.
// It deliberately has no HH reader/writer, gateway, AI, or concrete storage
// dependency. Remote preflight and action/nonce consumption remain outside
// this package.
package writeapproval
