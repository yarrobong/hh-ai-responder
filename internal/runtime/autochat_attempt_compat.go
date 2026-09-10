package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
	autochatattemptport "hh-ai-responder/internal/ports/autochatattempt"
)

func buildAutoChatAttemptStore(ctx context.Context, cfg Config, backend string, postgresPool *pgxpool.Pool) (autochatattemptport.Store, error) {
	if backend == storageBackendJSON {
		repository := jsonstorage.NewAutoChatAttemptRepository(automaticAutoChatAttemptsPath(cfg))
		if err := repository.Load(); err != nil {
			// Keep the repository object so every later operation continues to
			// fail closed on the same corrupt/unreadable file.
			return repository, err
		}
		return repository, nil
	}
	if postgresPool == nil {
		return nil, errors.New("postgres auto-chat attempt pool is not configured")
	}
	if err := ApplyPostgresMigrations(ctx, postgresPool); err != nil {
		return nil, err
	}
	return postgresstorage.NewAutoChatAttemptRepository(postgresPool), nil
}

func automaticAutoChatAttemptsPath(cfg Config) string {
	base := strings.TrimSpace(cfg.CandidateProfilePath)
	if base == "" {
		base = strings.TrimSpace(cfg.AlreadyRespondedStatePath)
	}
	if base == "" {
		return jsonstorage.AutoChatAttemptsFilename
	}
	return filepath.Join(filepath.Dir(base), jsonstorage.AutoChatAttemptsFilename)
}
