package gateway

import "strings"

// extractMessagesCore is the shared implementation for message extraction.
// prefixRoles controls whether user messages get a "User: " prefix.
func extractMessagesCore(msgs []Message, includeSystem, prefixRoles bool) (system, user string) {
	var systemParts []string
	var userParts []string
	for _, msg := range msgs {
		switch msg.Role {
		case "system":
			if includeSystem {
				systemParts = append(systemParts, msg.Content)
			}
		case "user":
			if prefixRoles {
				userParts = append(userParts, "User: "+msg.Content)
			} else {
				userParts = append(userParts, msg.Content)
			}
		case "assistant":
			userParts = append(userParts, "Assistant: "+msg.Content)
		}
	}
	system = strings.Join(systemParts, "\n")
	user = strings.Join(userParts, "\n")
	return
}

// ExtractMessages extracts system and user content from a message array.
// If includeSystem is true, system messages are included in the system string.
// User and assistant messages are combined into the user content string.
func ExtractMessages(msgs []Message, includeSystem bool) (system, user string) {
	return extractMessagesCore(msgs, includeSystem, false)
}

// ExtractUserContent extracts only user and assistant messages (no system).
// This is a convenience wrapper for ExtractMessages(msgs, false).
func ExtractUserContent(msgs []Message) string {
	_, user := ExtractMessages(msgs, false)
	return user
}

// ExtractPrompts extracts both system and user content from messages.
// This is a convenience wrapper for ExtractMessages(msgs, true).
func ExtractPrompts(msgs []Message) (system, user string) {
	return ExtractMessages(msgs, true)
}
