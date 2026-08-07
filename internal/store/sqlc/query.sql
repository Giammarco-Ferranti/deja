-- name: InsertCommand :execlastid
INSERT INTO commands (command, directory, timestamp, exit_code, duration_ms, session_id)
VALUES (?, ?, ?, ?, ?, ?);

-- Adds to the existing count rather than replacing it, and keeps the later of
-- the two timestamps. MAX over the stored text works because the driver writes
-- every time in the same sortable layout.
-- name: UpsertCommandStat :exec
INSERT INTO command_stats (command, count, last_used) VALUES (?, ?, ?)
ON CONFLICT(command) DO UPDATE SET
  count = command_stats.count + excluded.count,
  last_used = MAX(command_stats.last_used, excluded.last_used);

-- name: UpsertSequence :exec
INSERT INTO sequences (prev_command, next_command, count) VALUES (?, ?, ?)
ON CONFLICT(prev_command, next_command) DO UPDATE SET
  count = sequences.count + excluded.count;

-- name: ListCommandStats :many
SELECT command, count, last_used FROM command_stats ORDER BY count DESC;

-- name: SequenceCountsFor :many
SELECT next_command, count FROM sequences WHERE prev_command = ? ORDER BY count DESC;

-- name: DirCountsForCommand :many
SELECT directory, COUNT(*) AS n FROM commands WHERE command = ? GROUP BY directory;
