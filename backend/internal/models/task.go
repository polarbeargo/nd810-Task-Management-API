package models

import (
	"time"

	"github.com/gofrs/uuid"
	"gorm.io/gorm"
)

type Task struct {
	ID          uuid.UUID      `json:"id" gorm:"primaryKey;type:uuid;default:uuid_generate_v4()"`
	UserID      uuid.UUID      `json:"user_id" gorm:"type:uuid;not null"`
	Title       string         `json:"title" gorm:"not null"`
	Description string         `json:"description"`
	Status      string         `json:"status" gorm:"not null;default:'pending'"`
	DueDate     time.Time      `json:"due_date" gorm:"type:timestamp"`
	Priority    string         `json:"priority" gorm:"not null;default:'Low'"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}
