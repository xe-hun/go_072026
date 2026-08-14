package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	db "notes-server/db/generated"
	"notes-server/internal/config"
)

// dbtx is the small common interface implemented by both pgxpool.Pool and
// pgx.Tx. sqlc uses the same shape, which lets the Store swap between pool and
// transaction connections.
type dbtx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store is the persistence boundary used by services. It wraps a pgx pool and
// sqlc Queries while keeping transaction coordination explicit.
type Store struct {
	// pool is the process-wide connection pool.
	pool *pgxpool.Pool
	// conn is either the pool or a transaction. It is retained for future custom
	// SQL helpers that need the shared dbtx interface.
	conn dbtx
	// q is the sqlc-generated query set bound to conn.
	q *db.Queries
	// inTx marks Store values that already run inside a PostgreSQL transaction.
	inTx bool
	// savepointSeq creates transaction-local savepoint names for partial
	// rollbacks.
	savepointSeq int
}

// Open builds and validates the PostgreSQL pool from runtime configuration.
func Open(ctx context.Context, cfg config.Config) (*Store, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	poolConfig.MaxConns = cfg.MaxDBConnections
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		// Fail fast: readiness should not depend on a lazy first query.
		pool.Close()
		return nil, err
	}
	return New(pool), nil
}

// New wraps an existing pool. Tests can use this when they create their own
// pgxpool instance.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, conn: pool, q: db.New(pool)}
}

// Pool exposes the underlying pool for low-level readiness or metrics hooks.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// Close releases database resources owned by the process.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Ping verifies PostgreSQL connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// WithTx runs fn inside a PostgreSQL transaction. If called on an existing
// transaction store, it reuses that transaction instead of nesting.
func (s *Store) WithTx(ctx context.Context, fn func(*Store) error) error {
	if s.inTx {
		// This prevents accidentally opening a second transaction inside sync or
		// worker code that already owns one.
		return fn(s)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	txStore := &Store{pool: s.pool, conn: tx, q: s.q.WithTx(tx), inTx: true}
	if err := fn(txStore); err != nil {
		// Rollback errors are attached to the original error because the original
		// error explains why the transaction failed.
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return fmt.Errorf("%w: rollback failed: %v", err, rollbackErr)
		}
		return err
	}
	return tx.Commit(ctx)
}

// WithSavepoint runs fn inside a PostgreSQL savepoint. It must be called from a
// transaction store and lets callers roll back one unit of work without aborting
// the parent transaction.
func (s *Store) WithSavepoint(ctx context.Context, fn func(*Store) error) error {
	if !s.inTx {
		return s.WithTx(ctx, fn)
	}

	s.savepointSeq++
	name := fmt.Sprintf("sync_savepoint_%d", s.savepointSeq)
	if _, err := s.conn.Exec(ctx, "SAVEPOINT "+name); err != nil {
		return err
	}
	if err := fn(s); err != nil {
		if _, rollbackErr := s.conn.Exec(ctx, "ROLLBACK TO SAVEPOINT "+name); rollbackErr != nil {
			return fmt.Errorf("%w: rollback to savepoint failed: %v", err, rollbackErr)
		}
		if _, releaseErr := s.conn.Exec(ctx, "RELEASE SAVEPOINT "+name); releaseErr != nil {
			return fmt.Errorf("%w: release savepoint failed: %v", err, releaseErr)
		}
		return err
	}
	if _, err := s.conn.Exec(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		return err
	}
	return nil
}

// mapNoRows converts pgx's driver-specific no-row error into the package
// sentinel used by services.
func mapNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
