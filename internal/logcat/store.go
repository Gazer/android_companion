package logcat

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// maxStoredEntries bounds memory usage. Old entries are evicted FIFO.
	maxStoredEntries = 50000
)

// Store holds log entries in memory, decoupled from UI rendering.
type Store struct {
	mu      sync.RWMutex
	entries []Entry

	// pidToPackage maps PID -> package name (e.g. "com.example.app")
	pidToPackage map[int32]string
	// packageSet is the deduplicated set of known packages
	packageSet map[string]struct{}

	adbPath string
	serial  string

	refreshing bool
}

// NewStore creates an empty log store.
// If adbPath and serial are provided, it does an initial process map refresh.
func NewStore(adbPath, serial string) *Store {
	s := &Store{
		pidToPackage: make(map[int32]string),
		packageSet:   make(map[string]struct{}),
		adbPath:      adbPath,
		serial:       serial,
	}
	if adbPath != "" && serial != "" {
		s.RefreshProcessMap()
	}
	return s
}

// SetDevice updates the adb path and serial, then refreshes the process map.
func (s *Store) SetDevice(adbPath, serial string) {
	s.mu.Lock()
	s.adbPath = adbPath
	s.serial = serial
	s.mu.Unlock()
	s.RefreshProcessMap()
}

// Add appends an entry and resolves its package via PID.
// Returns the package name (may be empty if PID unknown).
//
// Continuation lines (unparsed, e.Time == "") inherit PID and Level from
// the previous entry. This matches Android Studio's behavior: the extra
// lines of a multi-line log message belong to the parent line's process
// and are colored with the parent line's level.
func (s *Store) Add(e Entry) string {
	s.mu.Lock()
	if len(s.entries) >= maxStoredEntries {
		drop := maxStoredEntries / 10
		s.entries = s.entries[drop:]
	}
	if e.Time == "" && len(s.entries) > 0 {
		// Inherit parent context for continuation lines
		parent := s.entries[len(s.entries)-1]
		if e.PID == "" {
			e.PID = parent.PID
		}
		if e.TID == "" {
			e.TID = parent.TID
		}
		if e.Level == "" {
			e.Level = parent.Level
		}
	}
	s.entries = append(s.entries, e)
	s.mu.Unlock()

	pid := parsePID(e.PID)
	return s.lookupPackage(pid)
}

// lookupPackage resolves a PID to a package name, caching the result.
// If unknown, it triggers an async refresh of the process map.
func (s *Store) lookupPackage(pid int32) string {
	if pid <= 0 {
		return ""
	}
	s.mu.RLock()
	pkg, ok := s.pidToPackage[pid]
	s.mu.RUnlock()
	if ok {
		return pkg
	}
	// Unknown PID: trigger async refresh (rate-limited)
	s.asyncRefreshProcessMap()
	return ""
}

// asyncRefreshProcessMap refreshes the PID->package map without blocking.
func (s *Store) asyncRefreshProcessMap() {
	s.mu.Lock()
	if s.refreshing {
		s.mu.Unlock()
		return
	}
	s.refreshing = true
	s.mu.Unlock()

	go func() {
		s.RefreshProcessMap()
		s.mu.Lock()
		s.refreshing = false
		s.mu.Unlock()
	}()
}

// RefreshProcessMap queries the device for running processes and builds PID->package map.
func (s *Store) RefreshProcessMap() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try modern ps format first, fall back to basic.
	// `ps -A -o PID,NAME` works on Android 8+.
	// On older versions, plain `ps` with PID + CMD columns works.
	cmd := exec.CommandContext(ctx, s.adbPath, "-s", s.serial, "shell", "ps", "-A", "-o", "PID,NAME")
	out, err := cmd.Output()
	if err != nil {
		return
	}

	newMap := make(map[int32]string)
	newPackages := make(map[string]struct{})

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.ParseInt(fields[0], 10, 32)
		if err != nil {
			continue
		}
		name := fields[len(fields)-1]
		// Filter out kernel/system processes that aren't interesting
		if isInterestingProcess(name) {
			newMap[int32(pid)] = name
			newPackages[name] = struct{}{}
		}
	}

	s.mu.Lock()
	s.pidToPackage = newMap
	s.packageSet = newPackages
	s.mu.Unlock()
}

// isInterestingProcess filters to likely app processes.
func isInterestingProcess(name string) bool {
	// Skip kernel threads and system binaries
	if strings.HasPrefix(name, "[") ||
		strings.HasPrefix(name, "system") ||
		strings.HasPrefix(name, "init") ||
		strings.HasPrefix(name, "zygote") ||
		strings.HasPrefix(name, "logd") ||
		strings.HasPrefix(name, "adbd") ||
		name == "sh" || name == "ps" {
		return false
	}
	// App processes usually have a dot (package name)
	return strings.Contains(name, ".")
}

// Snapshot returns all entries (copy).
func (s *Store) Snapshot() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]Entry, len(s.entries))
	copy(cp, s.entries)
	return cp
}

// FilterByPackage returns entries whose PID maps to the given package.
// Empty package returns all entries.
func (s *Store) FilterByPackage(pkg string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if pkg == "" {
		cp := make([]Entry, len(s.entries))
		copy(cp, s.entries)
		return cp
	}
	var out []Entry
	for _, e := range s.entries {
		pid := parsePID(e.PID)
		if p, ok := s.pidToPackage[pid]; ok && p == pkg {
			out = append(out, e)
		}
	}
	return out
}

// Packages returns the sorted list of known package names.
func (s *Store) Packages() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.packageSet))
	for p := range s.packageSet {
		out = append(out, p)
	}
	// insertion sort for small lists
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Len returns total entries stored.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Clear resets the store.
func (s *Store) Clear() {
	s.mu.Lock()
	s.entries = nil
	s.mu.Unlock()
}

func parsePID(pidStr string) int32 {
	pid, err := strconv.ParseInt(strings.TrimSpace(pidStr), 10, 32)
	if err != nil {
		return -1
	}
	return int32(pid)
}
