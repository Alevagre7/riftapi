#!/bin/bash
# backup.sh — online backup of the riftapi SQLite database, with
# a 7-day rolling retention window.
#
# Install in cron (run daily at 04:00, an hour after the sync
# timer at 03:00):
#
#   0 4 * * * /opt/riftapi/deploy/backup/backup.sh
#
# All paths are overridable via env vars so the same script works
# in cron, in a one-shot invocation, and in CI.

set -euo pipefail

# --- configuration (override via env) -----------------------------------

BACKUP_DIR="${BACKUP_DIR:-/var/backups/riftapi}"
DB_PATH="${DB_PATH:-/data/riftapi.db}"
RETENTION_DAYS="${RETENTION_DAYS:-7}"

# --- preconditions -------------------------------------------------------

if [ ! -f "$DB_PATH" ]; then
    echo "backup: database file not found at $DB_PATH" >&2
    exit 1
fi

if ! command -v sqlite3 >/dev/null 2>&1; then
    echo "backup: sqlite3 is not installed" >&2
    exit 1
fi

mkdir -p "$BACKUP_DIR"

# --- backup -------------------------------------------------------------

DATE=$(date -u +%Y-%m-%d)
BACKUP_FILE="$BACKUP_DIR/riftapi-$DATE.db"

# SQLite's `.backup` command does a safe online backup: it acquires
# a shared lock on the source, then writes a consistent snapshot to
# the destination. The API keeps reading the live file with WAL
# mode during the backup; the only window where the live file is
# briefly busy is the metadata switch at the end, which is
# sub-millisecond on this dataset.
sqlite3 "$DB_PATH" ".backup '$BACKUP_FILE'"

# Verify the backup is non-empty. A zero-byte file means the backup
# silently failed (e.g. disk full, permission error on the
# destination) and we should not silently delete yesterday's
# backup.
if [ ! -s "$BACKUP_FILE" ]; then
    echo "backup: $BACKUP_FILE is empty" >&2
    exit 1
fi

echo "backup: wrote $BACKUP_FILE ($(stat -c %s "$BACKUP_FILE" 2>/dev/null || stat -f %z "$BACKUP_FILE") bytes)"

# --- prune --------------------------------------------------------------

# Remove backups older than the retention window. `-mtime +N`
# matches files whose data was last modified more than N*24 hours
# ago, so this is "keep at least 7 days".
find "$BACKUP_DIR" -maxdepth 1 -name "riftapi-*.db" -mtime +"$RETENTION_DAYS" -print -delete
