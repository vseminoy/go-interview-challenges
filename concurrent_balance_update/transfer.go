package wallet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Sentinel errors allow callers to distinguish failure reasons via errors.Is.
var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrSameAccount       = errors.New("cannot transfer to self")
	ErrInvalidAmount     = errors.New("invalid amount")
	ErrAccountNotFound   = errors.New("one or both accounts not found")
)

// Store provides access to the accounts table.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store backed by the given database connection pool.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Transfer moves amount (in kopecks) from one account to another.
func (s *Store) Transfer(ctx context.Context, fromID, toID int64, amount int64) error {
	// 1. Validation
	if fromID == toID {
		return ErrSameAccount
	}
	if amount <= 0 {
		return ErrInvalidAmount
	}

	// 2. Transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 3. Querying data and locking rows in the database
	// WHY: To optimize the Prepared Statements cache.
	// If we pass (1, 2) and another query (2, 1), for the DB these may be different queries.
	// Sorting here ensures that the structure of the VALUES ($1), ($2) query is always identical.
	firstID, secondID := fromID, toID
	if fromID > toID {
		firstID, secondID = toID, fromID
	}

	// Subquery (SELECT FROM VALUES ... ORDER BY)
	// WHY: This is a GUARANTEE of the order of row locking in the database.
	// A simple "WHERE id IN (1, 2) FOR UPDATE" can lock rows in random order
	// (depending on how they are located on disk or in the index).
	// Using VALUES with ORDER BY forces Postgres to first create a sorted list in memory,
	// and then strictly sequentially (from smallest to largest) apply locks (Lock) to the rows of the table.
	// This 100% eliminates the risk of Deadlock.
	query := `
		SELECT a.id, a.balance
		FROM (
			SELECT x FROM (VALUES ($1::int8), ($2::int8)) AS t(x)
			ORDER BY x
		) AS sorted
		JOIN accounts a ON a.id = sorted.x
		FOR UPDATE`

	rows, err := tx.QueryContext(ctx, query, firstID, secondID)
	if err != nil {
		return fmt.Errorf("failed to lock accounts: %w", err)
	}
	defer rows.Close()

	var fromBalance int64
	var foundFrom, foundTo bool

	// We read both lines, blocked in strict order
	for rows.Next() {
		var id int64
		var bal int64
		if err := rows.Scan(&id, &bal); err != nil {
			return fmt.Errorf("failed to scan account row: %w", err)
		}
		if id == fromID {
			fromBalance = bal
			foundFrom = true
		}
		if id == toID {
			foundTo = true
		}
	}

	// Important: Check if the connection was broken while reading lines
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error during rows iteration: %w", err)
	}

	// We check that both accounts exist.
	if !foundFrom || !foundTo {
		return ErrAccountNotFound
	}

	// 4. Business logic
	// The data is already current and blocked for other transactions.
	if fromBalance < amount {
		return ErrInsufficientFunds
	}

	// 5. Updates
	// Executed within the same transaction.
	_, err = tx.ExecContext(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2", amount, fromID)
	if err != nil {
		return fmt.Errorf("failed to debit: %w", err)
	}

	_, err = tx.ExecContext(ctx, "UPDATE accounts SET balance = balance + $1 WHERE id = $2", amount, toID)
	if err != nil {
		return fmt.Errorf("failed to credit: %w", err)
	}

	// 6. Fixation
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}
