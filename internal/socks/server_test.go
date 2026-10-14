package socks

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLogErrorRateLimit(t *testing.T) {
	var lines []string
	s := &Server{logf: func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) }}

	s.logError(errors.New("connect [2001:db8::1]:443: network is unreachable"))
	if len(lines) != 0 {
		t.Fatalf("expected IPv6 unreachable to be skipped, got %v", lines)
	}
	for i := 0; i < 20; i++ {
		s.logError(fmt.Errorf("connect 1.2.3.4:443: timeout %d", i))
	}
	if len(lines) != logBurst {
		t.Fatalf("logged %d lines, want %d", len(lines), logBurst)
	}
	s.logWindow = time.Now().Add(-logWindow) // next window
	s.logError(errors.New("another"))
	if !strings.Contains(lines[logBurst], "ещё 15") || !strings.Contains(lines[logBurst+1], "another") {
		t.Fatalf("expected a summary and the new error, got %v", lines[logBurst:])
	}
}
