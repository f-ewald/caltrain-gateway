package caltraingateway

import "testing"

func TestGTFSIDToParentNameNotEmpty(t *testing.T) {
	if len(GTFSIDToParentName) == 0 {
		t.Error("GTFSIDToParentName map should not be empty")
	}
}
