package runtime

import (
	"net/http"
	"testing"
	"time"
)

func TestDashboardReadersDoNotSerializeBehindNotificationProjection(t *testing.T) {
	s := dashboardTestServer(t)

	// The notification projection has a local side effect (shown feedback), so
	// it owns a narrower lock. Holding that lock simulates a slow projection
	// without holding DashboardServer's global exclusive lock.
	s.notificationMu.Lock()
	defer s.notificationMu.Unlock()

	notificationsDone := make(chan int, 1)
	go func() {
		notificationsDone <- dashboardRequest(s, http.MethodGet, "/api/notifications/overview", "").Code
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.mu.TryLock() {
			s.mu.Unlock()
			time.Sleep(time.Millisecond)
			continue
		}
		break
	}
	if s.mu.TryLock() {
		s.mu.Unlock()
		select {
		case code := <-notificationsDone:
			t.Fatalf("notification request completed before reaching its projection lock: status=%d", code)
		case <-time.After(2 * time.Second):
			t.Fatal("notification request did not acquire the shared read lock")
		}
	}

	dashboardDone := make(chan int, 1)
	go func() {
		dashboardDone <- dashboardRequest(s, http.MethodGet, "/api/dashboard", "").Code
	}()
	select {
	case code := <-dashboardDone:
		if code != http.StatusOK {
			t.Fatalf("dashboard reader status=%d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dashboard reader serialized behind notification projection")
	}

	select {
	case code := <-notificationsDone:
		t.Fatalf("notification request unexpectedly completed while projection lock was held: status=%d", code)
	default:
	}
}
