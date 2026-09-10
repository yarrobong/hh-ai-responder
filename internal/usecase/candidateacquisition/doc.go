// Package candidateacquisition owns the deterministic Candidate Knowledge
// acquisition workflow. It decides which supplied Candidate evidence gaps are
// actionable, how to ask about them, and which typed mutation intent an answer
// represents. Persistence, AI transport, and mutation execution stay outside
// this package.
package candidateacquisition
