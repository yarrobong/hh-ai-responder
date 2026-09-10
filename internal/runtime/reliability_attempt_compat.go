package runtime

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
	applicationport "hh-ai-responder/internal/ports/applicationattempt"
	autochatport "hh-ai-responder/internal/ports/autochatattempt"
)

// These builders intentionally do not apply migrations. Inspection must read
// the durable state that exists and surface an unavailable/missing store to
// the operator rather than changing the schema during a GET or CLI read.
func buildApplicationAttemptReader(cfg Config, backend string, postgresPool *pgxpool.Pool) (applicationport.Reader, error) {
	if backend == storageBackendJSON {
		return jsonstorage.NewApplicationAttemptRepository(automaticApplicationAttemptsPath(cfg)), nil
	}
	if postgresPool == nil {
		return nil, errors.New("postgres application attempt pool is not configured")
	}
	return postgresstorage.NewApplicationAttemptRepository(postgresPool), nil
}

func buildAutoChatAttemptReader(cfg Config, backend string, postgresPool *pgxpool.Pool) (autochatport.Reader, error) {
	if backend == storageBackendJSON {
		return jsonstorage.NewAutoChatAttemptRepository(automaticAutoChatAttemptsPath(cfg)), nil
	}
	if postgresPool == nil {
		return nil, errors.New("postgres auto-chat attempt pool is not configured")
	}
	return postgresstorage.NewAutoChatAttemptRepository(postgresPool), nil
}

// Reconciliation stores deliberately do not apply migrations. A dashboard
// GET or a single operator read must not change the database schema; normal
// runtime/storage setup remains responsible for applying migrations.
func buildApplicationAttemptReconciliationStore(cfg Config, backend string, postgresPool *pgxpool.Pool) (applicationport.Store, error) {
	if backend == storageBackendJSON {
		return jsonstorage.NewApplicationAttemptRepository(automaticApplicationAttemptsPath(cfg)), nil
	}
	if postgresPool == nil {
		return nil, errors.New("postgres application attempt pool is not configured")
	}
	return postgresstorage.NewApplicationAttemptRepository(postgresPool), nil
}

func buildAutoChatAttemptReconciliationStore(cfg Config, backend string, postgresPool *pgxpool.Pool) (autochatport.Store, error) {
	if backend == storageBackendJSON {
		return jsonstorage.NewAutoChatAttemptRepository(automaticAutoChatAttemptsPath(cfg)), nil
	}
	if postgresPool == nil {
		return nil, errors.New("postgres auto-chat attempt pool is not configured")
	}
	return postgresstorage.NewAutoChatAttemptRepository(postgresPool), nil
}
