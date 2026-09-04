package caltraingateway

import "testing"

func TestGTFSIDToParentNameNotEmpty(t *testing.T) {
	if len(GTFSIDToParentName) == 0 {
		t.Error("GTFSIDToParentName map should not be empty")
	}
}

func TestBARTGTFSIDToParentNameNotEmpty(t *testing.T) {
	if len(BARTGTFSIDToParentName) == 0 {
		t.Error("BARTGTFSIDToParentName map should not be empty")
	}
}

func TestStopsByOperator_CoversCTAndBA(t *testing.T) {
	if stopsByOperator["CT"] == nil {
		t.Error("expected stopsByOperator to map CT to GTFSIDToParentName")
	}
	if stopsByOperator["BA"] == nil {
		t.Error("expected stopsByOperator to map BA to BARTGTFSIDToParentName")
	}
	if len(stopsByOperator) != 2 {
		t.Errorf("expected exactly 2 supported agencies in stopsByOperator, got %d", len(stopsByOperator))
	}
}
