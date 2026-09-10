// Package applicationsubmission owns one prepared automatic application
// submission attempt.
//
// Preparation data is deliberately detached at this boundary: it is an
// input to fresh provider checks, not a write grant. The package owns neither
// provider HTTP nor local batch counters. Mutations are exposed only through
// the consumer-owned ApplicationExecutor capability.
package applicationsubmission
