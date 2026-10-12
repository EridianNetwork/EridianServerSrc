package services

import (
	"fmt"
	"log"
	"sync"
	"time"

	"eridian/internal/online"
)

// ChatMessage represents a chat message.
type ChatMessage struct {
	ID        int64  `json:"id"`
	ChannelID string `json:"channel_id"`
	SenderID  int64  `json:"sender_id"`
	Sender    string `json:"sender"`
	Content   string `json:"content"`
	SentAt    string `json:"sent_at"`
}

// ChatChannel is a chat channel with an in-memory history.
type ChatChannel struct {
	mu       sync.RWMutex
	Messages []*ChatMessage
	nextID   int64
}

const maxChatMessages = 100

// ChatManager manages the chat channels.
type ChatManager struct {
	mu       sync.RWMutex
	channels map[string]*ChatChannel
}

var chatMgr = &ChatManager{
	channels: make(map[string]*ChatChannel),
}

func getOrCreateChannel(channelID string) *ChatChannel {
	chatMgr.mu.Lock()
	defer chatMgr.mu.Unlock()

	ch, exists := chatMgr.channels[channelID]
	if !exists {
		ch = &ChatChannel{
			Messages: make([]*ChatMessage, 0, maxChatMessages),
			nextID:   1,
		}
		chatMgr.channels[channelID] = ch
	}
	return ch
}

// RegisterChatService registers the chat handlers.
func RegisterChatService(s *online.OnlineServer) {
	s.Handle(online.ServiceChatSend, handleChatSend)
	s.Handle(online.ServiceChatGet, handleChatGet)
	s.Handle(online.ServiceChatChanJoin, handleChatChanJoin)
	s.Handle(online.ServiceChatChanLeave, handleChatChanLeave)
}

func handleChatSend(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	channelID, _ := data["channel_id"].(string)
	if channelID == "" {
		channelID = "global"
	}
	content, _ := data["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("content_required")
	}

	ch := getOrCreateChannel(channelID)
	ch.mu.Lock()

	msg := &ChatMessage{
		ID:        ch.nextID,
		ChannelID: channelID,
		SenderID:  c.AccountID,
		Sender:    c.Username,
		Content:   content,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}
	ch.nextID++

	if len(ch.Messages) >= maxChatMessages {
		ch.Messages = ch.Messages[1:]
	}
	ch.Messages = append(ch.Messages, msg)
	ch.mu.Unlock()

	log.Printf("[CHAT] %s/%s: <%s> %s", channelID, c.Username, c.Username, content)
	return map[string]any{"message": msg}, nil
}

func handleChatGet(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	channelID, _ := data["channel_id"].(string)
	if channelID == "" {
		channelID = "global"
	}

	since := int64(0)
	if v, ok := toInt64(data["since"]); ok {
		since = v
	}

	ch := getOrCreateChannel(channelID)
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	var messages []*ChatMessage
	for _, m := range ch.Messages {
		if m.ID > since {
			messages = append(messages, m)
		}
	}
	if messages == nil {
		messages = []*ChatMessage{}
	}

	return map[string]any{"messages": messages}, nil
}

func handleChatChanJoin(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	channelID, _ := data["channel_id"].(string)
	if channelID == "" {
		return nil, fmt.Errorf("channel_id_required")
	}

	getOrCreateChannel(channelID)
	log.Printf("[CHAT] %s joined channel %s", c.Username, channelID)
	return map[string]any{"channel_id": channelID}, nil
}

func handleChatChanLeave(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	channelID, _ := data["channel_id"].(string)
	if channelID == "" {
		return nil, fmt.Errorf("channel_id_required")
	}

	log.Printf("[CHAT] %s left channel %s", c.Username, channelID)
	return map[string]any{"channel_id": channelID}, nil
}
