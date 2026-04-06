package caltraingateway

import (
	"testing"
)

func TestInitDB_EmptyConnStr(t *testing.T) {
	DB = nil
	err := InitDB("")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if DB != nil {
		t.Error("Expected DB to remain nil when connection string is empty")
	}
}

func TestInsertSupportRequest_NilDB(t *testing.T) {
	DB = nil
	req := &supportRequest{
		Name:    "Test",
		App:     "Baby Bullet",
		Email:   "test@example.com",
		Type:    "Feedback",
		Message: "Hello",
	}
	err := InsertSupportRequest(req)
	if err != nil {
		t.Fatalf("Expected no error when DB is nil, got %v", err)
	}
}

func TestCloseDB_NilDB(t *testing.T) {
	DB = nil
	// Should not panic
	CloseDB()
}
