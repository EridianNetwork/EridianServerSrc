package services

import (
	"fmt"
	"log"

	"eridian/internal/db"
	"eridian/internal/domain"
	"eridian/internal/online"
)

// RegisterFriendService registers the friend handlers.
func RegisterFriendService(s *online.OnlineServer) {
	s.Handle(online.ServiceFriendList, handleFriendList)
	s.Handle(online.ServiceFriendAdd, handleFriendAdd)
	s.Handle(online.ServiceFriendAccept, handleFriendAccept)
	s.Handle(online.ServiceFriendRemove, handleFriendRemove)
}

func handleFriendList(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	friends, err := db.GetFriends(c.AccountID)
	if err != nil {
		return nil, fmt.Errorf("get_friends: %w", err)
	}
	if friends == nil {
		friends = []*domain.Friend{}
	}

	// Get pending requests (incoming)
	pending, err := db.GetPendingFriendRequests(c.AccountID)
	if err != nil {
		pending = []*domain.FriendRequest{}
	}
	if pending == nil {
		pending = []*domain.FriendRequest{}
	}

	return map[string]any{
		"friends": friends,
		"pending": pending,
	}, nil
}

func handleFriendAdd(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	targetName, _ := data["username"].(string)
	if targetName == "" {
		return nil, fmt.Errorf("username_required")
	}

	target, err := db.GetAccountByUsername(targetName)
	if err != nil {
		return nil, fmt.Errorf("user_not_found")
	}
	if target.AccountID == c.AccountID {
		return nil, fmt.Errorf("cannot_add_self")
	}

	// Check if already friends
	existing, err := db.GetFriends(c.AccountID)
	if err == nil {
		for _, f := range existing {
			if f.FriendID == target.AccountID {
				return nil, fmt.Errorf("already_friends")
			}
		}
	}

	if err := db.AddFriendRequest(c.AccountID, target.AccountID); err != nil {
		return nil, fmt.Errorf("add_friend: %w", err)
	}

	log.Printf("[FRIEND] request sent: %s -> %s", c.Username, targetName)
	return map[string]any{
		"username": targetName,
		"status":   "pending",
	}, nil
}

func handleFriendAccept(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	requesterID, ok := toInt64(data["account_id"])
	if !ok {
		return nil, fmt.Errorf("account_id_required")
	}

	if err := db.AcceptFriendRequest(requesterID, c.AccountID); err != nil {
		return nil, fmt.Errorf("accept_friend: %w", err)
	}

	log.Printf("[FRIEND] accepted: %d and %d are now friends", requesterID, c.AccountID)
	return map[string]any{
		"account_id": requesterID,
		"status":     "accepted",
	}, nil
}

func handleFriendRemove(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	friendID, ok := toInt64(data["friend_id"])
	if !ok {
		return nil, fmt.Errorf("friend_id_required")
	}

	if err := db.RemoveFriend(c.AccountID, friendID); err != nil {
		return nil, fmt.Errorf("remove_friend: %w", err)
	}

	return map[string]any{"friend_id": friendID}, nil
}
