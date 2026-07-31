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

type dbtx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct {
	pool *pgxpool.Pool
	conn dbtx
	q    *db.Queries
	inTx bool
}

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
		pool.Close()
		return nil, err
	}
	return New(pool), nil
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, conn: pool, q: db.New(pool)}
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) WithTx(ctx context.Context, fn func(*Store) error) error {
	if s.inTx {
		return fn(s)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	txStore := &Store{pool: s.pool, conn: tx, q: s.q.WithTx(tx), inTx: true}
	if err := fn(txStore); err != nil {
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return fmt.Errorf("%w: rollback failed: %v", err, rollbackErr)
		}
		return err
	}
	return tx.Commit(ctx)
}

func mapNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
