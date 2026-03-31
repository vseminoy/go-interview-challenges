Collection coding challenges across different domains.

# Concurrent Balance Update
Write a function that transfers money from one account to another within a single database. The system is highly loaded, and wallets can be involved in hundreds of transactions per second.
Thought process:
DB: Use transaction.
Race condition: Note that simply UPDATE balance = balance - 100 is insufficient if we first check the balance in the code. We need to use SELECT FOR UPDATE.
Deadlock: If User A transfers to User B, and at the same time B transfers to A, a deadlock will occur. Solution: Always lock records in ascending order of their IDs.
Go to: Use context.Context to continue processing if the connection is lost or timeout.
Solution file: [concurrent_balance_update/transfer.go](concurrent_balance_update/transfer.go)