package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	r := c.responder
	req, err := r.buildRequest(http.MethodGet, fmt.Sprintf("%s/chatik/api/chat_data?chatId=%d&applicantId=%d&do_not_track_session_events=true", r.chatURL, id, r.userId), nil, map[string]string{"Accept": "application/json", "X-Requested-With": "XMLHttpRequest", "X-Xsrftoken": r.XSRFToken()})
	if err != nil {
		return nil, err
	}
	resp, err := r.requester.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if resp.Status != http.StatusOK {
		return nil, unexpectedHTTPStatus(resp.Status)
	}
	var data ChatDataResponse
	if json.Unmarshal(resp.Body, &data) != nil || data.Chat.ID != id || data.Chat.Messages.Items == nil {
		return nil, errors.New("invalid HH chat detail identity or messages")
	}
	return &data, nil
}

func (c *HHAIResponderReadClient) readConversationPage(ctx context.Context, cursor string) (HHConversationPage, error) {
	start := time.Now()
	defer perfRecord("hh.conversation_page", start, 0)
	response, err := c.readChatsContext(ctx, cursor)
	if err != nil {
		return HHConversationPage{}, err
	}
	n := len(response.Chats.Items)
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
				data, err := c.readChatData(ctx, chat.Id)
				if err != nil {
					errs[i] = err
					continue
				}
				items[i] = hhChatRecord(chat, data, response)
				if key := hhListFingerprint(chat, response); key != "" {
					items[i].Metadata["sync_list_fingerprint"] = key
					items[i].Metadata["sync_detail_at"] = time.Now().UTC().Format(time.RFC3339Nano)
				}
				reportHHReadProgress(ctx, n, int(completed.Add(1)))
			}
		}()
	}
	for i := range response.Chats.Items {
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
