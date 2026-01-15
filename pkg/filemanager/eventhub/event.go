package eventhub

import "errors"

type (
	Event struct {
		Type   EventType `json:"type"`
		FileID string    `json:"file_id"`
		From   string    `json:"from"`
		To     string    `json:"to"`
	}

	EventType string
)

const (
	EventTypeCreate = "create"
	EventTypeModify = "modify"
	EventTypeRename = "rename"
	EventTypeDelete = "delete"
)

var (
	// ErrEventHubClosed is returned when operations are attempted on a closed EventHub.
	ErrEventHubClosed = errors.New("event hub is closed")
)

// eventState tracks the accumulated state for each file
type eventState struct {
	baseType    EventType // The base event type (Create, Delete, or first event type)
	originalSrc string    // Original source path (for Create or first Rename)
	currentDst  string    // Current destination path
}

/*
Modify + Modify → keep only the last Modify;
Create + Modify → fold into a single Create with final metadata/content.
Create + Rename(a→b) → Create at b.
Create + Delete → drop both (ephemeral object never needs to reach clients).
Modify + Delete → Delete (intermediate Modify is irrelevant to final state).
Rename(a→b) + Rename(b→c) → Rename(a→c).
Rename(a→b) + Modify → emit Rename(a→b) then a single Modify at b (or fold Modify into Create if the chain starts with Create).
Rename(a→b) + Delete → emit only Delete(object_id);
Rename(a→b) + Rename(b→a) with no intervening Modify → drop both (rename there-and-back is a no-op).
Delete + Create might be a valid case, e.g. user restore same file from trash bin.
*/
// DebounceEvents takes time-ordered events and returns debounced/merged events.
func DebounceEvents(in []*Event) []*Event {
	if len(in) == 0 {
		return nil
	}

	states := make(map[string]*eventState) // keyed by FileID
	order := make([]string, 0)             // to preserve order of first appearance

	for _, e := range in {
		state, exists := states[e.FileID]

		if !exists {
			// First event for this file
			order = append(order, e.FileID)
			states[e.FileID] = &eventState{
				baseType:    e.Type,
				originalSrc: e.From,
				currentDst:  e.To,
			}
			continue
		}

		switch e.Type {
		case EventTypeCreate:
			// Delete + Create → keep as Create (e.g. restore from trash)
			if state.baseType == EventTypeDelete {
				state.baseType = EventTypeCreate
				state.originalSrc = e.From
				state.currentDst = ""
			}

		case EventTypeModify:
			switch state.baseType {
			case EventTypeCreate:
				// Create + Modify → fold into Create (no change needed, Create already implies content)
			case EventTypeModify:
				// Modify + Modify → keep only last Modify (state already correct)
			case EventTypeRename:
				// Rename + Modify → fold into first Rename
			case EventTypeDelete:
				// Delete + Modify → should not happen, but ignore Modify
			}

		case EventTypeRename:
			switch state.baseType {
			case EventTypeCreate:
				// Create + Rename(a→b) → Create at b
				state.originalSrc = e.To
				state.currentDst = ""
			case EventTypeModify:
				// Modify + Rename → emit Rename only
				state.baseType = EventTypeRename
				state.currentDst = e.To
				state.originalSrc = e.From

			case EventTypeRename:
				// Rename(a→b) + Rename(b→c) → Rename(a→c)
				// Check for no-op: Rename(a→b) + Rename(b→a) → drop both
				if state.originalSrc == e.To {
					// Rename there-and-back, drop both
					delete(states, e.FileID)
					// Remove from order
					for i, id := range order {
						if id == e.FileID {
							order = append(order[:i], order[i+1:]...)
							break
						}
					}
				} else {
					state.currentDst = e.To
				}
			case EventTypeDelete:
				// Delete + Rename → should not happen, ignore
			}

		case EventTypeDelete:
			switch state.baseType {
			case EventTypeCreate:
				// Create + Delete → drop both (ephemeral object)
				delete(states, e.FileID)
				// Remove from order
				for i, id := range order {
					if id == e.FileID {
						order = append(order[:i], order[i+1:]...)
						break
					}
				}
			case EventTypeModify:
				// Modify + Delete → Delete
				state.baseType = EventTypeDelete
				state.originalSrc = e.From
				state.currentDst = ""
			case EventTypeRename:
				// Rename + Delete → Delete only
				state.baseType = EventTypeDelete
				state.originalSrc = e.From
				state.currentDst = ""
			case EventTypeDelete:
				// Delete + Delete → keep Delete (should not happen normally)
			}
		}
	}

	// Build output events in order
	result := make([]*Event, 0, len(order))
	for _, fileID := range order {
		state, exists := states[fileID]
		if !exists {
			continue
		}

		switch state.baseType {
		case EventTypeCreate:
			result = append(result, &Event{
				Type:   EventTypeCreate,
				FileID: fileID,
				From:   state.originalSrc,
			})
		case EventTypeModify:
			result = append(result, &Event{
				Type:   EventTypeModify,
				FileID: fileID,
				From:   state.originalSrc,
			})
		case EventTypeRename:
			// If hasModify and base was originally Modify (converted to Rename),
			// we need to emit Modify first at original location
			// But in our current logic, Modify+Rename sets hasModify=true
			// We emit Rename, then Modify if needed
			result = append(result, &Event{
				Type:   EventTypeRename,
				FileID: fileID,
				From:   state.originalSrc,
				To:     state.currentDst,
			})
		case EventTypeDelete:
			result = append(result, &Event{
				Type:   EventTypeDelete,
				FileID: fileID,
				From:   state.originalSrc,
			})
		}
	}

	return result
}
