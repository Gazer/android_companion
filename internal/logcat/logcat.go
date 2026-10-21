// Package logcat streams and parses `adb logcat` output from a device.
package logcat

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// Entry is a single parsed logcat line.
type Entry struct {
	Raw     string
	Time    string
	PID     string
	TID     string
	Level   string // V, D, I, W, E, F
	Tag     string
	Message string
}

var (
	// Matches the "threadtime" format: "MM-DD HH:MM:SS.mmm  PID  TID L TAG: message"
	// Note: after "TAG:" we strip at most ONE separating space so the
	// original message indentation (multi-line logs) is preserved.
	reThreadTime = regexp.MustCompile(`^(\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\.\d{3})\s+(\d+)\s+(\d+)\s+([VDIWEF])\s+([^:]+?): ?(.*)$`)
	// Fallback for lines that don't parse (e.g. beginning of logs markers)
	reLoose = regexp.MustCompile(`^(\S+\s+\S+)\s+(\d+)\s+(\d+)\s+([VDIWEF])\s+(.*)$`)
)

// PIDNum returns the PID as a number, or -1 if it doesn't parse.
func (e *Entry) PIDNum() int32 {
	return parsePID(e.PID)
}

// Streamer tails logcat and delivers parsed entries. It keeps no history:
// the Store is the single source of truth for past entries.
type Streamer struct {
	AdbPath string
	Serial  string

	mu   sync.RWMutex
	subs []chan Entry
	cmd  *exec.Cmd
}

// NewStreamer creates a streamer for the given adb serial.
func NewStreamer(adbPath, serial string) *Streamer {
	return &Streamer{AdbPath: adbPath, Serial: serial}
}

// Start begins streaming logcat in the background.
// The returned channel receives new entries and is closed when adb logcat
// exits: the device was disconnected, or ctx was cancelled / Stop called.
func (s *Streamer) Start(ctx context.Context) (<-chan Entry, error) {
	// threadtime format. -T 0 means "only logs from now", not the whole
	// boot backlog. This avoids flooding the UI with thousands of old lines.
	args := []string{
		"-s", s.Serial,
		"logcat",
		"-v", "threadtime",
		"-T", "0",
	}
	s.cmd = exec.CommandContext(ctx, s.AdbPath, args...)
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("logcat stdout pipe: %w", err)
	}
	if err := s.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start logcat: %w", err)
	}

	out := make(chan Entry, 4096)
	s.addSub(out)

	go func() {
		defer s.closeSubs()
		defer s.cmd.Wait()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			s.broadcast(ParseLine(line))
		}
	}()

	return out, nil
}

// Stop terminates logcat.
func (s *Streamer) Stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// Filter returns entries whose Raw matches the predicate.
func Filter(entries []Entry, substr string) []Entry {
	if substr == "" {
		return entries
	}
	sub := strings.ToLower(substr)
	out := entries[:0]
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Raw), sub) ||
			strings.Contains(strings.ToLower(e.Tag), sub) ||
			strings.Contains(strings.ToLower(e.Message), sub) {
			out = append(out, e)
		}
	}
	return out
}

// ParseLine converts one logcat line into an Entry.
func ParseLine(line string) Entry {
	e := Entry{Raw: line}
	if m := reThreadTime.FindStringSubmatch(line); m != nil {
		e.Time, e.PID, e.TID, e.Level, e.Tag, e.Message = m[1], m[2], m[3], m[4], strings.TrimSpace(m[5]), m[6]
		return e
	}
	if m := reLoose.FindStringSubmatch(line); m != nil {
		e.Time, e.PID, e.TID, e.Level = m[1], m[2], m[3], m[4]
		rest := m[5]
		if idx := strings.Index(rest, ":"); idx > 0 {
			e.Tag = strings.TrimSpace(rest[:idx])
			e.Message = strings.TrimPrefix(rest[idx+1:], " ")
		} else {
			e.Message = rest
		}
		return e
	}
	// Unparsed: keep raw as message
	e.Message = line
	return e
}

func (s *Streamer) addSub(ch chan Entry) {
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
}

func (s *Streamer) broadcast(e Entry) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.subs {
		select {
		case ch <- e:
		default: // drop if consumer is slow
		}
	}
}

func (s *Streamer) closeSubs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		close(ch)
	}
	s.subs = nil
}
