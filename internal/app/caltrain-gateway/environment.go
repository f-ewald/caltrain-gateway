package caltraingateway

import (
	"log"
	"os"
	"strconv"
)

// LoadAPIKeysFromEnv loads API keys from environment variables named 511_API_KEY_1, 511_API_KEY_2, etc.
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
