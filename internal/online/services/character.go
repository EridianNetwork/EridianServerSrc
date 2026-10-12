package services

import (
	"fmt"
	"log"

	"eridian/internal/db"
	"eridian/internal/domain"
	"eridian/internal/online"
)

// RegisterCharacterService registers character-related handlers.
func RegisterCharacterService(s *online.OnlineServer) {
	s.Handle(online.ServiceCharList, handleCharList)
	s.Handle(online.ServiceCharCreate, handleCharCreate)
	s.Handle(online.ServiceCharSelect, handleCharSelect)
	s.Handle(online.ServiceCharDelete, handleCharDelete)
	s.Handle(online.ServiceCharSave, handleCharSave)
	s.Handle(online.ServiceCharGet, handleCharGet)
}

func handleCharList(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}
	chars, err := db.GetCharactersByAccount(c.AccountID)
	if err != nil {
		return nil, fmt.Errorf("get_characters: %w", err)
	}
	if chars == nil {
		chars = []*domain.Character{}
	}
	return map[string]any{"characters": chars}, nil
}

func handleCharCreate(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	name, _ := data["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name_required")
	}

	classType, _ := toInt(data["class_type"])
	skinColor, _ := toInt(data["skin_color"])
	eyeColor, _ := toInt(data["eye_color"])
	hairColor, _ := toInt(data["hair_color"])
	hairStyle, _ := toInt(data["hair_style"])
	faceStyle, _ := toInt(data["face_style"])
	headStyle, _ := toInt(data["head_style"])
	decoStyle, _ := toInt(data["deco_style"])

	// Limit to 4 characters per account
	existing, err := db.GetCharactersByAccount(c.AccountID)
	if err != nil {
		return nil, fmt.Errorf("get_existing: %w", err)
	}
	if len(existing) >= 4 {
		return nil, fmt.Errorf("max_characters_reached")
	}

	char := &domain.Character{
		AccountID: c.AccountID,
		Name:      name,
		ClassType: classType,
		SkinColor: skinColor,
		EyeColor:  eyeColor,
		HairColor: hairColor,
		HairStyle: hairStyle,
		FaceStyle: faceStyle,
		HeadStyle: headStyle,
		DecoStyle: decoStyle,
		Level:     1,
		Health:    100.0,
		Shield:    100.0,
	}

	id, err := db.CreateCharacter(char)
	if err != nil {
		return nil, fmt.Errorf("create_character: %w", err)
	}
	char.CharID = id

	log.Printf("[ONLINE] character created: %s (id=%d, account=%d)", name, id, c.AccountID)
	return map[string]any{"character": char}, nil
}

func handleCharSelect(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	charID, ok := toInt64(data["char_id"])
	if !ok {
		return nil, fmt.Errorf("char_id_required")
	}

	char, err := db.GetCharacterByID(charID)
	if err != nil {
		return nil, fmt.Errorf("character_not_found")
	}
	if char.AccountID != c.AccountID {
		return nil, fmt.Errorf("not_your_character")
	}

	c.CharID = charID
	log.Printf("[ONLINE] character selected: %s (id=%d, account=%d)", char.Name, charID, c.AccountID)
	return map[string]any{"character": char}, nil
}

func handleCharDelete(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	charID, ok := toInt64(data["char_id"])
	if !ok {
		return nil, fmt.Errorf("char_id_required")
	}

	char, err := db.GetCharacterByID(charID)
	if err != nil {
		return nil, fmt.Errorf("character_not_found")
	}
	if char.AccountID != c.AccountID {
		return nil, fmt.Errorf("not_your_character")
	}

	// Clean up inventory, quests, PvP stats
	if err := db.DeleteCharacterInventory(charID); err != nil {
		return nil, fmt.Errorf("clear_inventory: %w", err)
	}
	if err := db.SoftDeleteCharacter(charID); err != nil {
		return nil, fmt.Errorf("delete_character: %w", err)
	}

	if c.CharID == charID {
		c.CharID = 0
	}

	log.Printf("[ONLINE] character deleted: id=%d, account=%d", charID, c.AccountID)
	return map[string]any{"char_id": charID}, nil
}

func handleCharSave(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	charID := c.CharID
	if cid, ok := toInt64(data["char_id"]); ok {
		charID = cid
	}
	if charID == 0 {
		return nil, fmt.Errorf("no_character_selected")
	}

	char, err := db.GetCharacterByID(charID)
	if err != nil {
		return nil, fmt.Errorf("character_not_found")
	}
	if char.AccountID != c.AccountID {
		return nil, fmt.Errorf("not_your_character")
	}

	// Apply changes from data
	if v, ok := toInt(data["level"]); ok {
		char.Level = v
	}
	if v, ok := toInt(data["experience"]); ok {
		char.Experience = v
	}
	if v, ok := toFloat64(data["health"]); ok {
		char.Health = v
	}
	if v, ok := toFloat64(data["shield"]); ok {
		char.Shield = v
	}
	if v, ok := toFloat64(data["position_x"]); ok {
		char.PositionX = v
	}
	if v, ok := toFloat64(data["position_y"]); ok {
		char.PositionY = v
	}
	if v, ok := toFloat64(data["position_z"]); ok {
		char.PositionZ = v
	}
	if v, ok := toFloat64(data["rotation"]); ok {
		char.Rotation = v
	}
	if v, ok := toInt(data["money"]); ok {
		char.Money = v
	}
	if v, ok := toInt(data["eridium"]); ok {
		char.Eridium = v
	}
	if v, ok := toInt(data["seraph_crystals"]); ok {
		char.SeraphCrystals = v
	}
	if v, ok := toInt(data["play_time"]); ok {
		char.PlayTime = v
	}
	if v, ok := toInt(data["class_type"]); ok {
		char.ClassType = v
	}

	if err := db.UpdateCharacter(char); err != nil {
		return nil, fmt.Errorf("save_character: %w", err)
	}

	return map[string]any{"character": char}, nil
}

func handleCharGet(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	charID, ok := toInt64(data["char_id"])
	if !ok {
		charID = c.CharID
	}
	if charID == 0 {
		return nil, fmt.Errorf("char_id_required")
	}

	char, err := db.GetCharacterByID(charID)
	if err != nil {
		return nil, fmt.Errorf("character_not_found")
	}
	if char.AccountID != c.AccountID {
		return nil, fmt.Errorf("not_your_character")
	}

	return map[string]any{"character": char}, nil
}
