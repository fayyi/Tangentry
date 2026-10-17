package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const chatResultKind = "chat"

func chatKey(contentID string) string {
	return hashSource(struct{ Kind, ContentID string }{chatResultKind, contentID})
}

func (s *Service) ChatHistory(ctx context.Context, contentID string) (ChatResult, error) {
	if !contentIDValid(contentID) {
		return ChatResult{}, ErrInvalid
	}
	row, err := s.results.AIResult(ctx, chatResultKind, chatKey(contentID), "")
	if errors.Is(err, ErrNotFound) {
		return ChatResult{ContentID: contentID, Messages: []ChatMessage{}}, nil
	}
	if err != nil {
		return ChatResult{}, ErrStorage
	}
	var messages []ChatMessage
	if len(row.Value) > 512<<10 || json.Unmarshal([]byte(row.Value), &messages) != nil || !utf8ValidChat("", messages) {
		return ChatResult{}, ErrStorage
	}
	return ChatResult{ContentID: contentID, Messages: messages, UpdatedAt: row.CreatedAt}, nil
}

func (s *Service) Chat(ctx context.Context, in ChatRequest) (ChatResult, error) {
	if !contentIDValid(in.ContentID) || !utf8ValidChat(in.Context, in.Messages) || len(in.Messages) > 40 {
		return ChatResult{}, ErrInvalid
	}
	s.mu.Lock()
	c, provider := s.config, s.provider
	s.mu.Unlock()
	if in.Revision != c.Revision {
		return ChatResult{}, ErrConflict
	}
	chatter, ok := provider.(Chatter)
	if !ok || !public(c).Ready {
		return ChatResult{}, ErrNotReady
	}
	key := chatKey(in.ContentID)
	if in.Clear {
		if err := s.results.DeleteAIResult(ctx, chatResultKind, key, ""); err != nil {
			return ChatResult{}, ErrStorage
		}
		return ChatResult{ContentID: in.ContentID, Messages: []ChatMessage{}}, nil
	}
	var history []ChatMessage
	if row, err := s.results.AIResult(ctx, chatResultKind, key, ""); err == nil {
		_ = json.Unmarshal([]byte(row.Value), &history)
	} else if !errors.Is(err, ErrNotFound) {
		return ChatResult{}, ErrStorage
	}
	if len(in.Messages) > 0 {
		history = append([]ChatMessage(nil), in.Messages...)
	}
	if len(history) == 0 || history[len(history)-1].Role != "user" {
		return ChatResult{}, ErrInvalid
	}
	answer, err := chatter.Chat(ctx, c, in.Context, history)
	if err != nil {
		return ChatResult{}, err
	}
	history = append(history, ChatMessage{Role: "assistant", Content: strings.TrimSpace(answer)})
	value, _ := json.Marshal(history)
	if err := s.results.PutAIResult(ctx, StoredResult{Kind: chatResultKind, SourceHash: key, Value: string(value), Target: "", Model: c.Model, CreatedAt: time.Now().Unix()}); err != nil {
		return ChatResult{}, ErrStorage
	}
	return ChatResult{ContentID: in.ContentID, Messages: history, UpdatedAt: time.Now().Unix()}, nil
}

func utf8ValidChat(contextText string, messages []ChatMessage) bool {
	if len(contextText) > 200000 || strings.ContainsAny(contextText, "\x00") {
		return false
	}
	for _, message := range messages {
		if (message.Role != "user" && message.Role != "assistant") || strings.TrimSpace(message.Content) == "" || len(message.Content) > 12000 {
			return false
		}
	}
	return true
}

func chatContext(content string, history []ChatMessage) string {
	return fmt.Sprintf("内容上下文：\n%s\n\n对话：%v", content, history)
}
