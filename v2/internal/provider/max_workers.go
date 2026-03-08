package provider

import (
	"path/filepath"
	"runtime"
	"strings"
)

const visionSwiftAutoMaxWorkers = 2

func ResolveAutoMaxWorkers(providerName, providerBin string) int {
	if isVisionSwiftProvider(providerName, providerBin) {
		return visionSwiftAutoMaxWorkers
	}
	workers := runtime.NumCPU() - 1
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	return workers
}

func isVisionSwiftProvider(providerName, providerBin string) bool {
	if strings.EqualFold(strings.TrimSpace(providerName), "vision-swift") {
		return true
	}
	base := strings.TrimSpace(providerBin)
	if base == "" {
		return false
	}
	return strings.EqualFold(filepath.Base(base), "vision-provider")
}
