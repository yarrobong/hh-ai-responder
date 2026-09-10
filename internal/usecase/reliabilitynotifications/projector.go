// Package reliabilitynotifications projects typed local reliability outcomes
// into bounded, advisory operator notifications. It has no HH or AI
// capability and never changes attempt/action state.
package reliabilitynotifications

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Kind string

const (
	ApplicationDeliveryUncertain           Kind = "application_delivery_uncertain"
	ApplicationResponseConfirmed           Kind = "application_response_confirmed"
	ApplicationReconciliationConflict      Kind = "application_reconciliation_conflict"
	ApplicationStoreUnavailable            Kind = "application_store_unavailable"
	AutoChatDeliveryUncertain              Kind = "autochat_delivery_uncertain"
	AutoChatReplyConfirmed                 Kind = "autochat_reply_confirmed"
	AutoChatReconciliationConflict         Kind = "autochat_reconciliation_conflict"
	AutoChatLeaveReconciliationUnsupported Kind = "autochat_leave_reconciliation_unsupported"
	AutoChatStoreUnavailable               Kind = "autochat_store_unavailable"
)

type Priority string

const (
	PriorityHigh   Priority = "HIGH"
	PriorityMedium Priority = "MEDIUM"
)

// Record is the storage-neutral advisory notification shape. Key is the
// stable incident identity; it is deliberately not derived from human text.
type Record struct {
	Key              string
	Kind             Kind
	Priority         Priority
	Message          string
	AttemptID        string
	VacancyID        int
	ConversationID   string
	TriggerMessageID string
	ActionType       string
	DetailPath       string
	Resolve          bool
}

// Store is intentionally limited to notification persistence. Implementations
// must not expose attempt reservation, reconciliation, HH reads/writes, or AI.
type Store interface {
	UpsertReliability(context.Context, Record) error
}

// Sink is the narrow capability consumed by reliability workflows. Calls are
// best effort at the workflow boundary: a projection failure must never
// authorize a retry or alter the durable safety result.
type Sink interface {
	ProjectApplicationOutcome(context.Context, ApplicationOutcome) error
	ProjectApplicationReconciliation(context.Context, ApplicationReconciliation) error
	ProjectApplicationStoreUnavailable(context.Context, ApplicationStoreHealth) error
	ProjectAutoChatOutcome(context.Context, AutoChatOutcome) error
	ProjectAutoChatReconciliation(context.Context, AutoChatReconciliation) error
	ProjectAutoChatStoreUnavailable(context.Context, AutoChatStoreHealth) error
}

type Projector struct{ store Store }

func NewProjector(store Store) *Projector { return &Projector{store: store} }

type ApplicationOutcome struct {
	AttemptID            string
	VacancyID            int
	State                string
	PersistenceUncertain bool
}

type ApplicationReconciliation struct {
	AttemptID string
	VacancyID int
	Status    string
}

type ApplicationStoreHealth struct {
	VacancyID int
	Reason    string
}

type AutoChatOutcome struct {
	AttemptID            string
	ConversationID       string
	TriggerMessageID     string
	ActionType           string
	State                string
	PersistenceUncertain bool
}

type AutoChatReconciliation struct {
	AttemptID        string
	ConversationID   string
	TriggerMessageID string
	ActionType       string
	Status           string
	EvidenceKind     string
}

type AutoChatStoreHealth struct {
	ConversationID string
	Reason         string
}

func (p *Projector) ProjectApplicationOutcome(ctx context.Context, event ApplicationOutcome) error {
	if !applicationUncertain(event.State) && !event.PersistenceUncertain {
		return nil
	}
	message := "Возможно отправлено; повторная отправка запрещена."
	if event.PersistenceUncertain {
		message = "Исход отправки неизвестен; локальное состояние могло остаться SENDING. Повторная отправка запрещена."
	} else if strings.EqualFold(strings.TrimSpace(event.State), "SENDING") {
		message = "Исход отправки неизвестен; повторная отправка запрещена."
	}
	return p.put(ctx, Record{
		Key:  "application/" + nonEmpty(event.AttemptID, "unknown") + "/delivery-uncertain",
		Kind: ApplicationDeliveryUncertain, Priority: PriorityHigh, Message: message,
		AttemptID: event.AttemptID, VacancyID: event.VacancyID,
		DetailPath: applicationDetailPath(event.AttemptID),
	})
}

func (p *Projector) ProjectApplicationReconciliation(ctx context.Context, event ApplicationReconciliation) error {
	base := Record{AttemptID: event.AttemptID, VacancyID: event.VacancyID, DetailPath: applicationDetailPath(event.AttemptID)}
	switch strings.ToUpper(strings.TrimSpace(event.Status)) {
	case "CONFIRMED", "CONFIRMED_RESPONSE_EXISTS":
		base.Key = "application/" + nonEmpty(event.AttemptID, "unknown") + "/delivery-uncertain"
		base.Kind, base.Priority = ApplicationResponseConfirmed, PriorityMedium
		base.Message = "Отклик подтверждён на HH."
		base.Resolve = true
	case "CONFLICTING", "CONFLICTING_EVIDENCE":
		base.Key = "application/" + nonEmpty(event.AttemptID, "unknown") + "/reconciliation-conflict"
		base.Kind, base.Priority = ApplicationReconciliationConflict, PriorityHigh
		base.Message = "Данные HH противоречат друг другу; требуется ручная проверка. Повторная автоматическая отправка заблокирована."
	case "PERSISTENCE_ERROR", "PERSISTENCE_UNCERTAIN":
		base.Key = "application/" + nonEmpty(event.AttemptID, "unknown") + "/persistence-uncertain"
		base.Kind, base.Priority = ApplicationReconciliationConflict, PriorityHigh
		base.Message = "Результат проверки HH не удалось надёжно сохранить; исход отправки неизвестен, повторная отправка запрещена."
	default:
		return nil
	}
	return p.put(ctx, base)
}

func (p *Projector) ProjectApplicationStoreUnavailable(ctx context.Context, event ApplicationStoreHealth) error {
	return p.put(ctx, Record{
		Key:  "application/store-unavailable/" + strconv.Itoa(event.VacancyID),
		Kind: ApplicationStoreUnavailable, Priority: PriorityHigh,
		Message:   "Автоматическая отправка остановлена: хранилище состояния недоступно.",
		VacancyID: event.VacancyID,
	})
}

func (p *Projector) ProjectAutoChatOutcome(ctx context.Context, event AutoChatOutcome) error {
	if !applicationUncertain(event.State) && !event.PersistenceUncertain {
		return nil
	}
	message := "Ответ мог быть отправлен. Повторный автоматический ответ на это сообщение заблокирован."
	if event.PersistenceUncertain {
		message = "Исход отправки неизвестен; локальное состояние могло остаться SENDING. Повторное автоматическое действие заблокировано."
	} else if strings.EqualFold(strings.TrimSpace(event.ActionType), "LEAVE") {
		message = "Исход выхода из диалога неизвестен; повторное автоматическое действие заблокировано."
	}
	return p.put(ctx, Record{
		Key:  "autochat/" + nonEmpty(event.AttemptID, "unknown") + "/delivery-uncertain",
		Kind: AutoChatDeliveryUncertain, Priority: PriorityHigh, Message: message,
		AttemptID: event.AttemptID, ConversationID: event.ConversationID,
		TriggerMessageID: event.TriggerMessageID, ActionType: event.ActionType,
		DetailPath: autoChatDetailPath(event.AttemptID),
	})
}

func (p *Projector) ProjectAutoChatReconciliation(ctx context.Context, event AutoChatReconciliation) error {
	base := Record{AttemptID: event.AttemptID, ConversationID: event.ConversationID, TriggerMessageID: event.TriggerMessageID, ActionType: event.ActionType, DetailPath: autoChatDetailPath(event.AttemptID)}
	incidentKey := "autochat/" + nonEmpty(event.AttemptID, "unknown") + "/delivery-uncertain"
	switch strings.ToUpper(strings.TrimSpace(event.Status)) {
	case "CONFIRMED":
		if strings.EqualFold(strings.TrimSpace(event.ActionType), "LEAVE") {
			// There is no strong positive leave evidence contract. A target
			// leave state is never turned into a misleading reply confirmation.
			return nil
		}
		base.Key = incidentKey
		base.Kind, base.Priority = AutoChatReplyConfirmed, PriorityMedium
		base.Resolve = true
		if strings.EqualFold(strings.TrimSpace(event.EvidenceKind), "EXACT_OUTGOING_MESSAGE_CONFIRMED") {
			base.Message = "Ответ в диалоге подтверждён на HH (точное исходящее сообщение найдено)."
		} else {
			base.Message = "Ответ после сообщения работодателя подтверждён на HH; причинность конкретной авто-попытки не установлена."
		}
	case "UNSUPPORTED":
		base.Key = incidentKey
		base.Kind, base.Priority = AutoChatLeaveReconciliationUnsupported, PriorityHigh
		base.Message = "Исход выхода из диалога неизвестен. HH не предоставляет достаточно сильных данных для безопасного подтверждения выхода из диалога."
	case "CONFLICTING", "PERSISTENCE_ERROR":
		base.Key = "autochat/" + nonEmpty(event.AttemptID, "unknown") + "/reconciliation-conflict"
		base.Kind, base.Priority = AutoChatReconciliationConflict, PriorityHigh
		base.Message = "Данные по ответу в диалоге противоречат друг другу или не сохранены; требуется ручная проверка. Повторный автоматический ответ заблокирован."
	default:
		return nil
	}
	return p.put(ctx, base)
}

func (p *Projector) ProjectAutoChatStoreUnavailable(ctx context.Context, event AutoChatStoreHealth) error {
	return p.put(ctx, Record{
		Key:  "autochat/store-unavailable/" + nonEmpty(event.ConversationID, "global"),
		Kind: AutoChatStoreUnavailable, Priority: PriorityHigh,
		Message:        "Автоматическая отправка остановлена: хранилище состояния недоступно.",
		ConversationID: event.ConversationID,
	})
}

func (p *Projector) put(ctx context.Context, record Record) error {
	if p == nil || p.store == nil {
		return errors.New("reliability notification store is unavailable")
	}
	if strings.TrimSpace(record.Key) == "" || record.Kind == "" || strings.TrimSpace(record.Message) == "" {
		return errors.New("invalid reliability notification")
	}
	return p.store.UpsertReliability(ctx, record)
}

func applicationUncertain(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "SENDING", "DELIVERY_UNCERTAIN":
		return true
	default:
		return false
	}
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func applicationDetailPath(attemptID string) string {
	if strings.TrimSpace(attemptID) == "" {
		return "/reliability"
	}
	return "/reliability/application-attempts/" + attemptID
}

func autoChatDetailPath(attemptID string) string {
	if strings.TrimSpace(attemptID) == "" {
		return "/reliability"
	}
	return fmt.Sprintf("/reliability/autochat-attempts/%s", attemptID)
}
