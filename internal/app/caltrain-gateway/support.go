package caltraingateway

import (
	"encoding/json"
	"log"
	"net/http"
)

// supportRequest represents a support/feedback submission.
type supportRequest struct {
	Name    string `json:"name"`
	App     string `json:"app"`
	Email   string `json:"email"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// supportHandler accepts POST requests with support/feedback submissions.
func supportHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "https://fewald.net")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req supportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Email == "" || req.Type == "" || req.Message == "" {
		http.Error(w, "Bad Request: missing required fields (name, email, type, message)", http.StatusBadRequest)
		return
	}

	log.Printf("Support request: name=%s app=%s email=%s type=%s message=%s",
		req.Name, req.App, req.Email, req.Type, req.Message)

	if err := InsertSupportRequest(&req); err != nil {
		log.Printf("Warning: failed to persist support request: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
