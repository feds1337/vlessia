package core

import (
	"sync"
	"time"

	"github.com/feds1337/vlessia/internal/store"
)

// HistoryLen is how many one-second speed samples the meter keeps.
const HistoryLen = 90

// Sample is one second of traffic through the VPN, in bytes.
type Sample struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// TrafficInfo is what the UI shows: the current speed, its recent history
// (oldest first) and the totals of this session and of today.
type TrafficInfo struct {
	Up          int64    `json:"up"`
	Down        int64    `json:"down"`
	History     []Sample `json:"history"`
	SessionUp   int64    `json:"sessionUp"`
	SessionDown int64    `json:"sessionDown"`
	TodayUp     int64    `json:"todayUp"`
	TodayDown   int64    `json:"todayDown"`
}

// Meter accumulates traffic. Today's total is kept in the store, written
// at most once a minute and when a session ends.
type Meter struct {
	st *store.Store

	mu        sync.Mutex
	hist      []Sample
	session   Sample
	today     store.Traffic
	lastFlush time.Time
	now       func() time.Time // for tests
}

func NewMeter(st *store.Store) *Meter {
	m := &Meter{st: st, now: time.Now}
	st.Read(func(d *store.Data) { m.today = d.Today })
	return m
}

// StartSession clears the history and the session totals.
func (m *Meter) StartSession() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hist = m.hist[:0]
	m.session = Sample{}
}

// Add records one second of traffic.
func (m *Meter) Add(up, down int64) {
	m.mu.Lock()
	if len(m.hist) == HistoryLen {
		copy(m.hist, m.hist[1:])
		m.hist = m.hist[:HistoryLen-1]
	}
	m.hist = append(m.hist, Sample{up, down})
	m.session.Up += up
	m.session.Down += down
	day := m.now().Format("2006-01-02")
	if m.today.Day != day {
		m.today = store.Traffic{Day: day}
	}
	m.today.Up += up
	m.today.Down += down
	flush := m.now().Sub(m.lastFlush) >= time.Minute
	m.mu.Unlock()
	if flush {
		m.Flush()
	}
}

// Flush writes today's total to the store.
func (m *Meter) Flush() {
	m.mu.Lock()
	today := m.today
	m.lastFlush = m.now()
	m.mu.Unlock()
	_ = m.st.Update(func(d *store.Data) { d.Today = today })
}

// Info returns the current numbers.
func (m *Meter) Info() TrafficInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := TrafficInfo{
		History:     append([]Sample{}, m.hist...),
		SessionUp:   m.session.Up,
		SessionDown: m.session.Down,
	}
	if n := len(m.hist); n > 0 {
		info.Up, info.Down = m.hist[n-1].Up, m.hist[n-1].Down
	}
	if m.today.Day == m.now().Format("2006-01-02") {
		info.TodayUp, info.TodayDown = m.today.Up, m.today.Down
	}
	return info
}
