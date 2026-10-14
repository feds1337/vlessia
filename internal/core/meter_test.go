package core

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/feds1337/vlessia/internal/profile"
	"github.com/feds1337/vlessia/internal/store"
)

func TestMeter(t *testing.T) {
	l := newLibrary(t)
	clock := time.Date(2026, 10, 14, 23, 59, 0, 0, time.Local)
	m := NewMeter(l.Store)
	m.now = func() time.Time { return clock }
	m.StartSession()
	for i := 0; i < HistoryLen+10; i++ {
		m.Add(1, 10)
	}
	info := m.Info()
	if len(info.History) != HistoryLen || info.SessionUp != HistoryLen+10 || info.TodayDown != (HistoryLen+10)*10 || info.Down != 10 {
		t.Fatalf("info %+v", info)
	}

	// Midnight starts a new day, not a new session.
	clock = clock.Add(2 * time.Minute)
	m.Add(5, 50)
	info = m.Info()
	if info.TodayUp != 5 || info.TodayDown != 50 || info.SessionUp != HistoryLen+15 {
		t.Fatalf("after midnight %+v", info)
	}

	// Today's total survives a restart.
	m.Flush()
	again := NewMeter(l.Store)
	again.now = m.now
	if got := again.Info(); got.TodayDown != 50 || got.SessionDown != 0 {
		t.Fatalf("reloaded %+v", got)
	}
	var stored store.Traffic
	l.Store.Read(func(d *store.Data) { stored = d.Today })
	if stored.Day != "2026-10-15" {
		t.Fatalf("stored %+v", stored)
	}
}

func TestWatchdog(t *testing.T) {
	quiet := func(string, ...any) {}
	run := func(check func() error, online bool) (failed bool, healthy int32) {
		var h atomic.Int32
		done := make(chan bool, 1)
		stop := make(chan struct{})
		go func() {
			Watchdog{
				Check:    check,
				Online:   func() bool { return online },
				Healthy:  func() { h.Add(1) },
				Failed:   func() { done <- true },
				Logf:     quiet,
				Interval: 5 * time.Millisecond,
			}.Run(stop)
			done <- false
		}()
		select {
		case failed = <-done:
		case <-time.After(200 * time.Millisecond):
		}
		close(stop)
		return failed, h.Load()
	}

	if failed, healthy := run(func() error { return nil }, true); failed || healthy == 0 {
		t.Errorf("working connection: failed=%v healthy=%d", failed, healthy)
	}
	if failed, _ := run(func() error { return errors.New("timeout") }, true); !failed {
		t.Error("dead connection was not reported")
	}
	if failed, _ := run(func() error { return errors.New("timeout") }, false); failed {
		t.Error("reported a dead VPN while the whole internet was down")
	}
	// One failed check among good ones is not enough.
	var n atomic.Int32
	flaky := func() error {
		if n.Add(1)%2 == 0 {
			return errors.New("blip")
		}
		return nil
	}
	if failed, _ := run(flaky, true); failed {
		t.Error("a single failed check counted as dead")
	}
}

func TestGroup(t *testing.T) {
	a := &profile.Server{ID: "a", SubID: "s1"}
	b := &profile.Server{ID: "b", SubID: "s1"}
	c := &profile.Server{ID: "c", SubID: "s2"}
	m := &profile.Server{ID: "m"}
	all := []*profile.Server{a, b, c, m}
	if g := Group(all, a, "a"); len(g) != 1 || g[0] != b {
		t.Errorf("group of a without a: %v", g)
	}
	if g := Group(all, m); len(g) != 1 || g[0] != m {
		t.Errorf("manual group: %v", g)
	}
}
