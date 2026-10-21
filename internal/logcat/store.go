package logcat

import (
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// maxStoredEntries bounds memory usage. Old entries are evicted FIFO.
	maxStoredEntries = 50000

	// minProcessMapRefresh rate-limits `adb shell ps` calls triggered by
	// logs from unknown PIDs.
	minProcessMapRefresh = 2 * time.Second
)

// Store holds log entries in memory, decoupled from UI rendering.
//
// Every entry gets a monotonically increasing sequence number (kept across
// evictions and Clear), so readers can fetch only what was added since
// their last read with Collect instead of re-scanning the whole store.
type Store struct {
	mu      sync.RWMutex
	entries []Entry
	// firstSeq is the sequence number of entries[0].
	firstSeq uint64

	// pidToPackage maps PID -> package name (e.g. "com.example.app")
	pidToPackage map[int32]string
	// packageSet is the deduplicated set of known packages
	packageSet map[string]struct{}
	// seenPIDs holds every PID listed by the last process refresh,
	// including uninteresting ones (system processes), so their logs
	// don't trigger a new refresh on every line.
	seenPIDs map[int32]struct{}
	// mapVersion changes whenever pidToPackage changes.
	mapVersion uint64

	adbPath string
	serial  string

	refreshing     bool
	refreshPending bool
	lastRefresh    time.Time
}

// NewStore creates an empty log store.
// If adbPath and serial are provided, it does an initial process map refresh.
func NewStore(adbPath, serial string) *Store {
	s := &Store{
		pidToPackage: make(map[int32]string),
		packageSet:   make(map[string]struct{}),
		seenPIDs:     make(map[int32]struct{}),
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
		s.firstSeq += uint64(drop)
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
// If the PID was not listed by the last process refresh, it triggers an
// async (rate-limited) refresh.
func (s *Store) lookupPackage(pid int32) string {
	if pid <= 0 {
		return ""
	}
	s.mu.RLock()
	pkg, ok := s.pidToPackage[pid]
	_, seen := s.seenPIDs[pid]
	s.mu.RUnlock()
	if ok {
		return pkg
	}
	if !seen {
		s.asyncRefreshProcessMap()
	}
	return ""
}

// asyncRefreshProcessMap refreshes the PID->package map without blocking.
// Calls within minProcessMapRefresh of the previous refresh are coalesced
// into a single deferred refresh.
func (s *Store) asyncRefreshProcessMap() {
	s.mu.Lock()
	if s.refreshing || s.refreshPending || s.adbPath == "" {
		s.mu.Unlock()
		return
	}
	if wait := minProcessMapRefresh - time.Since(s.lastRefresh); wait > 0 {
		s.refreshPending = true
		s.mu.Unlock()
		time.AfterFunc(wait, func() {
			s.mu.Lock()
			s.refreshPending = false
			s.mu.Unlock()
			s.asyncRefreshProcessMap()
		})
		return
	}
	s.refreshing = true
	s.lastRefresh = time.Now()
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
	s.mu.RLock()
	adbPath, serial := s.adbPath, s.serial
	s.mu.RUnlock()
	if adbPath == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// `ps -A -o PID,NAME` works on Android 8+.
	cmd := exec.CommandContext(ctx, adbPath, "-s", serial, "shell", "ps", "-A", "-o", "PID,NAME")
	out, err := cmd.Output()
	if err != nil {
		return
	}

	s.setProcessMap(parseProcessList(string(out)))
}

// parseProcessList parses `ps -o PID,NAME` output. It returns the PIDs of
// interesting (app) processes mapped to their names, and the set of all
// PIDs listed.
func parseProcessList(out string) (map[int32]string, map[int32]struct{}) {
	apps := make(map[int32]string)
	seen := make(map[int32]struct{})
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.ParseInt(fields[0], 10, 32)
		if err != nil {
			continue
		}
		seen[int32(pid)] = struct{}{}
		name := fields[len(fields)-1]
		// Filter out kernel/system processes that aren't interesting
		if isInterestingProcess(name) {
			apps[int32(pid)] = name
		}
	}
	return apps, seen
}

// setProcessMap installs a new PID->package map, bumping mapVersion only
// if it differs from the current one.
func (s *Store) setProcessMap(apps map[int32]string, seen map[int32]struct{}) {
	packages := make(map[string]struct{}, len(apps))
	for _, name := range apps {
		packages[name] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !sameProcessMap(s.pidToPackage, apps) {
		s.mapVersion++
	}
	s.pidToPackage = apps
	s.packageSet = packages
	s.seenPIDs = seen
}

func sameProcessMap(a, b map[int32]string) bool {
	if len(a) != len(b) {
		return false
	}
	for pid, name := range a {
		if b[pid] != name {
			return false
		}
	}
	return true
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

// Collect returns copies of the entries with sequence number >= from that
// satisfy keep (nil keeps everything), plus the sequence number to pass
// on the next call. Entries evicted before being collected are skipped.
//
// keep runs under the store's read lock: it must be fast and must not
// call back into the Store.
func (s *Store) Collect(from uint64, keep func(*Entry) bool) ([]Entry, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	next := s.firstSeq + uint64(len(s.entries))
	if from < s.firstSeq {
		from = s.firstSeq
	}
	if from >= next {
		return nil, next
	}
	src := s.entries[from-s.firstSeq:]
	if keep == nil {
		out := make([]Entry, len(src))
		copy(out, src)
		return out, next
	}
	var out []Entry
	for i := range src {
		if keep(&src[i]) {
			out = append(out, src[i])
		}
	}
	return out, next
}

// CollectLast is like Collect(0, keep) but returns only the newest limit
// matching entries, scanning from the end so older entries are skipped.
func (s *Store) CollectLast(limit int, keep func(*Entry) bool) ([]Entry, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	next := s.firstSeq + uint64(len(s.entries))
	var out []Entry
	for i := len(s.entries) - 1; i >= 0 && len(out) < limit; i-- {
		if keep == nil || keep(&s.entries[i]) {
			out = append(out, s.entries[i])
		}
	}
	slices.Reverse(out)
	return out, next
}

// FilterByPackage returns entries whose PID maps to the given package.
// Empty package returns all entries.
func (s *Store) FilterByPackage(pkg string) []Entry {
	if pkg == "" {
		return s.Snapshot()
	}
	pids := s.PIDsForPackage(pkg)
	out, _ := s.Collect(0, func(e *Entry) bool {
		_, ok := pids[e.PIDNum()]
		return ok
	})
	return out
}

// PIDsForPackage returns the set of PIDs currently mapped to pkg.
func (s *Store) PIDsForPackage(pkg string) map[int32]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int32]struct{})
	for pid, name := range s.pidToPackage {
		if name == pkg {
			out[pid] = struct{}{}
		}
	}
	return out
}

// MapVersion returns a counter that changes every time the PID->package
// map changes.
func (s *Store) MapVersion() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mapVersion
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

// Clear resets the store. Sequence numbers keep growing.
func (s *Store) Clear() {
	s.mu.Lock()
	s.firstSeq += uint64(len(s.entries))
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
