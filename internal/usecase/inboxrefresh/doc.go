// Package inboxrefresh owns one bounded, read-only inbox refresh iteration.
//
// Scheduling, coalescing, persistence adapters, HH writes, candidate truth
// mutation, and direct AI access remain outside this package.
package inboxrefresh
