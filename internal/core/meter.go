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
	daily     []store.Traffic
	lastFlush time.Time
	now       func() time.Time // for tests
}

func NewMeter(st *store.Store) *Meter {
	m := &Meter{st: st, now: time.Now}
	st.Read(func(d *store.Data) { m.today, m.daily = d.Today, d.DailyTraffic })
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
		if m.today.Day != "" {
			m.daily = append(m.daily, m.today)
		}
		for i, past := range m.daily {
			if past.Day == day {
				m.today = past
				m.daily = append(m.daily[:i], m.daily[i+1:]...)
				break
			}
		}
	}
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
	defer m.mu.Unlock()
	first := m.now().AddDate(0, 0, -29).Format("2006-01-02")
	daily := make([]store.Traffic, 0, 30)
	for _, day := range m.daily {
		if day.Day >= first {
			daily = append(daily, day)
		}
	}
	m.daily = daily
	_ = m.st.Update(func(d *store.Data) {
		d.Today, d.DailyTraffic = m.today, append([]store.Traffic(nil), daily...)
	})
	m.lastFlush = m.now()
}

type TrafficHistory struct {
	Days        []store.Traffic    `json:"days"`
	Connections []store.Connection `json:"connections"`
}

func (m *Meter) History() TrafficHistory {
	m.mu.Lock()
	defer m.mu.Unlock()
	history := TrafficHistory{Days: make([]store.Traffic, 0, 30)}
	totals := make(map[string]store.Traffic, len(m.daily)+1)
	for _, day := range m.daily {
		totals[day.Day] = day
	}
	totals[m.today.Day] = m.today
	now := m.now()
	for i := 29; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		day := totals[date]
		day.Day = date
		history.Days = append(history.Days, day)
	}
	m.st.Read(func(d *store.Data) { history.Connections = d.Connections })
	return history
}

func (m *Meter) RecordConnection(server, mode string) error {
	return m.st.Update(func(d *store.Data) {
		d.Connections = append(d.Connections, store.Connection{Started: m.now().Unix(), Server: server, Mode: mode})
		if len(d.Connections) > 20 {
			d.Connections = d.Connections[len(d.Connections)-20:]
		}
	})
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
