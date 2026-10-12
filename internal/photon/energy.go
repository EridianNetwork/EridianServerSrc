package photon

import (
	"log"
	"time"

	"eridian/internal/db"
	"eridian/internal/domain"
)


const (
	maxEnergy = 20
	energyRegenInterval = 5 * time.Minute
)

// currentEnergy computes energy value. right now we are bypassing energy cost
// so we can probably avoid running this. might be better for server health, but this call is pretty cheap
func currentEnergy(ch *domain.Character) int32 {
	if ch.EnergyUpdatedAt == "" {
		return int32(maxEnergy)
	}
	updated, err := time.Parse("2006-01-02 15:04:05", ch.EnergyUpdatedAt)
	if err != nil {
		return int32(ch.Energy)
	}
	energy := ch.Energy + int(time.Since(updated)/energyRegenInterval)
	if energy > maxEnergy {
		energy = maxEnergy
	}
	return int32(energy)
}

// again we should probably just not call this. energy is some kind of P2W sys
func consumeEnergyForInstance(room *Room, charID int64) {
	switch gamemodeName(room) {
	case "GamemodePvE", "GameModeSurvival":
	default:
		return
	}
	ch, err := db.GetCharacterByID(charID)
	if err != nil || ch == nil {
		return
	}
	newEnergy := currentEnergy(ch) - 1
	if newEnergy < 0 {
		newEnergy = 0
	}
	if err := db.SetCharacterEnergy(charID, newEnergy); err != nil {
		log.Printf("[PHOTON] consumeEnergyForInstance charID=%d: %v", charID, err)
	}
}
