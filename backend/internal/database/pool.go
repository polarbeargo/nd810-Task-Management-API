package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DatabasePool struct {
	*gorm.DB
	config *PoolConfig
}

type PoolConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	LogLevel        logger.LogLevel
}

func DefaultPoolConfig() *PoolConfig {
	return &PoolConfig{
		MaxOpenConns:    25,               
		MaxIdleConns:    10,               
		ConnMaxLifetime: time.Hour,        
		ConnMaxIdleTime: time.Minute * 30, 
		LogLevel:        logger.Info,
	}
}

func NewDatabasePool(config *PoolConfig) (*DatabasePool, error) {
	if config == nil {
		config = DefaultPoolConfig()
	}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(config.LogLevel),
		PrepareStmt:                              true,
		DisableForeignKeyConstraintWhenMigrating: false,
		SkipDefaultTransaction: true,
	}

	db, err := gorm.Open(postgres.Open(config.DSN), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(config.MaxOpenConns)
	sqlDB.SetMaxIdleConns(config.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(config.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("Database pool initialized with %d max connections", config.MaxOpenConns)

	return &DatabasePool{
		DB:     db,
		config: config,
	}, nil
}

func (p *DatabasePool) Health() error {
	if p.DB == nil {
		return errors.New("database connection is nil")
	}

	sqlDB, err := p.DB.DB()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return sqlDB.PingContext(ctx)
}

func (p *DatabasePool) Stats() map[string]interface{} {
	if p.DB == nil {
		return map[string]interface{}{"error": "database connection is nil"}
	}

	sqlDB, err := p.DB.DB()
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}

	stats := sqlDB.Stats()

	return map[string]interface{}{
		"max_open_connections": stats.MaxOpenConnections,
		"open_connections":     stats.OpenConnections,
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration_ms":     stats.WaitDuration.Milliseconds(),
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_idle_time_closed": stats.MaxIdleTimeClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}
}

func (p *DatabasePool) Close() error {
	if p.DB == nil {
		return nil 
	}

	sqlDB, err := p.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
