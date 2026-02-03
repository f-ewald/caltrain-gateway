package caltraingateway

import (
	"log"
	"os"
	"strconv"
)

// LoadAPIKeysFromEnv loads API keys from environment variables named FIVEONEONE_API_KEY_1, FIVEONEONE_API_KEY_2, etc.
func LoadAPIKeysFromEnv() []string {
	var keys []string
	for i := 1; ; i++ {
		key := os.Getenv("FIVEONEONE_API_KEY_" + strconv.Itoa(i))
		if key == "" {
			break
		}
		keys = append(keys, key)
	}
	log.Printf("Loaded %d API keys from environment variables.", len(keys))
	return keys
}

// LoadSecretFromEnv loads the Caltrain Gateway secret from the CALTRAIN_GATEWAY_SECRET environment variable.
func LoadSecretFromEnv() string {
	secret := os.Getenv("CALTRAIN_GATEWAY_SECRET")
	if secret == "" {
		log.Println("CALTRAIN_GATEWAY_SECRET environment variable is not set. This is not recommended for production environments.")
	}
	return secret
}
