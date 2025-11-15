package utils_test

import (
	"os"
	"testing"
	"time"

	"task-manager/backend/internal/utils"

	"github.com/gofrs/uuid"
)

func TestParseJWT_ValidToken(t *testing.T) {
	_, err := utils.ParseJWT("invalid.jwt.token", "secret")
	if err == nil {
		t.Error("Expected error for invalid JWT token, got nil")
	}
}

func TestIsValidUUID_Valid(t *testing.T) {
	validUUID := uuid.Must(uuid.NewV4()).String()

	if !utils.IsValidUUID(validUUID) {
		t.Errorf("Expected valid UUID %s to return true", validUUID)
	}
}

func TestIsValidUUID_Invalid(t *testing.T) {
	invalidUUIDs := []string{
		"invalid-uuid",
		"",
		"123-456-789",
		"not-a-uuid-at-all",
	}

	for _, invalid := range invalidUUIDs {
		if utils.IsValidUUID(invalid) {
			t.Errorf("Expected invalid UUID %s to return false", invalid)
		}
	}
}

func TestGetEnv_ExistingVariable(t *testing.T) {
	key := "TEST_ENV_VAR"
	expectedValue := "test_value"

	os.Setenv(key, expectedValue)
	defer os.Unsetenv(key)

	result := utils.GetEnv(key, "default")
	if result != expectedValue {
		t.Errorf("Expected %s, got %s", expectedValue, result)
	}
}

func TestGetEnv_NonExistingVariable(t *testing.T) {
	key := "NON_EXISTING_ENV_VAR"
	defaultValue := "default_value"

	os.Unsetenv(key)

	result := utils.GetEnv(key, defaultValue)
	if result != defaultValue {
		t.Errorf("Expected %s, got %s", defaultValue, result)
	}
}

func TestGetEnvAsInt_ValidInteger(t *testing.T) {
	key := "TEST_INT_VAR"
	expectedValue := 42

	os.Setenv(key, "42")
	defer os.Unsetenv(key)

	result := utils.GetEnvAsInt(key, 0)
	if result != expectedValue {
		t.Errorf("Expected %d, got %d", expectedValue, result)
	}
}

func TestGetEnvAsInt_InvalidInteger(t *testing.T) {
	key := "TEST_INVALID_INT_VAR"
	defaultValue := 10

	os.Setenv(key, "not_an_integer")
	defer os.Unsetenv(key)

	result := utils.GetEnvAsInt(key, defaultValue)
	if result != defaultValue {
		t.Errorf("Expected %d, got %d", defaultValue, result)
	}
}

func TestGetEnvAsInt_NonExistingVariable(t *testing.T) {
	key := "NON_EXISTING_INT_VAR"
	defaultValue := 5

	os.Unsetenv(key)

	result := utils.GetEnvAsInt(key, defaultValue)
	if result != defaultValue {
		t.Errorf("Expected %d, got %d", defaultValue, result)
	}
}

func TestGetEnvAsDuration_ValidDuration(t *testing.T) {
	key := "TEST_DURATION_VAR"
	expectedDuration := 30 * time.Second

	os.Setenv(key, "30s")
	defer os.Unsetenv(key)

	result := utils.GetEnvAsDuration(key, 0)
	if result != expectedDuration {
		t.Errorf("Expected %v, got %v", expectedDuration, result)
	}
}

func TestGetEnvAsDuration_InvalidDuration(t *testing.T) {
	key := "TEST_INVALID_DURATION_VAR"
	defaultDuration := 1 * time.Minute

	os.Setenv(key, "invalid_duration")
	defer os.Unsetenv(key)

	result := utils.GetEnvAsDuration(key, defaultDuration)
	if result != defaultDuration {
		t.Errorf("Expected %v, got %v", defaultDuration, result)
	}
}

func TestGetEnvAsDuration_NonExistingVariable(t *testing.T) {
	key := "NON_EXISTING_DURATION_VAR"
	defaultDuration := 2 * time.Hour

	os.Unsetenv(key)

	result := utils.GetEnvAsDuration(key, defaultDuration)
	if result != defaultDuration {
		t.Errorf("Expected %v, got %v", defaultDuration, result)
	}
}
