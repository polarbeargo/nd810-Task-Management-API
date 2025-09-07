package services

import (
	"errors"
	"task-manager/backend/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type RegisterService interface {
	RegisterUser(db *gorm.DB, email string, password string) error
}

type RegisterServiceImpl struct{}

func NewRegisterService() *RegisterServiceImpl {
	return &RegisterServiceImpl{}
}

func (s *RegisterServiceImpl) RegisterUser(db *gorm.DB, email string, password string) error {

	var existing models.User
	if err := db.Where("email = ?", email).First(&existing).Error; err == nil {
		return errors.New("username already exists")
	} else if err != gorm.ErrRecordNotFound {
		return err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user := models.User{
		Email:    email,
		Password: string(hashedPassword),
	}
	if err := db.Create(&user).Error; err != nil {
		return err
	}

	var roleID string
	err = db.Raw("SELECT id FROM roles WHERE name = ?", "user").Scan(&roleID).Error
	if err != nil || roleID == "" {
		return errors.New("default role not found")
	}
	err = db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", user.ID, roleID).Error
	if err != nil {
		return err
	}
	return nil
}
