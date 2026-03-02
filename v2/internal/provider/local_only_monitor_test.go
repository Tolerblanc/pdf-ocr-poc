package provider

import (
	"os"
	"testing"
)

func TestIsLoopbackEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		expect   bool
	}{
		{endpoint: "127.0.0.1:80", expect: true},
		{endpoint: "localhost:443", expect: true},
		{endpoint: "[::1]:443", expect: true},
		{endpoint: "1.1.1.1:53", expect: false},
		{endpoint: "example.com:443", expect: false},
	}

	for _, test := range tests {
		if actual := isLoopbackEndpoint(test.endpoint); actual != test.expect {
			t.Fatalf("endpoint=%s expected %v got %v", test.endpoint, test.expect, actual)
		}
	}
}

func TestProcessTreePIDsIncludesRootPID(t *testing.T) {
	root := os.Getpid()
	pids := processTreePIDs(root)
	found := false
	for _, pid := range pids {
		if pid == root {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected root pid %d in process tree: %+v", root, pids)
	}
}
