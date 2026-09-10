package runtime

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Reads only. The detail response carries its own destination, topic and vacancy
// resources; listing every other chat is unnecessary even on a cold start.
func (c *HHAIResponderReadClient) readChatData(ctx context.Context, id int64) (*ChatDataResponse, error) {
	start := time.Now()
	defer perfRecord("hh.chat_detail", start, 1)
	adapter, err := c.readAdapter()
	if err != nil {
		return nil, err
	}
	history, err := adapter.ReadChatHistory(ctx, id, c.responder.userId)
	if err != nil {
		return nil, err
	}
	result := &ChatDataResponse{Chat: ChatDetail{ID: history.ID, Messages: ChatMessages{Items: []ChatMessage{}}}, ChatStates: ChatStates{WriteMessageState: StateAllowed{Allowed: history.WriteAllowed}}}
	for _, message := range history.Messages {
		result.Chat.Messages.Items = append(result.Chat.Messages.Items, *legacyChatMessage(message))
	}
	return result, nil
}

func (c *HHAIResponderReadClient) readConversationPage(ctx context.Context, cursor string) (HHConversationPage, error) {
	return c.readConversationPageBounded(ctx, cursor, 0)
}

// ReadConversationsBounded is the operator one-run read path. It keeps the
// provider's order and applies the prefix limit before any chat detail read.
func (c *HHAIResponderReadClient) ReadConversationsBounded(ctx context.Context, cursor string, limit int) (HHConversationPage, error) {
	if limit < 0 {
		return HHConversationPage{}, errors.New("HH conversation limit must be non-negative")
	}
	return c.readConversationPageBounded(ctx, cursor, limit)
}

func (c *HHAIResponderReadClient) readConversationPageBounded(ctx context.Context, cursor string, limit int) (HHConversationPage, error) {
	start := time.Now()
	defer perfRecord("hh.conversation_page", start, 0)
	adapter, err := c.readAdapter()
	if err != nil {
		return HHConversationPage{}, err
	}
	listStart := time.Now()
	page, err := adapter.ReadChatList(ctx, cursor)
	meterRecord(ctx, "network", time.Since(listStart))
	if err != nil {
		return HHConversationPage{}, err
	}
	response := legacyChatsResponse(page)
	n := len(response.Chats.Items)
	if limit > 0 && n > limit {
		n = limit
	}
	reportHHReadProgress(ctx, n, 0)
	var completed atomic.Int64
	var historyReused atomic.Int64
	var detailedFetched atomic.Int64
	items := make([]HHConversationRecord, n)
	errs := make([]error, n)
	workers := c.responder.requester.readConcurrency
	if workers < 1 {
		workers = 4
	}
	if workers > 8 {
		workers = 8
	}
	if workers > n {
		workers = n
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					errs[i] = err
					continue
				}
				chat := response.Chats.Items[i]
				if hhMetadataUnchanged(ctx, chat, response) {
					meterRecord(ctx, "hit", 0)
					historyReused.Add(1)
					items[i] = HHConversationRecord{ExternalID: strconv.FormatInt(chat.Id, 10), MetadataUnchanged: true}
					reportHHReadProgress(ctx, n, int(completed.Add(1)))
					continue
				}
				meterRecord(ctx, "miss", 0)
				detailedFetched.Add(1)
				detailStart := time.Now()
				record, err := adapter.ReadConversation(ctx, strconv.FormatInt(chat.Id, 10))
				meterRecord(ctx, "network", time.Since(detailStart))
				if err != nil {
					errs[i] = err
					continue
				}
				items[i] = record
				if key := hhListFingerprint(chat, response); key != "" {
					items[i].Metadata["sync_list_fingerprint"] = key
					items[i].Metadata["sync_detail_at"] = time.Now().UTC().Format(time.RFC3339Nano)
				}
				reportHHReadProgress(ctx, n, int(completed.Add(1)))
			}
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return HHConversationPage{}, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	// Deterministic import order and fail closed on incomplete pages.
	for _, err := range errs {
		if err != nil {
			return HHConversationPage{}, err
		}
	}
	return HHConversationPage{Items: items, NextCursor: response.Chats.NextFrom, MetadataChecked: n, HistoryReused: int(historyReused.Load()), DetailedChatsFetched: int(detailedFetched.Load())}, nil
}

func (r *HHRequester) Do(req *http.Request) (*HHResponse, error) {
	if req == nil {
		return nil, errors.New("HH read request is nil")
	}
	// HHRequester is the shared read transport. Keep its public compatibility
	// method incapable of dispatching mutations; all HH writes must enter via
	// the narrow hhwrite capabilities and gateway.
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return nil, errors.New("HH read requester blocks state-changing methods")
	}
	for attempt := 0; ; attempt++ {
		response, err := r.doOnce(req)
		if err != nil || response.Status != http.StatusTooManyRequests || (req.Method != http.MethodGet && req.Method != http.MethodHead) {
			return response, err
		}
		delay := time.Second * time.Duration(1<<attempt)
		if seconds, err := strconv.Atoi(response.RetryAfter); err == nil && seconds > 0 {
			delay = max(delay, time.Duration(seconds)*time.Second)
		} else if until, err := http.ParseTime(response.RetryAfter); err == nil {
			delay = max(delay, time.Until(until))
		}
		r.mu.Lock()
		until := time.Now().Add(delay)
		if until.After(r.readNotBefore) {
			r.readNotBefore = until
		}
		r.signalReadSchedulerLocked()
		r.mu.Unlock()
		perfRecord("hh.rate_limited", time.Now(), 1)
		if attempt >= 2 {
			return response, nil
		}
	}
}
