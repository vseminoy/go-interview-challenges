Collection of coding challenges across different domains.

---

## Concurrent Balance Update

**Problem:** Write a function that transfers money from one account to another. The system is highly loaded — wallets can be involved in hundreds of transactions per second.

**Key constraints:**
- Amounts are stored as integers (kopecks) to avoid floating-point rounding errors.
- Must be safe under concurrent load (no race conditions, no deadlocks).
- Must handle connection loss / timeout via `context.Context`.

**Solution:** [`concurrent_balance_update/transfer.go`](concurrent_balance_update/transfer.go)

**Design decisions:**

| Problem | Solution |
|---|---|
| Floating-point money | `int64` kopecks — exact arithmetic |
| Race condition on balance check | `SELECT FOR UPDATE` inside a transaction — DB holds the lock until commit |
| Deadlock (A→B concurrent with B→A) | Always lock rows in ascending ID order using a sorted `VALUES` subquery |
| Prepared-statement cache miss | Sorting IDs before the query ensures the same query shape regardless of direction |
| Mid-read connection failure | `rows.Err()` checked after iteration |
