package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	postgresstorage "hh-ai-responder/internal/adapters/storage/postgres"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
)

func buildAutomaticApplicationAttemptStore(ctx context.Context, cfg Config, backend string, postgresPool *pgxpool.Pool) (attemptport.Store, error) {
	if backend == storageBackendJSON {
		path := automaticApplicationAttemptsPath(cfg)
		repository := jsonstorage.NewApplicationAttemptRepository(path)
		if err := repository.Load(); err != nil {
			// Return the repository as well: subsequent live Reserve calls will
			// fail closed on the same unreadable file; no writer is reachable.
			return repository, err
		}
		return repository, nil
	}
	if postgresPool == nil {
		return nil, errors.New("postgres application attempt pool is not configured")
	}
	if err := ApplyPostgresMigrations(ctx, postgresPool); err != nil {
		return nil, err
	}
	return postgresstorage.NewApplicationAttemptRepository(postgresPool), nil
}

func automaticApplicationAttemptsPath(cfg Config) string {
	base := strings.TrimSpace(cfg.CandidateProfilePath)
	if base == "" {
		base = strings.TrimSpace(cfg.AlreadyRespondedStatePath)
	}
	if base == "" {
		return jsonstorage.ApplicationAttemptsFilename
	}
	return filepath.Join(filepath.Dir(base), jsonstorage.ApplicationAttemptsFilename)
}
