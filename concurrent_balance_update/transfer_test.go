package wallet

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newStore(t *testing.T) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db), mock
}

// TestTransfer_ValidationErrors verifies that Transfer rejects invalid input
// before touching the database: same-account transfer, zero amount, and negative amount.
func TestTransfer_ValidationErrors(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if err := s.Transfer(ctx, 1, 1, 100); !errors.Is(err, ErrSameAccount) {
		t.Errorf("expected ErrSameAccount, got %v", err)
	}
	if err := s.Transfer(ctx, 1, 2, 0); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("expected ErrInvalidAmount for zero amount, got %v", err)
	}
	if err := s.Transfer(ctx, 1, 2, -50); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("expected ErrInvalidAmount for negative amount, got %v", err)
	}
}

// TestTransfer_BeginTxError verifies that Transfer propagates the error
// when the database is unreachable and the transaction cannot be opened.
func TestTransfer_BeginTxError(t *testing.T) {
	s, mock := newStore(t)
	ctx := context.Background()

	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	if err := s.Transfer(ctx, 1, 2, 100); err == nil {
		t.Error("expected error when BeginTx fails")
	}
}

// TestTransfer_InsufficientFunds verifies that Transfer returns ErrInsufficientFunds
// when the source account balance is lower than the requested amount, and the transaction is rolled back.
func TestTransfer_InsufficientFunds(t *testing.T) {
	s, mock := newStore(t)
	ctx := context.Background()

	mock.ExpectBegin()
	rows := sqlmock.NewRows([]string{"id", "balance"}).
		AddRow(int64(1), int64(50)).
		AddRow(int64(2), int64(1000))
	mock.ExpectQuery(`SELECT a\.id, a\.balance`).
		WithArgs(int64(1), int64(2)).
		WillReturnRows(rows)
	mock.ExpectRollback()

	if err := s.Transfer(ctx, 1, 2, 100); !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}
}

// TestTransfer_Success verifies the happy path: both accounts exist, balance is sufficient,
// two UPDATEs are executed, and the transaction is committed.
func TestTransfer_Success(t *testing.T) {
	s, mock := newStore(t)
	ctx := context.Background()

	mock.ExpectBegin()
	rows := sqlmock.NewRows([]string{"id", "balance"}).
		AddRow(int64(1), int64(500)).
		AddRow(int64(2), int64(200))
	mock.ExpectQuery(`SELECT a\.id, a\.balance`).
		WithArgs(int64(1), int64(2)).
		WillReturnRows(rows)
	mock.ExpectExec(`UPDATE accounts SET balance = balance - \$1 WHERE id = \$2`).
		WithArgs(int64(100), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE accounts SET balance = balance \+ \$1 WHERE id = \$2`).
		WithArgs(int64(100), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := s.Transfer(ctx, 1, 2, 100); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestTransfer_IDSortingForDeadlockPrevention verifies the anti-deadlock invariant:
// when transferring from a higher ID to a lower one, the database lock is still acquired
// in ascending ID order (1 then 5), regardless of the argument order.
func TestTransfer_IDSortingForDeadlockPrevention(t *testing.T) {
	s, mock := newStore(t)
	ctx := context.Background()

	// Transfer from high ID to low ID — lock query must still use (1, 5) not (5, 1).
	mock.ExpectBegin()
	rows := sqlmock.NewRows([]string{"id", "balance"}).
		AddRow(int64(1), int64(1000)).
		AddRow(int64(5), int64(200))
	mock.ExpectQuery(`SELECT a\.id, a\.balance`).
		WithArgs(int64(1), int64(5)). // sorted ascending
		WillReturnRows(rows)
	mock.ExpectExec(`UPDATE accounts SET balance = balance - \$1 WHERE id = \$2`).
		WithArgs(int64(50), int64(5)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE accounts SET balance = balance \+ \$1 WHERE id = \$2`).
		WithArgs(int64(50), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := s.Transfer(ctx, 5, 1, 50); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestTransfer_AccountNotFound verifies that Transfer returns ErrAccountNotFound
// when SELECT FOR UPDATE returns only one row, meaning one of the accounts does not exist.
func TestTransfer_AccountNotFound(t *testing.T) {
	s, mock := newStore(t)
	ctx := context.Background()

	mock.ExpectBegin()
	rows := sqlmock.NewRows([]string{"id", "balance"}).
		AddRow(int64(1), int64(500)) // only one account returned
	mock.ExpectQuery(`SELECT a\.id, a\.balance`).
		WithArgs(int64(1), int64(99)).
		WillReturnRows(rows)
	mock.ExpectRollback()

	if err := s.Transfer(ctx, 1, 99, 100); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("expected ErrAccountNotFound, got %v", err)
	}
}
