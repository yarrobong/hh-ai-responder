package inboxrefresh

import (
	"context"
	"errors"
	"time"
)

type Service struct {
	deps    Dependencies
	options Options
}

func NewService(deps Dependencies, options Options) *Service {
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps, options: options}
}

// Run performs exactly one inbox refresh. Its order is intentionally the
// existing dashboard order: HH read, local reload, projection, notification
// refresh, then daily-state persistence.
func (s *Service) Run(ctx context.Context, input Input) (Result, error) {
	if s == nil || s.deps.Inbox == nil || s.deps.Projection == nil || s.deps.Notifications == nil || s.deps.DailyState == nil {
		return Result{}, errors.New("inbox refresh is not configured")
	}
	if ctx == nil {
		return Result{}, errors.New("inbox refresh context is nil")
	}
	now := input.Now
	if now.IsZero() {
		now = s.options.Now()
		if now.IsZero() {
			now = time.Now().UTC()
		}
	}

	syncResult, syncErr := s.deps.Inbox.RefreshInbox(ctx)
	result := Result{Sync: syncResult}

	// The dashboard always attempted the local reload after the HH operation,
	// even when the HH operation returned an error. Preserve that ordering and
	// preserve the original sync error as the caller-visible error.
	reloadErr := s.deps.Projection.Reload(ctx)
	if syncErr != nil {
		return result, syncErr
	}
	if reloadErr != nil {
		return result, reloadErr
	}

	projection, err := s.deps.Projection.Build(ctx, now)
	result.Projection = projection
	if err != nil {
		return result, err
	}

	notifications, err := s.deps.Notifications.Refresh(ctx, now)
	result.Notifications = notifications
	if err != nil {
		return result, err
	}

	dailyState, err := s.deps.DailyState.Save(ctx, DailyStateInput{Now: now, Sync: syncResult, Projection: projection})
	result.DailyState = dailyState
	if err != nil {
		return result, err
	}
	return result, nil
}
