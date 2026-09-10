package runtime

import (
	"context"
	"errors"

	"hh-ai-responder/internal/usecase/hhwritepreflight"
)

func hhTestMetadata(value VacancyTest) hhwritepreflight.TestMetadata {
	result := hhwritepreflight.TestMetadata{UIDPK: value.UIDPk, GUID: value.GUID, StartTime: value.StartTime, Required: value.Required, Tasks: make([]hhwritepreflight.TestTask, 0, len(value.Tasks))}
	for _, task := range value.Tasks {
		item := hhwritepreflight.TestTask{ID: task.ID, Open: task.Open, ChoiceIDs: make([]string, 0, len(task.CandidateSolutions))}
		for _, choice := range task.CandidateSolutions {
			item.ChoiceIDs = append(item.ChoiceIDs, choice.ID)
		}
		result.Tasks = append(result.Tasks, item)
	}
	return result
}

// hhWritePreflightChatReader translates the root read-only capability into
// the neutral capability consumed by hhwritepreflight. The fallback preserves
// the legacy non-required-read test mode; controlled production composition
// sets RequireFreshRead and therefore fails closed when the targeted reader is
// unavailable.
type hhWritePreflightChatReader struct {
	reader          HHConversationPreflightReader
	conversation    EmployerConversation
	allowLocalState bool
}

func (r hhWritePreflightChatReader) ReadChatState(ctx context.Context, externalID string) (hhwritepreflight.ChatState, error) {
	if r.reader != nil {
		state, err := r.reader.ReadConversationState(ctx, externalID)
		if err != nil {
			return hhwritepreflight.ChatState{}, err
		}
		return hhwritepreflight.ChatState{
			ExternalID: state.ExternalID, LastMessageID: state.LastMessageID,
			State: state.State, ReplyRequirement: string(state.ReplyRequirement),
			MessageCount: state.MessageCount, Warnings: append([]string(nil), state.Warnings...),
			MessageIDs: append([]string(nil), state.MessageIDs...),
		}, nil
	}
	if !r.allowLocalState {
		return hhwritepreflight.ChatState{}, errors.New("fresh HH conversation read is unavailable")
	}
	return hhwritepreflight.ChatState{
		ExternalID: externalID, LastMessageID: latestDeliveredMessageID(r.conversation),
		State: string(r.conversation.Status), MessageCount: len(deliveredMessages(r.conversation.Messages)),
	}, nil
}

func (g *HHWriteGateway) freshChatPreflightService(c EmployerConversation) *hhwritepreflight.Service {
	var reader HHConversationPreflightReader
	if candidate, ok := g.ReadClient.(HHConversationPreflightReader); ok {
		reader = candidate
	}
	return hhwritepreflight.NewService(hhwritepreflight.Dependencies{
		Chats: hhWritePreflightChatReader{reader: reader, conversation: c, allowLocalState: !g.RequireFreshRead},
	})
}
