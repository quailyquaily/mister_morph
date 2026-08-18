package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/quailyquaily/mistermorph/internal/testhttp"
)

func TestSendMessageHTMLReplyUsesHTMLParseMode(t *testing.T) {
	var calls []telegramSendMessageRequest
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendMessage" {
			http.NotFound(w, r)
			return
		}
		var req telegramSendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		calls = append(calls, req)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	api := newTelegramAPI(srv.Client, srv.URL, "token")
	_, err := api.sendMessageHTMLReplyInThreadWithMessageID(context.Background(), 42, 0, "*hello*", true, 99)
	if err != nil {
		t.Fatalf("sendMessageHTMLReplyInThreadWithMessageID() error = %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(calls))
	}
	if calls[0].ParseMode != "HTML" {
		t.Fatalf("parse_mode = %q, want HTML", calls[0].ParseMode)
	}
	if calls[0].ReplyToMessageID != 99 {
		t.Fatalf("reply_to_message_id = %d, want 99", calls[0].ReplyToMessageID)
	}
}

func TestSendMessageHTMLReplyInThreadWithReplyMarkup(t *testing.T) {
	var call telegramSendMessageRequest
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))

	api := newTelegramAPI(srv.Client, srv.URL, "token")
	messageID, err := api.sendMessageHTMLReplyInThreadWithMessageIDAndMarkup(
		context.Background(),
		123,
		456,
		"Approve?",
		true,
		789,
		telegramApprovalReplyMarkup("apr_123"),
	)
	if err != nil {
		t.Fatalf("send message with markup: %v", err)
	}
	if messageID != 42 {
		t.Fatalf("message id = %d, want 42", messageID)
	}
	if call.ChatID != 123 || call.MessageThreadID != 456 || call.ReplyToMessageID != 789 {
		t.Fatalf("call target = %+v", call)
	}
	if call.ReplyMarkup == nil || len(call.ReplyMarkup.InlineKeyboard) != 1 || len(call.ReplyMarkup.InlineKeyboard[0]) != 2 {
		t.Fatalf("reply markup = %#v, want one row with two buttons", call.ReplyMarkup)
	}
	if got := call.ReplyMarkup.InlineKeyboard[0][0].CallbackData; got != "ap:a:apr_123" {
		t.Fatalf("approve callback data = %q", got)
	}
	if got := call.ReplyMarkup.InlineKeyboard[0][1].CallbackData; got != "ap:d:apr_123" {
		t.Fatalf("deny callback data = %q", got)
	}
}

func TestSendMessageHTMLReplyInThreadIncludesMessageThreadID(t *testing.T) {
	var calls []telegramSendMessageRequest
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendMessage" {
			http.NotFound(w, r)
			return
		}
		var req telegramSendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		calls = append(calls, req)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":12345}}`))
	}))

	api := newTelegramAPI(srv.Client, srv.URL, "token")
	_, err := api.sendMessageHTMLReplyInThreadWithMessageID(context.Background(), 42, 901, "hello", true, 99)
	if err != nil {
		t.Fatalf("sendMessageHTMLReplyInThreadWithMessageID() error = %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(calls))
	}
	if calls[0].MessageThreadID != 901 {
		t.Fatalf("message_thread_id = %d, want 901", calls[0].MessageThreadID)
	}
	if calls[0].ReplyToMessageID != 99 {
		t.Fatalf("reply_to_message_id = %d, want 99", calls[0].ReplyToMessageID)
	}
}

func TestSendMessageHTMLReplyFallbackToPlainOnParseError(t *testing.T) {
	var calls []telegramSendMessageRequest
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendMessage" {
			http.NotFound(w, r)
			return
		}
		var req telegramSendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		calls = append(calls, req)
		switch req.ParseMode {
		case "HTML":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: can't parse entities"}`))
		case "":
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"description":"unexpected parse mode"}`))
		}
	}))

	api := newTelegramAPI(srv.Client, srv.URL, "token")
	_, err := api.sendMessageHTMLReplyInThreadWithMessageID(context.Background(), 42, 0, "*bad*", true, 77)
	if err != nil {
		t.Fatalf("sendMessageHTMLReplyInThreadWithMessageID() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(calls))
	}
	if calls[0].ParseMode != "HTML" || calls[1].ParseMode != "" {
		t.Fatalf("unexpected parse mode sequence: %#v", []string{calls[0].ParseMode, calls[1].ParseMode})
	}
	if calls[0].ReplyToMessageID != 77 || calls[1].ReplyToMessageID != 77 {
		t.Fatalf("reply_to_message_id sequence = %#v, want both 77", []int64{calls[0].ReplyToMessageID, calls[1].ReplyToMessageID})
	}
}

func TestSendMessageHTMLReplyWithMessageID(t *testing.T) {
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/sendMessage" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":12345}}`))
	}))

	api := newTelegramAPI(srv.Client, srv.URL, "token")
	messageID, err := api.sendMessageHTMLReplyInThreadWithMessageID(context.Background(), 42, 0, "hello", true, 99)
	if err != nil {
		t.Fatalf("sendMessageHTMLReplyInThreadWithMessageID() error = %v", err)
	}
	if messageID != 12345 {
		t.Fatalf("message_id = %d, want 12345", messageID)
	}
}

func TestEditMessageHTMLUsesEditEndpointAndParseMode(t *testing.T) {
	var calls []telegramEditMessageTextRequest
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/editMessageText" {
			http.NotFound(w, r)
			return
		}
		var req telegramEditMessageTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		calls = append(calls, req)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	api := newTelegramAPI(srv.Client, srv.URL, "token")
	err := api.editMessageHTML(context.Background(), 42, 77, "*hello*", true)
	if err != nil {
		t.Fatalf("editMessageHTML() error = %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(calls))
	}
	if calls[0].ParseMode != "HTML" {
		t.Fatalf("parse_mode = %q, want HTML", calls[0].ParseMode)
	}
	if calls[0].MessageID != 77 {
		t.Fatalf("message_id = %d, want 77", calls[0].MessageID)
	}
}

func TestEditMessageHTMLFallbackToPlainOnParseError(t *testing.T) {
	var calls []telegramEditMessageTextRequest
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/editMessageText" {
			http.NotFound(w, r)
			return
		}
		var req telegramEditMessageTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		calls = append(calls, req)
		switch req.ParseMode {
		case "HTML":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: can't parse entities"}`))
		case "":
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"description":"unexpected parse mode"}`))
		}
	}))

	api := newTelegramAPI(srv.Client, srv.URL, "token")
	err := api.editMessageHTML(context.Background(), 42, 88, "*bad*", true)
	if err != nil {
		t.Fatalf("editMessageHTML() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(calls))
	}
	if calls[0].ParseMode != "HTML" || calls[1].ParseMode != "" {
		t.Fatalf("unexpected parse mode sequence: %#v", []string{calls[0].ParseMode, calls[1].ParseMode})
	}
	if calls[0].MessageID != 88 || calls[1].MessageID != 88 {
		t.Fatalf("message_id sequence = %#v, want both 88", []int64{calls[0].MessageID, calls[1].MessageID})
	}
}
