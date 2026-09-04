package caltraingateway

import (
	"testing"
	"time"
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

func TestGetCalendarOverride_NilDB(t *testing.T) {
	DB = nil
	row, err := GetCalendarOverride("CT", time.Now())
	if err != nil {
		t.Fatalf("Expected no error when DB is nil, got %v", err)
	}
	if row != nil {
		t.Error("Expected nil row when DB is nil")
	}
}

func TestListCalendarOverrides_NilDB(t *testing.T) {
	DB = nil
	rows, err := ListCalendarOverrides()
	if err != nil {
		t.Fatalf("Expected no error when DB is nil, got %v", err)
	}
	if rows != nil {
		t.Error("Expected nil rows when DB is nil")
	}
}

func TestUpsertCalendarOverride_NilDB(t *testing.T) {
	DB = nil
	err := UpsertCalendarOverride("CT", time.Now(), string(ScheduleSunday), "Labor Day")
	if err != nil {
		t.Fatalf("Expected no error when DB is nil, got %v", err)
	}
}

func TestDeleteCalendarOverride_NilDB(t *testing.T) {
	DB = nil
	err := DeleteCalendarOverride(1)
	if err != nil {
		t.Fatalf("Expected no error when DB is nil, got %v", err)
	}
}
