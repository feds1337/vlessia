package core

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/feds1337/vlessia/internal/store"
)

// Reminder alert bits, kept in store.Subscription.Notified. An alert fires
// when its bit turns on and is re-fired after the condition has cleared.
const (
	RemindTraffic10  = 1 << iota // 10% or less of the traffic left
	RemindTraffic25              // 25% or less
	RemindTrafficOut             // the traffic is used up
	RemindExpire1                // expires within a day
	RemindExpire7                // expires within a week
	RemindExpired                // has expired
)

// CheckReminders sends an alert for every subscription whose traffic or
// expiry crossed a warning threshold since the previous check, and
// remembers it so the user is not warned twice. It is called after
// subscription updates; call it once at start too.
func (l *Library) CheckReminders() {
	if l.Remind == nil {
		return
	}
	now := time.Now().Unix()
	var due []string
	_ = l.Store.Update(func(d *store.Data) {
		for _, s := range d.Subscriptions {
			if text := remindText(s, now); text != "" {
				due = append(due, text)
			}
		}
	})
	for _, text := range due {
		l.Remind(text)
	}
}

// remindText returns a new alert for sub and updates its Notified mask in
// place (the caller holds the store update). "" means nothing new.
func remindText(sub *store.Subscription, now int64) string {
	name := sub.Name
	if name == "" {
		name = sub.Title
	}
	if name == "" {
		name = "подписка"
	}

	var mask int
	var texts []string
	if sub.HasUserInfo && sub.Total > 0 {
		used := sub.Upload + sub.Download
		left := sub.Total - used
		switch {
		case left <= 0:
			mask |= RemindTrafficOut
			texts = append(texts, "трафик закончился")
		case float64(left) <= 0.10*float64(sub.Total):
			mask |= RemindTraffic10
			texts = append(texts, "осталось "+fmtPct(sub.Total, left)+" трафика")
		case float64(left) <= 0.25*float64(sub.Total):
			mask |= RemindTraffic25
			texts = append(texts, "осталось "+fmtPct(sub.Total, left)+" трафика")
		}
	}
	if sub.Expire > 0 {
		days := math.Ceil((float64(sub.Expire) - float64(now)) / 86400)
		switch {
		case days <= 0:
			mask |= RemindExpired
			texts = append(texts, "подписка истекла")
		case days <= 1:
			mask |= RemindExpire1
			texts = append(texts, "подписка истекает завтра")
		case days <= 7:
			mask |= RemindExpire7
			texts = append(texts, fmt.Sprintf("подписка истекает через %g дн.", days))
		}
	}

	old := sub.Notified
	if len(texts) == 0 {
		// The condition cleared (a top-up, a new term): allow the alert
		// again later.
		sub.Notified = 0
		return ""
	}
	sub.Notified = mask
	if mask&^old == 0 {
		return "" // already warned for every current condition
	}
	return fmt.Sprintf("Подписка «%s»: %s", name, strings.Join(texts, ", "))
}

// fmtPct renders the left traffic together with the percentage, e.g.
// "12% (5.2 ГБ)".
func fmtPct(total, left int64) string {
	pct := int(math.Round(100 * float64(left) / float64(total)))
	return fmt.Sprintf("%d%% (%s)", pct, fmtBytes(left))
}

// fmtBytes renders a byte count in binary units, the way the UI does.
func fmtBytes(b int64) string {
	units := []string{"Б", "КБ", "МБ", "ГБ", "ТБ"}
	v := float64(b)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d %s", b, units[0])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
