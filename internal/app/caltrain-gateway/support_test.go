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

	t.Run("OPTIONS returns 204 with CORS headers", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/support", nil)
		rec := httptest.NewRecorder()

		supportHandler(rec, req)

		resp := rec.Result()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("Expected status %d, got %d", http.StatusNoContent, resp.StatusCode)
		}
		if origin := resp.Header.Get("Access-Control-Allow-Origin"); origin != "https://fewald.net" {
			t.Errorf("Expected Access-Control-Allow-Origin 'https://fewald.net', got '%s'", origin)
		}
		if methods := resp.Header.Get("Access-Control-Allow-Methods"); methods != "POST, OPTIONS" {
			t.Errorf("Expected Access-Control-Allow-Methods 'POST, OPTIONS', got '%s'", methods)
		}
		if headers := resp.Header.Get("Access-Control-Allow-Headers"); headers != "Content-Type" {
			t.Errorf("Expected Access-Control-Allow-Headers 'Content-Type', got '%s'", headers)
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
