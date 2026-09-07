package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	storageBackendJSON     = "json"
	storageBackendPostgres = "postgres"
)

// PostgresConfig contains connection and pool settings. DatabaseURL is kept
// as a DSN and is never written to logs by this package.
type PostgresConfig struct {
	DatabaseURL     string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// OpenPostgres creates a checked, ready-to-use connection pool. The caller
// owns the returned pool and must call Close when the process is done.
func OpenPostgres(ctx context.Context, config PostgresConfig) (*pgxpool.Pool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(config.DatabaseURL) == "" {
		return nil, errors.New("postgres database URL is required")
	}
	if config.MaxConns < 0 || config.MinConns < 0 {
		return nil, errors.New("postgres pool connection limits must not be negative")
	}
	if config.MaxConns > 0 && config.MinConns > config.MaxConns {
		return nil, errors.New("postgres min connections must not exceed max connections")
	}

	poolConfig, err := pgxpool.ParseConfig(strings.TrimSpace(config.DatabaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse postgres connection config: %w", err)
	}
	if config.MaxConns > 0 {
		poolConfig.MaxConns = config.MaxConns
	}
	if config.MinConns > 0 {
		poolConfig.MinConns = config.MinConns
	}
	if config.MaxConnLifetime > 0 {
		poolConfig.MaxConnLifetime = config.MaxConnLifetime
	}
	if config.MaxConnIdleTime > 0 {
		poolConfig.MaxConnIdleTime = config.MaxConnIdleTime
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

//go:embed migrations/*.sql
var postgresMigrationFiles embed.FS

// ApplyPostgresMigrations applies embedded, ordered migrations exactly once.
// The bookkeeping table is the only schema object created outside a migration.
func ApplyPostgresMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	ctx = postgresContext(ctx)
	if err := repositoryContextErr(ctx); err != nil {
		return err
	}
	if pool == nil {
		return errors.New("postgres pool is required")
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL
	)`); err != nil {
		return fmt.Errorf("create postgres migration table: %w", err)
	}

	entries, err := fs.Glob(postgresMigrationFiles, "migrations/*.up.sql")
	if err != nil {
		return fmt.Errorf("list postgres migrations: %w", err)
	}
	sort.Strings(entries)
	for _, entry := range entries {
		version, err := migrationVersion(entry)
		if err != nil {
			return err
		}
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("check postgres migration %d: %w", version, err)
		}
		if applied {
			continue
		}
		sql, err := postgresMigrationFiles.ReadFile(entry)
		if err != nil {
			return fmt.Errorf("read postgres migration %d: %w", version, err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin postgres migration %d: %w", version, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply postgres migration %d: %w", version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES ($1, now()) ON CONFLICT (version) DO NOTHING`, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record postgres migration %d: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit postgres migration %d: %w", version, err)
		}
	}
	return nil
}

func postgresContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func migrationVersion(name string) (int64, error) {
	base := name[strings.LastIndex(name, "/")+1:]
	parts := strings.SplitN(base, "_", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid postgres migration filename %q", name)
	}
	version, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("invalid postgres migration version %q", parts[0])
	}
	return version, nil
}

// BuildVacancyRepository is the composition-root selection point. All
// application code below this boundary receives the VacancyRepository only.
func BuildVacancyRepository(ctx context.Context, config Config, jsonStore *VacancyStore) (VacancyRepository, func(), error) {
	backend, err := normalizeStorageBackend(config.StorageBackend)
	if err != nil {
		return nil, func() {}, err
	}
	if backend == storageBackendJSON {
		if jsonStore == nil {
			return nil, func() {}, errors.New("json vacancy store is required")
		}
		return NewJSONVacancyRepository(jsonStore), func() {}, nil
	}
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: config.DatabaseURL})
	if err != nil {
		return nil, func() {}, err
	}
	if err := ApplyPostgresMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, func() {}, err
	}
	repository := NewPostgresVacancyRepository(pool)
	return repository, pool.Close, nil
}

// CareerRepositories is the composition-root bundle for the migrated career
// data. A backend is selected once for all three repositories, preventing a
// PostgreSQL vacancy repository from being paired with JSON applications or
// conversations by accident.
type CareerRepositories struct {
	Vacancies     VacancyRepository
	Applications  ApplicationRepository
	Conversations ConversationRepository
	Postgres      *PostgresCareerStore
}

// BuildCareerRepositories preserves the JSON compatibility path while making
// the PostgreSQL path all-or-nothing for vacancy/application/conversation
// data. The returned close function owns only resources opened by this call.
func BuildCareerRepositories(ctx context.Context, config Config, vacancies *VacancyStore, applications *ApplicationStore, conversations *ConversationStore) (CareerRepositories, func(), error) {
	backend, err := normalizeStorageBackend(config.StorageBackend)
	if err != nil {
		return CareerRepositories{}, func() {}, err
	}
	if backend == storageBackendJSON {
		if vacancies == nil || applications == nil || conversations == nil {
			return CareerRepositories{}, func() {}, errors.New("json career stores are required")
		}
		return CareerRepositories{
			Vacancies: NewJSONVacancyRepository(vacancies), Applications: NewJSONApplicationRepository(applications),
			Conversations: NewJSONConversationRepository(conversations),
		}, func() {}, nil
	}
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: config.DatabaseURL})
	if err != nil {
		return CareerRepositories{}, func() {}, err
	}
	if err := ApplyPostgresMigrations(ctx, pool); err != nil {
		pool.Close()
		return CareerRepositories{}, func() {}, err
	}
	store := NewPostgresCareerStore(pool)
	return CareerRepositories{
		Vacancies: NewPostgresVacancyRepository(pool), Applications: NewPostgresApplicationRepository(pool),
		Conversations: NewPostgresConversationRepository(pool), Postgres: store,
	}, pool.Close, nil
}

func normalizeStorageBackend(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = storageBackendJSON
	}
	switch value {
	case storageBackendJSON, storageBackendPostgres:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported storage backend %q: use json or postgres", value)
	}
}
