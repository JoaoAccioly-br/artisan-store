package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joao-accioly/artisan-store/api/internal/repository/sqlc"
)

// WithTx executa uma função dentro de uma transação PostgreSQL usando queries geradas pelo sqlc,
// fazendo rollback automático em erro/panic e commit se bem-sucedida.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(q *sqlc.Queries) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				panic(fmt.Errorf("panic (%v) and rollback error (%w)", p, rbErr))
			}
			panic(p)
		}
	}()

	q := sqlc.New(tx)
	if err := fn(q); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("tx error (%w) and rollback error (%w)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
