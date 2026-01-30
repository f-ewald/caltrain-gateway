package main

import (
	"log"
	"net/http"

	caltraingateway "caltrain-gateway/internal/app/caltrain-gateway"
)

func main() {
	apiKeyPool := caltraingateway.NewKeyPool(
		caltraingateway.LoadAPIKeysFromEnv(),
		1, // 1 request per second
		5, // burst size of 5
	)

	if len(apiKeyPool.Keys) == 0 {
		log.Fatal("No API keys found in environment variables 511_API_KEY_1, 511_API_KEY_2, etc.")
	}

	caltraingateway.SetupRoutes(apiKeyPool)

	log.Println("Caltrain Proxy running on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
