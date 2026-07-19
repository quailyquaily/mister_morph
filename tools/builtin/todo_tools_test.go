package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	cronstore "github.com/quailyquaily/mistermorph/internal/cron"
	"github.com/quailyquaily/mistermorph/internal/todo"
	"github.com/quailyquaily/mistermorph/llm"
)

type stubTodoToolLLMClient struct {
	replies []string
	err     error
	calls   []llm.Request
}

func (s *stubTodoToolLLMClient) Chat(_ context.Context, req llm.Request) (llm.Result, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return llm.Result{}, s.err
	}
	if len(s.replies) == 0 {
		return llm.Result{}, fmt.Errorf("no more stub replies")
	}
	reply := s.replies[0]
	s.replies = s.replies[1:]
	return llm.Result{Text: reply}, nil
}

func TestTodoUpdateAddOnceAndDeleteSemantic(t *testing.T) {
	root := t.TempDir()
	cronPath := filepath.Join(root, "cron.yaml")
	contactsDir := filepath.Join(root, "contacts")
	seedTodoContacts(t, contactsDir)

	client := &stubTodoToolLLMClient{
		replies: []string{
			`{"status":"ok","rewritten_content":"提醒 [John](tg:1001) 提交评估报告"}`,
			`{"status":"matched","index":0}`,
		},
	}
	update := NewTodoUpdateToolWithLLM(true, cronPath, contactsDir, client, "gpt-5.2")
	out, err := update.Execute(context.Background(), map[string]any{
		"action":  "add_once",
		"id":      "submit-report",
		"title":   "Submit report",
		"content": "提醒 John 提交评估报告",
		"people":  []any{"John"},
		"at":      "2026-05-12 09:00",
		"tz":      "Asia/Tokyo",
		"chat_id": "tg:-1001981343441",
	})
	if err != nil {
		t.Fatalf("todo_update add_once error = %v", err)
	}
	var addParsed struct {
		OK        bool `json:"ok"`
		TaskCount int  `json:"task_count"`
		Task      struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			At      string `json:"at"`
			TZ      string `json:"tz"`
			ChatID  string `json:"chat_id"`
			Content string `json:"content"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &addParsed); err != nil {
		t.Fatalf("todo_update add_once json parse error = %v", err)
	}
	if !addParsed.OK || addParsed.TaskCount != 1 || addParsed.Task.ID != "submit-report" {
		t.Fatalf("unexpected add_once result: %s", out)
	}
	if addParsed.Task.Title != "Submit report" {
		t.Fatalf("task title = %q, want Submit report", addParsed.Task.Title)
	}
	if addParsed.Task.ChatID != "tg:-1001981343441" || !strings.Contains(addParsed.Task.Content, "[John](tg:1001)") {
		t.Fatalf("unexpected added task: %#v", addParsed.Task)
	}

	_, err = update.Execute(context.Background(), map[string]any{
		"action":  "delete",
		"content": "提交评估报告",
	})
	if err != nil {
		t.Fatalf("todo_update delete error = %v", err)
	}
	if len(client.calls) != 2 || !client.calls[0].ForceJSON || !client.calls[1].ForceJSON {
		t.Fatalf("expected two ForceJSON llm calls")
	}
	file, _, err := cronstore.NewStore(cronPath).Read()
	if err != nil {
		t.Fatalf("read cron file: %v", err)
	}
	if len(file.Tasks) != 0 {
		t.Fatalf("expected delete to remove task, got %#v", file.Tasks)
	}
}

func TestTodoUpdateAddRecurringWritesCronYAML(t *testing.T) {
	root := t.TempDir()
	cronPath := filepath.Join(root, "cron.yaml")
	contactsDir := filepath.Join(root, "contacts")
	client := &stubTodoToolLLMClient{}
	update := NewTodoUpdateToolWithLLM(true, cronPath, contactsDir, client, "gpt-5.2")
	out, err := update.Execute(context.Background(), map[string]any{
		"action":  "add_recurring",
		"id":      "tennis",
		"title":   "Tennis practice",
		"content": "去打网球。",
		"cron":    "0 15 * * 4",
		"tz":      "Asia/Tokyo",
	})
	if err != nil {
		t.Fatalf("todo_update add_recurring error = %v", err)
	}
	var parsed struct {
		OK        bool `json:"ok"`
		TaskCount int  `json:"task_count"`
		Task      struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Cron    string `json:"cron"`
			TZ      string `json:"tz"`
			Content string `json:"content"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("todo_update add_recurring json parse error = %v", err)
	}
	if !parsed.OK || parsed.TaskCount != 1 || parsed.Task.ID != "tennis" || parsed.Task.Cron != "0 15 * * 4" {
		t.Fatalf("unexpected add_recurring result: %s", out)
	}
	if parsed.Task.Title != "Tennis practice" {
		t.Fatalf("task title = %q, want Tennis practice", parsed.Task.Title)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no llm calls for recurring task without people, got %d", len(client.calls))
	}
	file, _, err := cronstore.NewStore(cronPath).Read()
	if err != nil {
		t.Fatalf("read cron file: %v", err)
	}
	if len(file.Tasks) != 1 || file.Tasks[0].Title != "Tennis practice" || file.Tasks[0].Content != "去打网球。" {
		t.Fatalf("unexpected cron file: %#v", file.Tasks)
	}
}

func TestTodoUpdateAddDefaultsConsoleNotificationChatID(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]any
		wantChatID string
	}{
		{
			name: "once",
			params: map[string]any{
				"action":  "add_once",
				"content": "提醒我喝水。",
				"at":      "2026-07-20 09:00",
				"tz":      "Asia/Tokyo",
			},
			wantChatID: cronstore.ConsoleNotificationChatID,
		},
		{
			name: "recurring",
			params: map[string]any{
				"action":  "add_recurring",
				"content": "提醒我喝水。",
				"cron":    "0 9 * * *",
				"tz":      "Asia/Tokyo",
			},
			wantChatID: cronstore.ConsoleNotificationChatID,
		},
		{
			name: "explicit target",
			params: map[string]any{
				"action":  "add_once",
				"content": "提醒我喝水。",
				"at":      "2026-07-20 09:00",
				"tz":      "Asia/Tokyo",
				"chat_id": "tg:-1001981343441",
			},
			wantChatID: "tg:-1001981343441",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			update := NewTodoUpdateToolWithLLM(
				true,
				filepath.Join(root, "cron.yaml"),
				filepath.Join(root, "contacts"),
				&stubTodoToolLLMClient{},
				"gpt-5.2",
			)
			update.SetAddContext(todo.AddResolveContext{Channel: "console"})

			out, err := update.Execute(context.Background(), tt.params)
			if err != nil {
				t.Fatalf("todo_update %s error = %v", tt.name, err)
			}
			var parsed struct {
				Task struct {
					ChatID string `json:"chat_id"`
				} `json:"task"`
			}
			if err := json.Unmarshal([]byte(out), &parsed); err != nil {
				t.Fatalf("todo_update %s json parse error = %v", tt.name, err)
			}
			if parsed.Task.ChatID != tt.wantChatID {
				t.Fatalf("task chat_id = %q, want %q", parsed.Task.ChatID, tt.wantChatID)
			}
		})
	}
}

func TestTodoUpdateDeleteByIDDoesNotRequireLLM(t *testing.T) {
	root := t.TempDir()
	cronPath := filepath.Join(root, "cron.yaml")
	contactsDir := filepath.Join(root, "contacts")
	store := cronstore.NewStore(cronPath)
	if _, err := store.AddOnceWithChatID("", "Review invoices.", "2026-05-12 09:00", "UTC", "invoice-review", ""); err != nil {
		t.Fatalf("seed cron task: %v", err)
	}
	update := NewTodoUpdateTool(true, cronPath, contactsDir)
	_, err := update.Execute(context.Background(), map[string]any{
		"action": "delete",
		"id":     "invoice-review",
	})
	if err != nil {
		t.Fatalf("delete by id should not require llm: %v", err)
	}
	file, _, err := store.Read()
	if err != nil {
		t.Fatalf("read cron file: %v", err)
	}
	if len(file.Tasks) != 0 {
		t.Fatalf("expected no tasks after delete, got %#v", file.Tasks)
	}
}

func TestTodoUpdateCompleteActionRemoved(t *testing.T) {
	root := t.TempDir()
	update := NewTodoUpdateToolWithLLM(true, filepath.Join(root, "cron.yaml"), filepath.Join(root, "contacts"), &stubTodoToolLLMClient{}, "gpt-5.2")
	_, err := update.Execute(context.Background(), map[string]any{
		"action":  "complete",
		"content": "旧任务",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid action") {
		t.Fatalf("expected invalid action for complete, got %v", err)
	}
}

func TestTodoUpdateAddRejectsInvalidReferenceBeforeLLM(t *testing.T) {
	root := t.TempDir()
	client := &stubTodoToolLLMClient{}
	update := NewTodoUpdateToolWithLLM(true, filepath.Join(root, "cron.yaml"), filepath.Join(root, "contacts"), client, "gpt-5.2")
	_, err := update.Execute(context.Background(), map[string]any{
		"action":  "add_once",
		"content": "提醒 [John](not-a-reference) 明天确认内容",
		"people":  []any{"John"},
		"at":      "2026-05-12 09:00",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid reference id") {
		t.Fatalf("expected invalid reference id error, got %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no llm calls for invalid reference input")
	}
}

func TestTodoUpdateAddRecurringResolvesConsoleSpeakerPlaceholder(t *testing.T) {
	root := t.TempDir()
	contactsDir := filepath.Join(root, "contacts")
	seedTodoContacts(t, contactsDir)
	client := &stubTodoToolLLMClient{}
	update := NewTodoUpdateToolWithLLM(true, filepath.Join(root, "cron.yaml"), contactsDir, client, "gpt-5.2")
	update.SetAddContext(todo.AddResolveContext{
		Channel:         "console",
		ChatType:        "topic",
		SpeakerUsername: "console:user",
		UserInputRaw:    "每周四东京时间下午 3 点，提醒我去打网球。",
	})
	out, err := update.Execute(context.Background(), map[string]any{
		"action":  "add_recurring",
		"content": "提醒我去打网球。",
		"people":  []any{"$SPEAKER"},
		"cron":    "0 15 * * 4",
		"tz":      "Asia/Tokyo",
	})
	if err != nil {
		t.Fatalf("todo_update add_recurring error = %v", err)
	}
	var parsed struct {
		OK   bool `json:"ok"`
		Task struct {
			Content string `json:"content"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("todo_update add_recurring json parse error = %v", err)
	}
	if !parsed.OK || parsed.Task.Content != "提醒[我](console:user)去打网球。" {
		t.Fatalf("unexpected add_recurring result: %s", out)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no llm calls for builtin speaker placeholder, got %d", len(client.calls))
	}
}

func TestTodoUpdateAddOnceIgnoresUnusedBuiltinSpeakerPeople(t *testing.T) {
	root := t.TempDir()
	contactsDir := filepath.Join(root, "contacts")
	client := &stubTodoToolLLMClient{}
	update := NewTodoUpdateToolWithLLM(true, filepath.Join(root, "cron.yaml"), contactsDir, client, "gpt-5.2")
	update.SetAddContext(todo.AddResolveContext{
		Channel:         "console",
		ChatType:        "topic",
		SpeakerUsername: "console:user",
		UserInputRaw:    "明天下午五点提醒我会议",
	})
	content := "提醒：30分钟后有会议 (17:30-18:30)。链接：https://meet.google.com/skz-xyca-foi"
	out, err := update.Execute(context.Background(), map[string]any{
		"action":  "add_once",
		"content": content,
		"people":  []any{"$SPEAKER"},
		"at":      "2026-06-17 17:00",
		"title":   "会议提醒",
		"tz":      "Asia/Tokyo",
	})
	if err != nil {
		t.Fatalf("todo_update add_once error = %v", err)
	}
	var parsed struct {
		OK   bool `json:"ok"`
		Task struct {
			Content string `json:"content"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("todo_update add_once json parse error = %v", err)
	}
	if !parsed.OK || parsed.Task.Content != content {
		t.Fatalf("unexpected add_once result: %s", out)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no llm calls for unused builtin speaker people, got %d", len(client.calls))
	}
}

func seedTodoContacts(t *testing.T, contactsDir string) {
	t.Helper()
	svc := contacts.NewService(contacts.NewFileStore(contactsDir))
	now := time.Date(2026, 2, 9, 12, 0, 0, 0, time.UTC)
	_, err := svc.UpsertContact(context.Background(), contacts.Contact{
		ContactID:       "tg:1001",
		ContactNickname: "John",
		Kind:            contacts.KindHuman,
		Channel:         contacts.ChannelTelegram,
		TGPrivateChatID: 1001,
	}, now)
	if err != nil {
		t.Fatalf("seed john contact error = %v", err)
	}
	_, err = svc.UpsertContact(context.Background(), contacts.Contact{
		ContactID:        "slack:T001:D002",
		ContactNickname:  "Momo",
		Kind:             contacts.KindHuman,
		Channel:          contacts.ChannelSlack,
		SlackTeamID:      "T001",
		SlackDMChannelID: "D002",
	}, now)
	if err != nil {
		t.Fatalf("seed momo contact error = %v", err)
	}
}
