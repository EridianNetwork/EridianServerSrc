package photon

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
)

type outfitPreset struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Rarity        string `json:"rarity"`
	AvatarType    string `json:"avatarType"`
	AvatarSubpart string `json:"avatarSubpart"`
	Index         int    `json:"index"`
}

var (
	outfitsOnce   sync.Once
	outfitPresets []outfitPreset
)

func loadOutfitPresets() {
	outfitsOnce.Do(func() {
		raw, err := os.ReadFile("data/outfits.json")
		if err != nil {
			log.Printf("[SHOP] data/outfits.json not loaded: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &outfitPresets); err != nil {
			log.Printf("[SHOP] data/outfits.json parse error: %v", err)
			outfitPresets = nil
			return
		}
		log.Printf("[SHOP] loaded %d real player outfit presets", len(outfitPresets))
	})
}

var avatarTypeNames = map[int]string{
	0: "SIREN", 1: "SOLDIER", 2: "BERSERKER", 3: "HUNTER",
}

var avatarSubPartValues = map[string]int{
	"HAIR": 0, "HEAD": 1, "MASK": 2, "BODY": 3,
	"PANTS": 4, "SHOE": 5, "CM": 6, "GLOVER": 7,
}

func findOutfitPresets(avatarClass int, query string) []*outfitPreset {
	loadOutfitPresets()
	wantType := avatarTypeNames[avatarClass]
	query = strings.ToLower(strings.TrimSpace(query))
	var out []*outfitPreset
	for i := range outfitPresets {
		if outfitPresets[i].AvatarType != wantType {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(outfitPresets[i].Name), query) {
			continue
		}
		out = append(out, &outfitPresets[i])
	}
	return out
}

func outfitPresetByID(id int) *outfitPreset {
	loadOutfitPresets()
	for i := range outfitPresets {
		if outfitPresets[i].ID == id {
			return &outfitPresets[i]
		}
	}
	return nil
}
