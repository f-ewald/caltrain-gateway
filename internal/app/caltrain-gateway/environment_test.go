package caltraingateway

import (
	"os"
	"strconv"
	"testing"
)

func TestLoadAPIKeysFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected []string
	}{
		{
			name:     "no keys set",
			envVars:  map[string]string{},
			expected: nil,
		},
		{
			name: "single key",
			envVars: map[string]string{
				"FIVEONEONE_API_KEY_1": "key1",
			},
			expected: []string{"key1"},
		},
		{
			name: "multiple keys",
			envVars: map[string]string{
				"FIVEONEONE_API_KEY_1": "key1",
				"FIVEONEONE_API_KEY_2": "key2",
				"FIVEONEONE_API_KEY_3": "key3",
			},
			expected: []string{"key1", "key2", "key3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear any existing env vars
			for i := 1; i <= 10; i++ {
				os.Unsetenv("FIVEONEONE_API_KEY_" + strconv.Itoa(i))
			}

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			// Run the function
			result := LoadAPIKeysFromEnv()

			// Check the result
			if len(result) != len(tt.expected) {
				t.Errorf("LoadAPIKeysFromEnv() returned %d keys, expected %d", len(result), len(tt.expected))
				return
			}

			for i, key := range result {
				if key != tt.expected[i] {
					t.Errorf("LoadAPIKeysFromEnv()[%d] = %q, expected %q", i, key, tt.expected[i])
				}
			}

			// Cleanup
			for k := range tt.envVars {
				os.Unsetenv(k)
			}
		})
	}
}
