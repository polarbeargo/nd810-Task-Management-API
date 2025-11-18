package services

import (
	"fmt"
	"os"
	"task-manager/backend/internal/models"
	"time"

	"github.com/gofrs/uuid"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService interface {
	LoginUser(db *gorm.DB, email, password string) (*models.User, error)
	GenerateToken(db *gorm.DB, userID uuid.UUID) (string, string, error)
	RefreshToken(db *gorm.DB, refreshToken string) (string, string, int64, error)
	GetUserPermissions(db *gorm.DB, userID uuid.UUID) ([]string, error)
	RevokeToken(db *gorm.DB, refreshToken string) error
}

type AuthServiceImpl struct{}

func (s *AuthServiceImpl) RefreshToken(db *gorm.DB, refreshToken string) (string, string, int64, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "default_secret_change_in_production"
	}

	token, err := jwt.Parse(refreshToken, func(token *jwt.Token) (interface{}, error) {

		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return "", "", 0, fmt.Errorf("invalid refresh token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", "", 0, fmt.Errorf("invalid refresh token claims")
	}

	tokenType, ok := claims["type"].(string)
	if !ok || tokenType != "refresh" {
		return "", "", 0, fmt.Errorf("invalid token type")
	}

	jtiStr, ok := claims["jti"].(string)
	if !ok {
		return "", "", 0, fmt.Errorf("missing jti in token")
	}

	jti, err := uuid.FromString(jtiStr)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid jti format: %w", err)
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return "", "", 0, fmt.Errorf("missing user_id in token")
	}

	userID, err := uuid.FromString(userIDStr)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid user_id format: %w", err)
	}

	var dbToken models.Token
	err = db.Where("jti = ? AND user_id = ? AND expires_at > ?", jti, userID, time.Now()).First(&dbToken).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", "", 0, fmt.Errorf("refresh token not found or expired")
		}
		return "", "", 0, fmt.Errorf("database error: %w", err)
	}

	accessToken, newRefreshToken, err := s.GenerateToken(db, userID)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to generate new tokens: %w", err)
	}

	expiresIn := int64(3600)

	if err := db.Delete(&dbToken).Error; err != nil {
		return "", "", 0, fmt.Errorf("failed to delete old token: %w", err)
	}

	return accessToken, newRefreshToken, expiresIn, nil
}

func NewAuthService() *AuthServiceImpl {
	return &AuthServiceImpl{}
}

func VerifyPassword(hashedPassword, plainPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(plainPassword))
	return err == nil
}

func (s *AuthServiceImpl) LoginUser(db *gorm.DB, email, password string) (*models.User, error) {
	var user models.User
	if err := db.Where("email = ? AND is_active = ?", email, true).First(&user).Error; err != nil {
		return nil, err
	}
	if !VerifyPassword(user.Password, password) {
		return nil, gorm.ErrInvalidData
	}
	return &user, nil
}

func (s *AuthServiceImpl) GetUserPermissions(db *gorm.DB, userID uuid.UUID) ([]string, error) {
	var permissions []string
	err := db.Raw(`SELECT DISTINCT p.resource || ':' || p.action FROM permissions p
		JOIN role_permissions rp ON p.id = rp.permission_id
		JOIN user_roles ur ON rp.role_id = ur.role_id
		WHERE ur.user_id = ?`, userID).Scan(&permissions).Error
	if err != nil {
		return []string{}, err
	}
	return permissions, nil
}

func (s *AuthServiceImpl) RevokeToken(db *gorm.DB, refreshToken string) error {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "default_secret_change_in_production"
	}

	token, err := jwt.Parse(refreshToken, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return fmt.Errorf("invalid refresh token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("invalid token claims")
	}

	jtiStr, ok := claims["jti"].(string)
	if !ok {
		return fmt.Errorf("missing jti in token")
	}

	jti, err := uuid.FromString(jtiStr)
	if err != nil {
		return fmt.Errorf("invalid jti format: %w", err)
	}

	return db.Where("jti = ?", jti).Delete(&models.Token{}).Error
}

func (s *AuthServiceImpl) GenerateToken(db *gorm.DB, userID uuid.UUID) (string, string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "default_secret_change_in_production"
	}

	var roleName string
	err := db.Raw("SELECT r.name FROM roles r JOIN user_roles ur ON r.id = ur.role_id WHERE ur.user_id = ? LIMIT 1", userID).Scan(&roleName).Error
	if err != nil || roleName == "" {
		roleName = "user"
	}

	var permissions []string
	err = db.Raw(`SELECT DISTINCT p.resource || ':' || p.action FROM permissions p
		JOIN role_permissions rp ON p.id = rp.permission_id
		JOIN user_roles ur ON rp.role_id = ur.role_id
		WHERE ur.user_id = ?`, userID).Scan(&permissions).Error
	if err != nil {
		permissions = []string{}
	}

	now := time.Now()

	accessTokenClaims := jwt.MapClaims{
		"user_id":     userID.String(),
		"role":        roleName,
		"permissions": permissions,
		"iat":         now.Unix(),
		"exp":         now.Add(time.Hour).Unix(),
		"iss":         "taskify-backend",
		"aud":         "taskify-users",
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessTokenClaims)
	accessTokenString, err := accessToken.SignedString([]byte(secret))
	if err != nil {
		return "", "", fmt.Errorf("failed to sign access token: %w", err)
	}

	jti, err := uuid.NewV4()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate jti: %w", err)
	}

	refreshTokenExpiry := now.Add(time.Hour)
	refreshTokenClaims := jwt.MapClaims{
		"user_id": userID.String(),
		"type":    "refresh",
		"jti":     jti.String(),
		"iat":     now.Unix(),
		"exp":     refreshTokenExpiry.Unix(),
		"iss":     "taskify-backend",
		"aud":     "taskify-users",
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshTokenClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(secret))
	if err != nil {
		return "", "", fmt.Errorf("failed to sign refresh token: %w", err)
	}

	tokenRecord := models.Token{
		ID:           uuid.Must(uuid.NewV4()),
		UserId:       userID,
		JTI:          jti,
		RefreshToken: refreshTokenString,
		ExpiresAt:    refreshTokenExpiry,
	}

	if err := db.Create(&tokenRecord).Error; err != nil {
		return "", "", fmt.Errorf("failed to create token record: %w", err)
	}

	return accessTokenString, refreshTokenString, nil
}
