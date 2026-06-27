package chromedp

import (
	"context"
	"testing"
	"time"
)

// newTestInstance builds a ChromeInstance without launching Chrome: the reaper
// only touches LastUsed, TTL, inFlight, and Cancel, so a plain cancellable
// context is enough to exercise the lifecycle logic.
func newTestInstance(ttl time.Duration, lastUsed time.Time) (*ChromeInstance, *bool) {
	_, cancel := context.WithCancel(context.Background())
	cancelled := false
	return &ChromeInstance{
		TTL:      ttl,
		LastUsed: lastUsed,
		Cancel: func() {
			cancelled = true
			cancel()
		},
	}, &cancelled
}

func TestCleanupSkipsInFlightInstance(t *testing.T) {
	cm := &ChromeManager{
		instances: make(map[string]*ChromeInstance),
		maximum:   5,
		ttl:       time.Minute,
	}

	// Expired (LastUsed well past TTL) but an action is in flight — must survive.
	busy, busyCancelled := newTestInstance(time.Minute, time.Now().Add(-10*time.Minute))
	busy.inFlight.Store(1)
	cm.instances["busy"] = busy

	// Expired and idle — must be reaped.
	idle, idleCancelled := newTestInstance(time.Minute, time.Now().Add(-10*time.Minute))
	cm.instances["idle"] = idle

	// Fresh and idle — must survive.
	fresh, freshCancelled := newTestInstance(time.Minute, time.Now())
	cm.instances["fresh"] = fresh

	cm.cleanupExpiredInstances()

	if *busyCancelled {
		t.Error("in-flight instance was cancelled by the reaper")
	}
	if _, ok := cm.instances["busy"]; !ok {
		t.Error("in-flight instance was deleted by the reaper")
	}
	if !*idleCancelled {
		t.Error("expired idle instance was not cancelled")
	}
	if _, ok := cm.instances["idle"]; ok {
		t.Error("expired idle instance was not deleted")
	}
	if *freshCancelled {
		t.Error("fresh instance was cancelled")
	}
}

func TestGetInstanceDoesNotExpireInFlight(t *testing.T) {
	cm := &ChromeManager{
		instances: make(map[string]*ChromeInstance),
		maximum:   5,
		ttl:       time.Minute,
	}
	busy, busyCancelled := newTestInstance(time.Minute, time.Now().Add(-10*time.Minute))
	busy.inFlight.Store(1)
	cm.instances["busy"] = busy

	if _, err := cm.GetInstance("busy"); err != nil {
		t.Fatalf("GetInstance expired an in-flight instance: %v", err)
	}
	if *busyCancelled {
		t.Error("GetInstance cancelled an in-flight instance")
	}
}

func TestTouchRefreshesIdleClock(t *testing.T) {
	cm := &ChromeManager{
		instances: make(map[string]*ChromeInstance),
		maximum:   5,
		ttl:       time.Minute,
	}
	old := time.Now().Add(-30 * time.Second)
	inst, _ := newTestInstance(time.Minute, old)
	cm.instances["x"] = inst

	cm.touch("x")

	if !cm.instances["x"].LastUsed.After(old) {
		t.Error("touch did not advance LastUsed")
	}
}
