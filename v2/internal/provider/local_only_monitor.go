package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	checkLocalOnlyToolsFn       = checkLocalOnlyTools
	monitorProcessTreeNetworkFn = monitorProcessTreeNetwork
)

var remoteEndpointPattern = regexp.MustCompile(`->([^ ]+)`)

func checkLocalOnlyTools() (bool, string) {
	if _, err := exec.LookPath("lsof"); err != nil {
		return false, "local-only monitor requires lsof in PATH"
	}
	if _, err := exec.LookPath("pgrep"); err != nil {
		return false, "local-only monitor requires pgrep in PATH"
	}
	return true, "local-only monitor active (lsof + pgrep)"
}

func LocalOnlySelfcheck() (bool, string) {
	return checkLocalOnlyToolsFn()
}

func monitorProcessTreeNetwork(
	ctx context.Context,
	rootPID int,
	sampleInterval time.Duration,
) (int, float64, []string, error) {
	if rootPID <= 0 {
		return 0, 0, nil, errors.New("invalid root pid")
	}
	if sampleInterval <= 0 {
		sampleInterval = 200 * time.Millisecond
	}

	start := time.Now()
	violations := map[string]struct{}{}
	samples := 0

	sample := func() error {
		samples++
		pids := processTreePIDs(rootPID)
		for _, pid := range pids {
			endpoints, err := listRemoteEndpointsForPID(pid)
			if err != nil {
				return err
			}
			for _, endpoint := range endpoints {
				if isLoopbackEndpoint(endpoint) {
					continue
				}
				key := fmt.Sprintf("pid=%d remote=%s", pid, endpoint)
				violations[key] = struct{}{}
			}
		}
		return nil
	}

	if err := sample(); err != nil {
		return samples, time.Since(start).Seconds(), nil, err
	}

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			items := make([]string, 0, len(violations))
			for item := range violations {
				items = append(items, item)
			}
			sort.Strings(items)
			return samples, time.Since(start).Seconds(), items, nil
		case <-ticker.C:
			if err := sample(); err != nil {
				return samples, time.Since(start).Seconds(), nil, err
			}
		}
	}
}

func processTreePIDs(rootPID int) []int {
	seen := map[int]struct{}{}
	queue := []int{rootPID}
	ordered := []int{}

	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		ordered = append(ordered, pid)

		children, err := childPIDs(pid)
		if err != nil {
			continue
		}
		queue = append(queue, children...)
	}

	sort.Ints(ordered)
	return ordered
}

func childPIDs(pid int) ([]int, error) {
	cmd := exec.Command("pgrep", "-P", strconv.Itoa(pid))
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	children := make([]int, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		childPID, convErr := strconv.Atoi(line)
		if convErr != nil {
			continue
		}
		children = append(children, childPID)
	}
	return children, nil
}

func listRemoteEndpointsForPID(pid int) ([]string, error) {
	cmd := exec.Command("lsof", "-nP", "-i", "-a", "-p", strconv.Itoa(pid))
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	lines := strings.Split(string(out), "\n")
	endpoints := []string{}
	for _, line := range lines {
		if !strings.Contains(line, "->") {
			continue
		}
		match := remoteEndpointPattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		endpoints = append(endpoints, strings.TrimSpace(match[1]))
	}
	return endpoints, nil
}

func isLoopbackEndpoint(endpoint string) bool {
	host := endpoint
	if strings.HasPrefix(host, "[") {
		end := strings.Index(host, "]")
		if end > 1 {
			host = host[1:end]
		}
	} else {
		if idx := strings.LastIndex(host, ":"); idx > 0 {
			host = host[:idx]
		}
	}

	host = strings.TrimSpace(host)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
