package core

import (
	"strings"
	"testing"
	"time"

	"github.com/feds1337/vlessia/internal/store"
)

const gib = int64(1) << 30

// TestRemindTraffic: 25% and 10% fire once each, the exhaustion is its own
// alert, and clearing the condition (a top-up) allows the alerts again.
func TestRemindTraffic(t *testing.T) {
	now := time.Now().Unix()
	sub := &store.Subscription{
		Name:        "Test",
		HasUserInfo: true,
		Total:       100 * gib,
		Upload:      79 * gib,
		Expire:      now + 30*86400,
	}

	// 21% left: the 25% warning.
	text := remindText(sub, now)
	if !strings.Contains(text, "21%") || !strings.Contains(text, "Подписка «Test»") {
		t.Fatalf("first alert: %q", text)
	}
	if !strings.Contains(text, "трафика") || strings.Contains(text, "закончился") {
		t.Fatalf("first alert text: %q", text)
	}
	if got := remindText(sub, now); got != "" {
		t.Fatalf("the 25%% alert repeated: %q", got)
	}

	// 8% left: the 10% warning is still new.
	sub.Upload = 92 * gib
	text = remindText(sub, now)
	if !strings.Contains(text, "8%") {
		t.Fatalf("second alert: %q", text)
	}
	if got := remindText(sub, now); got != "" {
		t.Fatalf("the 10%% alert repeated: %q", got)
	}

	// Used up.
	sub.Upload = 100 * gib
	text = remindText(sub, now)
	if !strings.Contains(text, "трафик закончился") {
		t.Fatalf("exhausted alert: %q", text)
	}
	if sub.Notified&RemindTrafficOut == 0 {
		t.Fatalf("Notified = %b: the exhausted bit is not set", sub.Notified)
	}

	// A top-up clears the state, so the next drop warns again.
	sub.Upload, sub.Download = 0, 0
	if got := remindText(sub, now); got != "" {
		t.Fatalf("alert after a top-up: %q", got)
	}
	if sub.Notified != 0 {
		t.Fatalf("Notified = %b after a top-up", sub.Notified)
	}
	sub.Upload = 90 * gib
	if got := remindText(sub, now); got == "" {
		t.Fatal("the warning did not come back after a top-up")
	}
}

// TestRemindExpiry: a week, a day and an already expired subscription.
func TestRemindExpiry(t *testing.T) {
	now := time.Now().Unix()
	sub := &store.Subscription{Name: "Test", HasUserInfo: true, Expire: now + 5*86400}

	text := remindText(sub, now)
	if !strings.Contains(text, "истекает через 5 дн.") {
		t.Fatalf("a week ahead: %q", text)
	}
	if got := remindText(sub, now); got != "" {
		t.Fatalf("repeat: %q", got)
	}

	sub.Expire = now + 20*3600 // under a day
	text = remindText(sub, now)
	if !strings.Contains(text, "истекает завтра") {
		t.Fatalf("a day ahead: %q", text)
	}

	sub.Expire = now - 60
	text = remindText(sub, now)
	if !strings.Contains(text, "истекла") {
		t.Fatalf("expired: %q", text)
	}

	// A new term clears the state.
	sub.Expire = now + 30*86400
	if got := remindText(sub, now); got != "" {
		t.Fatalf("alert after renewal: %q", got)
	}
	if sub.Notified != 0 {
		t.Fatalf("Notified = %b after renewal", sub.Notified)
	}
}

// TestRemindUnlimited: no traffic warnings without a limit, and a
// subscription that reports nothing at all stays quiet.
func TestRemindUnlimited(t *testing.T) {
	now := time.Now().Unix()
	unlimited := &store.Subscription{Name: "Unl", HasUserInfo: true, Upload: 900 * gib}
	quiet := &store.Subscription{Name: "Quiet"}
	for _, sub := range []*store.Subscription{unlimited, quiet} {
		if got := remindText(sub, now); got != "" {
			t.Fatalf("unexpected alert for %q: %q", sub.Name, got)
		}
	}
	// An unlimited subscription with an expiry date still warns about it.
	unlimited.Expire = now + 2*86400
	if got := remindText(unlimited, now); !strings.Contains(got, "истекает через 2 дн.") {
		t.Fatalf("expiry of an unlimited subscription: %q", got)
	}
}

// TestRemindBytes: the human-readable size in the alert.
func TestRemindBytes(t *testing.T) {
	if got := fmtBytes(5 * 1024 * 1024); got != "5.0 МБ" {
		t.Errorf("fmtBytes(5 MiB) = %q", got)
	}
	if got := fmtBytes(700); got != "700 Б" {
		t.Errorf("fmtBytes(700) = %q", got)
	}
	if got := fmtPct(100*gib, 12*gib); got != "12% (12.0 ГБ)" {
		t.Errorf("fmtPct = %q", got)
	}
}
