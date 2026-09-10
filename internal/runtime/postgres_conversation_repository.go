package runtime

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
)

// PostgresConversationRepository is a root compatibility alias. The
// authoritative Conversation SQL, scanners, codecs, and error translation
// live in the PostgreSQL storage adapter.
type PostgresConversationRepository = postgresstorage.ConversationRepository

var ErrDuplicateConversationExternal = postgresstorage.ErrDuplicateConversationExternal

func NewPostgresConversationRepository(pool *pgxpool.Pool) *PostgresConversationRepository {
	return postgresstorage.NewConversationRepository(pool)
}

func newPostgresConversationRepositoryTx(tx pgx.Tx) *PostgresConversationRepository {
	return postgresstorage.NewConversationRepositoryForTx(tx)
}
