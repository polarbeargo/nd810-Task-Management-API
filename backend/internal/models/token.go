package models

import (
	"time"

	"github.com/gofrs/uuid"
	"gorm.io/gorm"
)

type Token struct {
	gorm.Model
	ID           uuid.UUID `json:"id" gorm:"primaryKey"`
	UserId       uuid.UUID `json:"user_id"`
	JTI          uuid.UUID `json:"jti" gorm:"uniqueIndex"`
	RefreshToken string    `json:"refresh_token" gorm:"type:text"`
	ExpiresAt    time.Time `json:"expires_at"`
}
