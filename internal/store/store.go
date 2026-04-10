package store

import (
	"fmt"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
	PrevCommand string `gorm:"primaryKey;size:2048"`
	NextCommand string `gorm:"primaryKey;size:2048"`
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
