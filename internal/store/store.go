package store

import (
	"fmt"
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

func SaveImportBatch(db *gorm.DB, commands []Command) error {
	if len(commands) == 0 {
		return nil
	}
	seqRows := createSeq(commands)
	statsRows := createCommandStat(commands)

	return db.Transaction(func(tx *gorm.DB) error {

		if err := tx.CreateInBatches(commands, len(commands)).Error; err != nil {
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
