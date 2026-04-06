package caltraingateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSupportHandler(t *testing.T) {
	t.Run("valid POST", func(t *testing.T) {
		body := `{"name":"Test","app":"Baby Bullet","email":"test@example.com","type":"Feedback","message":"Hello"}`
		req := httptest.NewRequest("POST", "/support", strings.NewReader(body))
		rec := httptest.NewRecorder()

		supportHandler(rec, req)

		resp := rec.Result()
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if result["status"] != "ok" {
			t.Errorf("Expected status 'ok', got '%s'", result["status"])
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
		}
	})

	t.Run("GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/support", nil)
		rec := httptest.NewRecorder()

		supportHandler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/support", strings.NewReader("not json"))
		rec := httptest.NewRecorder()

		supportHandler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
		}
	})

	t.Run("missing required fields returns 400", func(t *testing.T) {
		body := `{"name":"Test","app":"Baby Bullet"}`
		req := httptest.NewRequest("POST", "/support", strings.NewReader(body))
		rec := httptest.NewRecorder()

		supportHandler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
		}
	})

	t.Run("empty body returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/support", strings.NewReader("{}"))
		rec := httptest.NewRecorder()

		supportHandler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
		}
	})
}
