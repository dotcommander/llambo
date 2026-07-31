package gateway

import "testing"

func TestExtractPrompts_SingleSystemUser(t *testing.T) {
	t.Parallel()
	msgs := []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello, how are you?"},
	}

	system, user := ExtractPrompts(msgs)

	if system != "You are a helpful assistant." {
		t.Errorf("expected system 'You are a helpful assistant.', got %q", system)
	}
	if user != "Hello, how are you?" {
		t.Errorf("expected user 'Hello, how are you?', got %q", user)
	}
}

func TestExtractPrompts_MultipleUserMessages(t *testing.T) {
	t.Parallel()
	msgs := []Message{
		{Role: "system", Content: "Be concise."},
		{Role: "user", Content: "First question"},
		{Role: "user", Content: "Second question"},
		{Role: "user", Content: "Third question"},
	}

	system, user := ExtractPrompts(msgs)

	expectedSystem := "Be concise."
	expectedUser := "First question\nSecond question\nThird question"

	if system != expectedSystem {
		t.Errorf("expected system %q, got %q", expectedSystem, system)
	}
	if user != expectedUser {
		t.Errorf("expected user %q, got %q", expectedUser, user)
	}
}

func TestExtractPrompts_AssistantMessages(t *testing.T) {
	t.Parallel()
	msgs := []Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "4"},
		{Role: "user", Content: "And 3+3?"},
	}

	system, user := ExtractPrompts(msgs)

	expectedSystem := "You are helpful."
	expectedUser := "What is 2+2?\nAssistant: 4\nAnd 3+3?"

	if system != expectedSystem {
		t.Errorf("expected system %q, got %q", expectedSystem, system)
	}
	if user != expectedUser {
		t.Errorf("expected user %q, got %q", expectedUser, user)
	}
}

func TestExtractPrompts_EmptyInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msgs []Message
	}{
		{"nil slice", nil},
		{"empty slice", []Message{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			system, user := ExtractPrompts(tt.msgs)

			if system != "" {
				t.Errorf("expected empty system, got %q", system)
			}
			if user != "" {
				t.Errorf("expected empty user, got %q", user)
			}
		})
	}
}

func TestExtractUserContent_ExcludesSystem(t *testing.T) {
	t.Parallel()
	msgs := []Message{
		{Role: "system", Content: "This should be excluded"},
		{Role: "user", Content: "User message"},
		{Role: "assistant", Content: "Assistant response"},
		{Role: "user", Content: "Follow up"},
	}

	user := ExtractUserContent(msgs)

	expectedUser := "User message\nAssistant: Assistant response\nFollow up"

	if user != expectedUser {
		t.Errorf("expected %q, got %q", expectedUser, user)
	}
}

func TestExtractMessages_VariousInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		msgs          []Message
		includeSystem bool
		wantSystem    string
		wantUser      string
	}{
		{
			name:          "only system message with include",
			msgs:          []Message{{Role: "system", Content: "System prompt"}},
			includeSystem: true,
			wantSystem:    "System prompt",
			wantUser:      "",
		},
		{
			name:          "only system message without include",
			msgs:          []Message{{Role: "system", Content: "System prompt"}},
			includeSystem: false,
			wantSystem:    "",
			wantUser:      "",
		},
		{
			name:          "only user message",
			msgs:          []Message{{Role: "user", Content: "User message"}},
			includeSystem: true,
			wantSystem:    "",
			wantUser:      "User message",
		},
		{
			name:          "only assistant message",
			msgs:          []Message{{Role: "assistant", Content: "Response"}},
			includeSystem: true,
			wantSystem:    "",
			wantUser:      "Assistant: Response",
		},
		{
			name: "multiple system messages",
			msgs: []Message{
				{Role: "system", Content: "Rule 1"},
				{Role: "system", Content: "Rule 2"},
				{Role: "user", Content: "Question"},
			},
			includeSystem: true,
			wantSystem:    "Rule 1\nRule 2",
			wantUser:      "Question",
		},
		{
			name: "conversation history",
			msgs: []Message{
				{Role: "system", Content: "Be helpful"},
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there!"},
				{Role: "user", Content: "How are you?"},
				{Role: "assistant", Content: "I'm doing well."},
				{Role: "user", Content: "Great!"},
			},
			includeSystem: true,
			wantSystem:    "Be helpful",
			wantUser:      "Hello\nAssistant: Hi there!\nHow are you?\nAssistant: I'm doing well.\nGreat!",
		},
		{
			name: "unknown role ignored",
			msgs: []Message{
				{Role: "system", Content: "System"},
				{Role: "function", Content: "Should be ignored"},
				{Role: "user", Content: "User"},
			},
			includeSystem: true,
			wantSystem:    "System",
			wantUser:      "User",
		},
		{
			name: "empty content messages",
			msgs: []Message{
				{Role: "system", Content: ""},
				{Role: "user", Content: ""},
				{Role: "assistant", Content: ""},
			},
			includeSystem: true,
			wantSystem:    "",
			wantUser:      "\nAssistant: ",
		},
		{
			name: "multiline content",
			msgs: []Message{
				{Role: "system", Content: "Line 1\nLine 2"},
				{Role: "user", Content: "Question\nWith multiple lines"},
			},
			includeSystem: true,
			wantSystem:    "Line 1\nLine 2",
			wantUser:      "Question\nWith multiple lines",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotSystem, gotUser := ExtractMessages(tt.msgs, tt.includeSystem)

			if gotSystem != tt.wantSystem {
				t.Errorf("system: got %q, want %q", gotSystem, tt.wantSystem)
			}
			if gotUser != tt.wantUser {
				t.Errorf("user: got %q, want %q", gotUser, tt.wantUser)
			}
		})
	}
}

func TestExtractPrompts_PreservesOrder(t *testing.T) {
	t.Parallel()
	// Verify that message order is preserved in output
	msgs := []Message{
		{Role: "user", Content: "1"},
		{Role: "assistant", Content: "2"},
		{Role: "user", Content: "3"},
		{Role: "assistant", Content: "4"},
		{Role: "user", Content: "5"},
	}

	_, user := ExtractPrompts(msgs)

	expected := "1\nAssistant: 2\n3\nAssistant: 4\n5"
	if user != expected {
		t.Errorf("order not preserved: got %q, want %q", user, expected)
	}
}

func TestExtractMessages_ConvenienceWrappers(t *testing.T) {
	t.Parallel()
	msgs := []Message{
		{Role: "system", Content: "System"},
		{Role: "user", Content: "User"},
	}

	// Test that ExtractPrompts is equivalent to ExtractMessages(msgs, true)
	system1, user1 := ExtractPrompts(msgs)
	system2, user2 := ExtractMessages(msgs, true)

	if system1 != system2 || user1 != user2 {
		t.Error("ExtractPrompts should be equivalent to ExtractMessages with includeSystem=true")
	}

	// Test that ExtractUserContent is equivalent to ExtractMessages(msgs, false)
	userOnly := ExtractUserContent(msgs)
	_, user3 := ExtractMessages(msgs, false)

	if userOnly != user3 {
		t.Error("ExtractUserContent should be equivalent to user part of ExtractMessages with includeSystem=false")
	}
}
