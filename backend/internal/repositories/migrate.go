package repositories

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/gorm"
)

type MigrationConfig struct {
	MigrationsPath string
	DBName         string
	MaxRetries     int
	RetryDelay     time.Duration
}

func DefaultMigrationConfig() *MigrationConfig {
	return &MigrationConfig{
		MigrationsPath: "file://migrations",
		DBName:         "task_manager",
		MaxRetries:     3,
		RetryDelay:     2 * time.Second,
	}
}

func RunMigrations(db *gorm.DB, config *MigrationConfig) error {
	if config == nil {
		config = DefaultMigrationConfig()
	}

	log.Printf("🔄 Starting database migrations from: %s", config.MigrationsPath)

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	if err := waitForDatabase(sqlDB, config.MaxRetries, config.RetryDelay); err != nil {
		return fmt.Errorf("database not ready: %w", err)
	}

	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{
		DatabaseName:          config.DBName,
		MigrationsTable:       "schema_migrations",
		MigrationsTableQuoted: false,
		MultiStatementEnabled: true,
		MultiStatementMaxSize: 10 * 1 << 20, // 10 MB
	})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		config.MigrationsPath,
		config.DBName,
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}

	currentVersion, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		log.Printf("⚠️  Could not get current migration version: %v", err)
	} else if err == migrate.ErrNilVersion {
		log.Println("📋 No migrations applied yet")
	} else {
		log.Printf("📋 Current migration version: %d (dirty: %v)", currentVersion, dirty)
	}

	err = m.Up()
	if err != nil {
		if err == migrate.ErrNoChange {
			log.Println("✅ Database schema is up to date - no migrations needed")
			return nil
		}
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	finalVersion, dirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("failed to get final migration version: %w", err)
	}

	log.Printf("✅ Database migrations completed successfully")
	log.Printf("📊 Final migration version: %d (dirty: %v)", finalVersion, dirty)

	if err := logMigrationDetails(sqlDB); err != nil {
		log.Printf("⚠️  Could not retrieve migration details: %v", err)
	}

	return nil
}

func waitForDatabase(db *sql.DB, maxRetries int, retryDelay time.Duration) error {
	for i := 0; i < maxRetries; i++ {
		if err := db.Ping(); err == nil {
			return nil
		}
		if i < maxRetries-1 {
			log.Printf("⏳ Database not ready, retrying in %v... (attempt %d/%d)", retryDelay, i+1, maxRetries)
			time.Sleep(retryDelay)
		}
	}
	return fmt.Errorf("database not ready after %d attempts", maxRetries)
}

func logMigrationDetails(db *sql.DB) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		return err
	}

	log.Printf("📈 Total migration records in schema_migrations: %d", count)

	rows, err := db.Query(`
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = 'public' 
		AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}
		tables = append(tables, tableName)
	}

	log.Printf("📊 Database tables (%d): %v", len(tables), tables)

	return nil
}

func RollbackMigration(db *gorm.DB, config *MigrationConfig) error {
	if config == nil {
		config = DefaultMigrationConfig()
	}

	log.Println("⬇️  Rolling back last migration...")

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}

	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{
		DatabaseName: config.DBName,
	})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		config.MigrationsPath,
		config.DBName,
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}

	if err := m.Steps(-1); err != nil {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	log.Println("✅ Migration rolled back successfully")
	return nil
}

func GetMigrationVersion(db *gorm.DB, config *MigrationConfig) (uint, bool, error) {
	if config == nil {
		config = DefaultMigrationConfig()
	}

	sqlDB, err := db.DB()
	if err != nil {
		return 0, false, fmt.Errorf("failed to get database instance: %w", err)
	}

	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{
		DatabaseName: config.DBName,
	})
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migration driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		config.MigrationsPath,
		config.DBName,
		driver,
	)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migration instance: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil {
		return 0, false, err
	}

	return version, dirty, nil
}
