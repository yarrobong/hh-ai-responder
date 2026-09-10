package runtime

import "hh-ai-responder/internal/ports"

// Compile-time checks cover legacy root compatibility wrappers and the
// unchanged PostgreSQL implementations. The authoritative JSON application
// implementation is asserted in internal/adapters/storage/json.
var (
	_ ports.CandidateReader         = (*PostgresCandidateRepository)(nil)
	_ ports.CandidateWriter         = (*PostgresCandidateRepository)(nil)
	_ ports.CandidateMutationWriter = (*PostgresCandidateRepository)(nil)
	_ ports.ApplicationStore        = (*JSONApplicationRepository)(nil)
	_ ports.ApplicationStore        = (*PostgresApplicationRepository)(nil)
	_ ports.ConversationStore       = (*PostgresConversationRepository)(nil)
)
