package store

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Command struct {
	ID         uint      `gorm:"primaryKey"`
	Command    string    `gorm:"not null;index:idx_commands_command"`
	Directory  string    `gorm:"not null;index:idx_commands_directory"`
	Timestamp  time.Time `gorm:"not null;index:idx_commands_timestamp"`
	ExitCode   int       `gorm:"not null"`
	DurationMS int       `gorm:"not null"`
	SessionID  string    `gorm:"not null;index:idx_commands_session"`
}

func (Command) TableName() string { return "commands" }

type CommandStat struct {
	Command  string    `gorm:"primaryKey;size:2048"` // command text as PK
	Count    int       `gorm:"not null;default:0"`
	LastUsed time.Time `gorm:"not null;index:idx_command_stats_last_used"`
}

func (CommandStat) TableName() string { return "command_stats" }

type Sequence struct {
	PrevCommand string `gorm:"size:2048;not null;uniqueIndex:idx_seq_pair"`
	NextCommand string `gorm:"size:2048;not null;uniqueIndex:idx_seq_pair"`
	Count       int    `gorm:"not null;default:0"`
}

func (Sequence) TableName() string { return "sequences" }

func InitDB(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	// Lock the database down here, before any statement touches it. gorm.Open
	// created the file with SQLite's default 0644&^umask, and it holds shell
	// history in plaintext, so it gets the same treatment zsh gives
	// ~/.zsh_history.
	//
	// Placement matters. SQLite creates the -wal and -shm sidecars lazily, on
	// the first real work against the database (AutoMigrate below), and gives
	// them the main file's mode as it stands at that moment. Tightening first
	// means they are born 0600; tightening at the end of InitDB instead leaves
	// them created 0644 and only fixed afterwards. Verified both ways.
	if err := restrictDBFiles(path); err != nil {
		return nil, err
	}

	if err := db.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if err := db.Exec("PRAGMA busy_timeout=5000;").Error; err != nil {
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}

	if err := db.AutoMigrate(&Command{}, &CommandStat{}, &Sequence{}); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	return db, nil
}

// restrictDBFiles makes the database owner-only.
//
// The main file always exists by the time this runs. The sidecars usually do
// not (see InitDB on when they appear), so they are chmod'd best-effort — that
// branch is what repairs a database left behind by an earlier version, where a
// -wal from an unclean shutdown can hold the most recent commands at 0644.
func restrictDBFiles(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0o600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("chmod %s: %w", path+suffix, err)
		}
	}
	return nil
}

// Keeps multi-row INSERTs under SQLite's SQLITE_MAX_VARIABLE_NUMBER
// (999 on older builds). Widest row is Command at 7 columns, so 100
// rows ≈ 700 host params.
const importBatchSize = 100

func SaveImportBatch(db *gorm.DB, commands []Command) error {
	if len(commands) == 0 {
		return nil
	}
	seqRows := createSeq(commands)
	statsRows := createCommandStat(commands)

	return db.Transaction(func(tx *gorm.DB) error {
		tx = tx.Session(&gorm.Session{CreateBatchSize: importBatchSize})

		if err := tx.Create(&commands).Error; err != nil {
			return err
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "command"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"count":     gorm.Expr("command_stats.count + excluded.count"),
				"last_used": gorm.Expr("MAX(command_stats.last_used, excluded.last_used)"),
			}),
		}).Create(&statsRows).Error; err != nil {
			return err
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "prev_command"},
				{Name: "next_command"},
			},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"count": gorm.Expr("sequences.count + excluded.count"),
			}),
		}).Create(&seqRows).Error; err != nil {
			return err
		}

		return nil
	})
}

type seqKey struct {
	prev string
	next string
}

func createSeq(commands []Command) []Sequence {
	if len(commands) < 2 {
		return nil
	}

	counts := make(map[seqKey]int)

	for i := 1; i < len(commands); i++ {
		prev := strings.TrimSpace(commands[i-1].Command)
		next := strings.TrimSpace(commands[i].Command)

		if prev == "" || next == "" {
			continue
		}

		k := seqKey{prev: prev, next: next}
		counts[k]++
	}

	out := make([]Sequence, 0, len(counts))
	for k, n := range counts {
		out = append(out, Sequence{
			PrevCommand: k.prev,
			NextCommand: k.next,
			Count:       n,
		})
	}
	return out

}

func createCommandStat(commands []Command) []CommandStat {
	if len(commands) == 0 {
		return nil
	}
	counts := map[string]int{}
	lastUsed := map[string]time.Time{}

	for _, c := range commands {
		cmd := strings.TrimSpace(c.Command)
		if cmd == "" {
			continue
		}
		counts[cmd]++
		if t, ok := lastUsed[cmd]; !ok || c.Timestamp.After(t) {
			lastUsed[cmd] = c.Timestamp
		}
	}

	out := make([]CommandStat, 0, len(counts))
	for cmd, n := range counts {
		out = append(out, CommandStat{
			Command:  cmd,
			Count:    n,
			LastUsed: lastUsed[cmd],
		})
	}
	return out
}

// RecordCommand inserts a single command row and upserts the derived
// command_stats + sequences rows in one transaction. prevCommand may be empty
// (the very first command in a session has no predecessor) — in that case
// the sequences upsert is skipped.
func RecordCommand(db *gorm.DB, cmd Command, prevCommand string) error {
	key := strings.TrimSpace(cmd.Command)
	if key == "" {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&cmd).Error; err != nil {
			return err
		}

		stat := CommandStat{
			Command:  key,
			Count:    1,
			LastUsed: cmd.Timestamp,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "command"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"count":     gorm.Expr("command_stats.count + 1"),
				"last_used": gorm.Expr("MAX(command_stats.last_used, excluded.last_used)"),
			}),
		}).Create(&stat).Error; err != nil {
			return err
		}

		prev := strings.TrimSpace(prevCommand)
		if prev == "" {
			return nil
		}
		seq := Sequence{
			PrevCommand: prev,
			NextCommand: key,
			Count:       1,
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "prev_command"},
				{Name: "next_command"},
			},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"count": gorm.Expr("sequences.count + 1"),
			}),
		}).Create(&seq).Error
	})
}

func GetCommandStats(db *gorm.DB) ([]CommandStat, error) {
	var commands []CommandStat
	if err := db.Order("count DESC").Find(&commands).Error; err != nil {
		return nil, err
	}
	return commands, nil
}

// get the commands where prev match
func GetSequenceCounts(db *gorm.DB, prev string) (map[string]int, error) {
	var seqVals []Sequence
	if err := db.Where("prev_command = ?", prev).Order("count DESC").Find(&seqVals).Error; err != nil {
		return nil, err
	}

	commands := make(map[string]int, len(seqVals))
	for _, s := range seqVals {
		commands[s.NextCommand] = s.Count
	}

	return commands, nil
}

// get the commands where directory match
func GetDirCountsForCommand(db *gorm.DB, cmd string) (map[string]int, error) {
	type row struct {
		Directory string
		N         int
	}

	var rows []row
	if err := db.Model(&Command{}).Select("directory, COUNT(*) as n").Where("command = ?", cmd).Group("directory").Find(&rows).Error; err != nil {
		return nil, err
	}

	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[r.Directory] = r.N
	}

	return counts, nil
}
