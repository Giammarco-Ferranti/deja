-- Schema for sqlc code generation only. Not executed at runtime.
--
-- This must stay identical to the DDL in migrations (internal/store/store.go),
-- which is what actually creates the database. `make sqlc-verify` checks that
-- generated code is current; the schema itself is kept in step by hand.
--
-- Note `command text` carries no NOT NULL: that is what AutoMigrate produced for
-- CommandStat, and changing it would mean rebuilding the table on every existing
-- install. See store.go on why that matters.
CREATE TABLE commands (
  id integer PRIMARY KEY AUTOINCREMENT,
  command text NOT NULL,
  directory text NOT NULL,
  timestamp datetime NOT NULL,
  exit_code integer NOT NULL,
  duration_ms integer NOT NULL,
  session_id text NOT NULL
);
CREATE INDEX idx_commands_command ON commands(command);
CREATE INDEX idx_commands_directory ON commands(directory);
CREATE INDEX idx_commands_timestamp ON commands(timestamp);
CREATE INDEX idx_commands_session ON commands(session_id);

CREATE TABLE command_stats (
  command text,
  count integer NOT NULL DEFAULT 0,
  last_used datetime NOT NULL,
  PRIMARY KEY (command)
);
CREATE INDEX idx_command_stats_last_used ON command_stats(last_used);

CREATE TABLE sequences (
  prev_command text NOT NULL,
  next_command text NOT NULL,
  count integer NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX idx_seq_pair ON sequences(prev_command, next_command);
