# PostgreSQL migration authority

`internal/runtime/migrations/` is the authoritative PostgreSQL migration
directory. `internal/runtime/postgres.go` embeds this directory and applies
its `*.up.sql` files in deterministic filename/version order. The database
bookkeeping table is `schema_migrations`.

The root `migrations/` directory is a compatibility mirror for operators,
inspection tools and existing repository conventions. Every SQL file must be
present in both directories with identical bytes and every migration must have
paired `up` and `down` files. `internal/runtime/migration_authority_test.go`
enforces this contract.

When adding a migration:

1. Add the paired files to `internal/runtime/migrations/`.
2. Copy the same files to the root `migrations/` mirror.
3. Keep the next numeric version strictly ordered and use additive SQL unless
   a separately reviewed compatibility change is required.
4. Run the migration authority and repository contract tests.

Existing databases remain compatible because already-applied versions continue
to be identified by their numeric `schema_migrations.version`; synchronizing
the mirror does not reapply those migrations.
