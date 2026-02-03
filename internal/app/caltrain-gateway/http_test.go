package caltraingateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyHandler_ExistingAPIKey(t *testing.T) {
	// Create a test server to mock the 511 API
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify that the API key from the pool is used, not the one from the request
		apiKey := r.URL.Query().Get("api_key")
		if apiKey != "pool-key-123" {
			t.Errorf("Expected api_key='pool-key-123', got '%s'", apiKey)
		}

		// Verify there's only one api_key parameter
		apiKeys := r.URL.Query()["api_key"]
		if len(apiKeys) != 1 {
			t.Errorf("Expected exactly one api_key parameter, got %d", len(apiKeys))
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer mockAPI.Close()

	// Create a key pool with a test key (10 requests per second, burst of 1)
	keyPool := NewKeyPool([]string{"pool-key-123"}, 10, 1)

	// Create a test request with an existing api_key parameter
	req := httptest.NewRequest("GET", "/transit/stops?api_key=user-provided-key&format=json", nil)
	rec := httptest.NewRecorder()

	// Create the handler with mock base URL
	handler := proxyHandlerWithBaseURL(keyPool, mockAPI.URL+"/")

	// Execute the handler
	handler(rec, req)

	// Verify the response
	resp := rec.Result()
	defer resp.Body.Close()

	// Check that the request was processed successfully
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Expected StatusOK, got %d. Body: %s", resp.StatusCode, string(body))
	}
}

func TestProxyHandler_NonOKStatusCode(t *testing.T) {
	tests := []struct {
		name             string
		mockStatusCode   int
		mockResponseBody string
		shouldCache      bool
	}{
		{
			name:             "200 OK should be cached",
			mockStatusCode:   http.StatusOK,
			mockResponseBody: `{"status": "ok"}`,
			shouldCache:      true,
		},
		{
			name:             "404 Not Found should not be cached",
			mockStatusCode:   http.StatusNotFound,
			mockResponseBody: `{"error": "not found"}`,
			shouldCache:      false,
		},
		{
			name:             "500 Internal Server Error should not be cached",
			mockStatusCode:   http.StatusInternalServerError,
			mockResponseBody: `{"error": "server error"}`,
			shouldCache:      false,
		},
		{
			name:             "429 Too Many Requests should not be cached",
			mockStatusCode:   http.StatusTooManyRequests,
			mockResponseBody: `{"error": "rate limited"}`,
			shouldCache:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear cache before each test
			Cache.Flush()

			// Create a mock API server
			mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.mockStatusCode)
				w.Write([]byte(tt.mockResponseBody))
			}))
			defer mockAPI.Close()

			// Create a key pool (10 requests per second, burst of 1)
			keyPool := NewKeyPool([]string{"test-key"}, 10, 1)

			// First request
			req1 := httptest.NewRequest("GET", "/transit/stops?format=json", nil)
			rec1 := httptest.NewRecorder()
			handler := proxyHandlerWithBaseURL(keyPool, mockAPI.URL+"/")
			handler(rec1, req1)

			resp1 := rec1.Result()
			defer resp1.Body.Close()
			body1, _ := io.ReadAll(resp1.Body)

			// Verify the status code is passed through
			if resp1.StatusCode != tt.mockStatusCode {
				t.Errorf("Expected status code %d, got %d", tt.mockStatusCode, resp1.StatusCode)
			}

			// Verify X-Cache header shows MISS
			if resp1.StatusCode == tt.mockStatusCode {
				cacheHeader := resp1.Header.Get("X-Cache")
				if !strings.Contains(cacheHeader, "MISS") && cacheHeader != "" {
					// Note: might be empty if StatusBadGateway
					t.Logf("First request X-Cache header: %s", cacheHeader)
				}

				// Second request to check caching behavior
				req2 := httptest.NewRequest("GET", "/transit/stops?format=json", nil)
				rec2 := httptest.NewRecorder()
				handler(rec2, req2)

				resp2 := rec2.Result()
				defer resp2.Body.Close()
				body2, _ := io.ReadAll(resp2.Body)

				cacheHeader2 := resp2.Header.Get("X-Cache")

				if tt.shouldCache {
					// Should get a cache HIT
					if cacheHeader2 != "HIT" {
						t.Errorf("Expected cache HIT on second request for status %d, got '%s'", tt.mockStatusCode, cacheHeader2)
					}
					// Response should be the same
					if string(body1) != string(body2) {
						t.Errorf("Cached response body doesn't match original")
					}
				} else {
					// Should NOT get a cache HIT (should be MISS or error)
					if cacheHeader2 == "HIT" {
						t.Errorf("Expected no cache HIT for status %d, but got cache HIT", tt.mockStatusCode)
					}
				}
			}
		})
	}
}

func TestAuthMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		secret         string
		requestSecret  string
		setHeader      bool
		expectedStatus int
		expectNext     bool
	}{
		{
			name:           "no secret configured - should allow all requests",
			secret:         "",
			requestSecret:  "",
			setHeader:      false,
			expectedStatus: http.StatusOK,
			expectNext:     true,
		},
		{
			name:           "secret configured but no header provided",
			secret:         "mysecret",
			requestSecret:  "",
			setHeader:      false,
			expectedStatus: http.StatusUnauthorized,
			expectNext:     false,
		},
		{
			name:           "secret configured with wrong header",
			secret:         "mysecret",
			requestSecret:  "wrongsecret",
			setHeader:      true,
			expectedStatus: http.StatusUnauthorized,
			expectNext:     false,
		},
		{
			name:           "secret configured with correct header",
			secret:         "mysecret",
			requestSecret:  "mysecret",
			setHeader:      true,
			expectedStatus: http.StatusOK,
			expectNext:     true,
		},
		{
			name:           "empty header provided when secret required",
			secret:         "mysecret",
			requestSecret:  "",
			setHeader:      true,
			expectedStatus: http.StatusUnauthorized,
			expectNext:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("next handler called"))
			})

			// Create the middleware
			handler := authMiddleware(tt.secret, nextHandler)

			// Create request
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.setHeader {
				req.Header.Set("X-API-SECRET", tt.requestSecret)
			}

			// Create response recorder
			rec := httptest.NewRecorder()

			// Execute the handler
			handler(rec, req)

			// Check status code
			resp := rec.Result()
			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			// Check if next handler was called
			if nextCalled != tt.expectNext {
				t.Errorf("Expected next handler called: %v, got: %v", tt.expectNext, nextCalled)
			}

			// For unauthorized requests, check response body
			if tt.expectedStatus == http.StatusUnauthorized {
				body, _ := io.ReadAll(resp.Body)
				if !strings.Contains(string(body), "Unauthorized") {
					t.Errorf("Expected 'Unauthorized' in response body, got: %s", string(body))
				}
			}
		})
	}
}

func TestLogRequestMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		url        string
		remoteAddr string
	}{
		{
			name:       "GET request",
			method:     "GET",
			url:        "/api/test?param=value",
			remoteAddr: "192.168.1.1:8080",
		},
		{
			name:       "POST request",
			method:     "POST",
			url:        "/api/submit",
			remoteAddr: "10.0.0.1:3000",
		},
		{
			name:       "request with complex URL",
			method:     "GET",
			url:        "/api/data?format=json&limit=10&offset=20",
			remoteAddr: "127.0.0.1:9000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			var capturedRequest *http.Request

			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				capturedRequest = r
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("success"))
			})

			// Create the middleware
			handler := logRequestMiddleware(nextHandler)

			// Create request
			req := httptest.NewRequest(tt.method, tt.url, nil)
			req.RemoteAddr = tt.remoteAddr

			// Create response recorder
			rec := httptest.NewRecorder()

			// Execute the handler
			handler(rec, req)

			// Verify next handler was called
			if !nextCalled {
				t.Error("Expected next handler to be called")
			}

			// Verify request was passed through correctly
			if capturedRequest == nil {
				t.Error("Request was not captured by next handler")
			} else {
				if capturedRequest.Method != tt.method {
					t.Errorf("Expected method %s, got %s", tt.method, capturedRequest.Method)
				}
				if capturedRequest.URL.String() != tt.url {
					t.Errorf("Expected URL %s, got %s", tt.url, capturedRequest.URL.String())
				}
				if capturedRequest.RemoteAddr != tt.remoteAddr {
					t.Errorf("Expected RemoteAddr %s, got %s", tt.remoteAddr, capturedRequest.RemoteAddr)
				}
			}

			// Verify response
			resp := rec.Result()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			body, _ := io.ReadAll(resp.Body)
			if string(body) != "success" {
				t.Errorf("Expected response body 'success', got '%s'", string(body))
			}
		})
	}
}
