package main

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestStage221SafetyPriorityBeatsQueuedBackgroundRead(t *testing.T) {
	r := NewHHRequester(context.Background(), nil, time.Millisecond)
	r.readConcurrency = 1
	if err := r.acquireRead(withHHReadPriority(context.Background(), hhReadBackground)); err != nil {
		t.Fatal(err)
	}
	backgroundGranted := make(chan struct{})
	safetyGranted := make(chan struct{})
	go func() {
		if err := r.acquireRead(withHHReadPriority(context.Background(), hhReadBackground)); err == nil {
			close(backgroundGranted)
		}
	}()
	go func() {
		if err := r.acquireRead(withHHReadPriority(context.Background(), hhReadSafety)); err == nil {
			close(safetyGranted)
		}
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		queued := len(r.readQueue)
		r.mu.Unlock()
		if queued == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	r.releaseRead()
	select {
	case <-safetyGranted:
	case <-backgroundGranted:
		t.Fatal("background read was granted before safety read")
	case <-time.After(time.Second):
		t.Fatal("priority scheduler did not grant a read")
	}
	r.releaseRead()
	select {
	case <-backgroundGranted:
	case <-time.After(time.Second):
		t.Fatal("queued background read did not finish")
	}
	r.releaseRead()
}

type stage221TargetedOnlyClient struct {
	targeted atomic.Int32
	full     atomic.Int32
}

func (c *stage221TargetedOnlyClient) ReadVacancies(context.Context, string) (HHVacancyPage, error) {
	return HHVacancyPage{}, nil
}
func (c *stage221TargetedOnlyClient) ReadApplications(context.Context, string) (HHApplicationPage, error) {
	return HHApplicationPage{}, nil
}
func (c *stage221TargetedOnlyClient) ReadConversations(context.Context, string) (HHConversationPage, error) {
	c.full.Add(1)
	return HHConversationPage{}, nil
}
func (c *stage221TargetedOnlyClient) ReadConversation(context.Context, string) (HHConversationRecord, error) {
	c.targeted.Add(1)
	return HHConversationRecord{ExternalID: "42", Status: "RESPONSE", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}

func TestStage221TargetedSyncNeverFallsBackToFullConversationSync(t *testing.T) {
	client := &stage221TargetedOnlyClient{}
	store := NewConversationStore(filepath.Join(t.TempDir(), EmployerConversationsFilename))
	service := NewHHReadSyncService(client, store, filepath.Join(t.TempDir(), HHSyncStateFilename))
	result, err := service.SyncConversation("42", context.Background())
	if err != nil || len(result.Errors) != 0 || client.targeted.Load() != 1 || client.full.Load() != 0 {
		t.Fatalf("targeted path invoked an unexpected read: result=%+v targeted=%d full=%d err=%v", result, client.targeted.Load(), client.full.Load(), err)
	}
}

func TestStage221IncrementalProgressCarriesReuseCounters(t *testing.T) {
	client := &stage221ProgressClient{}
	store := NewConversationStore(filepath.Join(t.TempDir(), EmployerConversationsFilename))
	service := NewHHReadSyncService(client, store, filepath.Join(t.TempDir(), HHSyncStateFilename))
	result, err := service.syncRead(context.Background(), "inbox")
	if err != nil || result.MetadataChecked != 3 || result.HistoryReused != 2 || result.DetailedChatsFetched != 1 {
		t.Fatalf("incremental counters missing: result=%+v err=%v", result, err)
	}
}

type stage221ProgressClient struct{}

func (stage221ProgressClient) ReadVacancies(context.Context, string) (HHVacancyPage, error) {
	return HHVacancyPage{}, nil
}
func (stage221ProgressClient) ReadApplications(context.Context, string) (HHApplicationPage, error) {
	return HHApplicationPage{}, nil
}
func (stage221ProgressClient) ReadConversations(context.Context, string) (HHConversationPage, error) {
	return HHConversationPage{Items: []HHConversationRecord{{ExternalID: "1", MetadataUnchanged: true}, {ExternalID: "2", MetadataUnchanged: true}, {ExternalID: "3", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Messages: []HHMessageRecord{{ExternalID: "m", Sender: "employer", Text: "hello", Timestamp: time.Now().UTC()}}}}, MetadataChecked: 3, HistoryReused: 2, DetailedChatsFetched: 1}, nil
}
