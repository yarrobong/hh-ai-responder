package careeragent

import "sort"

// BuildAttentionQueue derives one deterministic operator queue from read-model
// inputs. It intentionally does not persist lifecycle or authorization state.
func BuildAttentionQueue(items []AttentionItem) []AttentionItem {
	byID := make(map[string]AttentionItem, len(items))
	for _, item := range items {
		if item.ID == "" {
			continue
		}
		if previous, ok := byID[item.ID]; !ok || attentionItemPrecedes(item, previous) {
			byID[item.ID] = item
		}
	}
	result := make([]AttentionItem, 0, len(byID))
	for _, item := range byID {
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority < result[j].Priority
		}
		if !result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].UpdatedAt.Before(result[j].UpdatedAt)
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func attentionItemPrecedes(a, b AttentionItem) bool {
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	return a.UpdatedAt.Before(b.UpdatedAt)
}
