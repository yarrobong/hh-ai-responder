package inboxrefresh

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeInbox struct {
	log *[]string
	err error
}

func (f fakeInbox) RefreshInbox(ctx context.Context) (SyncResult, error) {
	*f.log = append(*f.log, "sync")
	if err := ctx.Err(); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Fetched: 2}, f.err
}

type fakeProjection struct {
	log       *[]string
	reloadErr error
	buildErr  error
}

func (f fakeProjection) Reload(context.Context) error {
	*f.log = append(*f.log, "reload")
	return f.reloadErr
}

func (f fakeProjection) Build(context.Context, time.Time) (Projection, error) {
	*f.log = append(*f.log, "projection")
	return Projection{WorkflowItems: 2}, f.buildErr
}

type fakeNotifications struct {
	log *[]string
	err error
}

func (f fakeNotifications) Refresh(context.Context, time.Time) (NotificationResult, error) {
	*f.log = append(*f.log, "notifications")
	return NotificationResult{Created: 1}, f.err
}

type fakeDailyState struct {
	log *[]string
	err error
}

func (f fakeDailyState) Save(context.Context, DailyStateInput) (DailyStateResult, error) {
	*f.log = append(*f.log, "daily-state")
	return DailyStateResult{Persisted: true}, f.err
}

func newTestService(log *[]string, syncErr, reloadErr, projectionErr, notificationErr, stateErr error) *Service {
	return NewService(Dependencies{
		Inbox:         fakeInbox{log: log, err: syncErr},
		Projection:    fakeProjection{log: log, reloadErr: reloadErr, buildErr: projectionErr},
		Notifications: fakeNotifications{log: log, err: notificationErr},
		DailyState:    fakeDailyState{log: log, err: stateErr},
	}, Options{Now: func() time.Time { return time.Unix(100, 0).UTC() }})
}

func TestServiceRunPreservesPostSyncOrder(t *testing.T) {
	log := []string{}
	result, err := newTestService(&log, nil, nil, nil, nil, nil).Run(context.Background(), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"sync", "reload", "projection", "notifications", "daily-state"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("order=%v want=%v", log, want)
	}
	if result.Sync.Fetched != 2 || result.Projection.WorkflowItems != 2 || result.Notifications.Created != 1 || !result.DailyState.Persisted {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestServiceRunSyncErrorStillReloadsAndStopsPostProcessing(t *testing.T) {
	log := []string{}
	wantErr := errors.New("sync failed")
	_, err := newTestService(&log, wantErr, nil, nil, nil, nil).Run(context.Background(), Input{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want=%v", err, wantErr)
	}
	if want := []string{"sync", "reload"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("order=%v want=%v", log, want)
	}
}

func TestServiceRunReloadErrorStopsAfterReload(t *testing.T) {
	log := []string{}
	wantErr := errors.New("reload failed")
	_, err := newTestService(&log, nil, wantErr, nil, nil, nil).Run(context.Background(), Input{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want=%v", err, wantErr)
	}
	if want := []string{"sync", "reload"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("order=%v want=%v", log, want)
	}
}

func TestServiceRunStopsAtEachPostSyncFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{name: "projection", err: errors.New("projection failed"), want: []string{"sync", "reload", "projection"}},
		{name: "notifications", err: errors.New("notifications failed"), want: []string{"sync", "reload", "projection", "notifications"}},
		{name: "daily state", err: errors.New("state failed"), want: []string{"sync", "reload", "projection", "notifications", "daily-state"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := []string{}
			var service *Service
			switch test.name {
			case "projection":
				service = newTestService(&log, nil, nil, test.err, nil, nil)
			case "notifications":
				service = newTestService(&log, nil, nil, nil, test.err, nil)
			default:
				service = newTestService(&log, nil, nil, nil, nil, test.err)
			}
			if _, err := service.Run(context.Background(), Input{}); !errors.Is(err, test.err) {
				t.Fatalf("err=%v want=%v", err, test.err)
			}
			if !reflect.DeepEqual(log, test.want) {
				t.Fatalf("order=%v want=%v", log, test.want)
			}
		})
	}
}

func TestServiceRunHonorsCancellation(t *testing.T) {
	log := []string{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := newTestService(&log, nil, nil, nil, nil, nil)
	if _, err := service.Run(ctx, Input{}); err == nil {
		t.Fatal("expected cancellation from read boundary")
	}
}
