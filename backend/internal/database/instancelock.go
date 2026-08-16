package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// instanceLockKey is the PostgreSQL advisory-lock key that identifies "the
// KrakenHashes backend". The value is arbitrary but must never change, or two
// versions of the backend would each think they hold the singleton.
const instanceLockKey int64 = 0x4B52414B454E4831 // "KRAKENH1"

// defaultInstanceLockWait is how long to keep retrying before giving up. This
// exists for rolling restarts: the outgoing process may still hold the lock
// for a few seconds while it drains, and the incoming process should wait for
// it rather than crash-looping.
const defaultInstanceLockWait = 30 * time.Second

// instanceLockRetryInterval is the poll interval while waiting.
const instanceLockRetryInterval = 2 * time.Second

/*
 * InstanceLock is a session-level PostgreSQL advisory lock that prevents a
 * second backend process from running against the same database.
 *
 * Why this exists: the scheduler's single-flight guard (Cycle.running) is a
 * process-local atomic.Bool, and nothing in the codebase does leader election
 * or SELECT ... FOR UPDATE SKIP LOCKED on the dispatch path. Two backends
 * against one database therefore both run the 3-second cycle and double-
 * dispatch the same keyspace intervals. Once cloud provisioning exists, they
 * would also both act on the same provisioning decision and launch duplicate
 * paid GPU instances — a correctness bug that costs real money.
 *
 * The lock is held on a DEDICATED connection for the life of the process.
 * It cannot be taken from the general pool: database/sql may hand a pooled
 * connection to another query or close it when idle, and a PostgreSQL
 * session-level advisory lock dies with its session — the lock would be
 * released silently while the process kept running.
 */
type InstanceLock struct {
	conn *sql.Conn
	key  int64
}

/*
 * AcquireInstanceLock takes the singleton advisory lock, waiting up to
 * KH_INSTANCE_LOCK_WAIT (default 30s) for an outgoing process to release it.
 *
 * Returns a nil lock and nil error when the guard is disabled via
 * KH_ALLOW_MULTIPLE_INSTANCES, so callers can treat that as "no lock held".
 */
func AcquireInstanceLock(ctx context.Context, sqlDB *sql.DB) (*InstanceLock, error) {
	if allowMultipleInstances() {
		debug.Warning("KH_ALLOW_MULTIPLE_INSTANCES is set: skipping the single-instance guard. " +
			"Running two backends against one database double-dispatches job chunks and, with cloud " +
			"provisioning enabled, can launch duplicate paid GPU instances.")
		return nil, nil
	}

	// Dedicated connection: the lock lives and dies with this session.
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("instance lock: acquire dedicated connection: %w", err)
	}

	deadline := time.Now().Add(instanceLockWait())
	for {
		var acquired bool
		if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, instanceLockKey).Scan(&acquired); err != nil {
			conn.Close()
			return nil, fmt.Errorf("instance lock: pg_try_advisory_lock: %w", err)
		}
		if acquired {
			debug.Info("Single-instance lock acquired (key %d)", instanceLockKey)
			return &InstanceLock{conn: conn, key: instanceLockKey}, nil
		}

		if time.Now().After(deadline) {
			conn.Close()
			return nil, fmt.Errorf(
				"instance lock: another KrakenHashes backend is already running against this database "+
					"(waited %s). Stop it first, or set KH_ALLOW_MULTIPLE_INSTANCES=true if you understand "+
					"that doing so double-dispatches job chunks and can launch duplicate paid cloud instances",
				instanceLockWait())
		}

		debug.Info("Single-instance lock held by another process; retrying in %s", instanceLockRetryInterval)
		select {
		case <-ctx.Done():
			conn.Close()
			return nil, ctx.Err()
		case <-time.After(instanceLockRetryInterval):
		}
	}
}

// Release drops the advisory lock and returns the dedicated connection to the
// pool. Safe to call on a nil lock (the disabled-guard case).
func (l *InstanceLock) Release(ctx context.Context) error {
	if l == nil || l.conn == nil {
		return nil
	}
	// Best-effort unlock. Closing the connection would release it anyway,
	// since the lock is session-scoped, but being explicit makes the intent
	// legible in the Postgres logs.
	if _, err := l.conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, l.key); err != nil {
		debug.Warning("Failed to release single-instance lock (it will drop when the session closes): %v", err)
	}
	err := l.conn.Close()
	l.conn = nil
	return err
}

func allowMultipleInstances() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KH_ALLOW_MULTIPLE_INSTANCES"))) {
	case "true", "1", "yes":
		return true
	}
	return false
}

func instanceLockWait() time.Duration {
	raw := strings.TrimSpace(os.Getenv("KH_INSTANCE_LOCK_WAIT"))
	if raw == "" {
		return defaultInstanceLockWait
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs < 0 {
		debug.Warning("Invalid KH_INSTANCE_LOCK_WAIT %q; using default %s", raw, defaultInstanceLockWait)
		return defaultInstanceLockWait
	}
	return time.Duration(secs) * time.Second
}
