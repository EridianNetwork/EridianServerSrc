package photon

import (
	"log"
	"strings"

	"eridian/internal/db"
)

const (
	achIDCharacterLevel = 1
	achIDEnterTown      = 2
	achIDKillBoss       = 6
	achIDKillBadass     = 7
	achIDMeltItems      = 9
	achIDCraftItems     = 10
	achIDOpenKimBox     = 11
)

var bossKits = map[string]bool{
	"ArchAngel": true, "BatistaTheRocketLord": true, "CrimsonMechaKnoxx": true,
	"DrBlofeld": true, "InterstellarEraser": true, "MajorVanCleef": true,
	"SpitterQueen": true, "TheInsane": true, "TheInsaneCrystal": true,
	"TheInsaneTentacle": true, "TheInsaneTentacleLarge": true, "ZillaKing": true,
	"GuardianWraith": true,
}

func isBadassKit(kit string) bool {
	return strings.HasPrefix(kit, "Badass")
}

var bossHudKits = map[string]bool{
	"BanditApollyon": true, "SpitterQueen": true, "CrimsonMechaKnoxx": true,
	"ArchAngel": true, "TheInsane": true, "SpiderantTalos": true,
	"SpiderantWidowmaker": true,
}

// achievementByID finds one catalog entry, or nil.
func achievementByID(id int) *xmlAchievement {
	for _, a := range allAchievementDefs() {
		if a.ID == id {
			return &a
		}
	}
	return nil
}

func bumpAchievement(charID int64, achievementID, delta int) {
	def := achievementByID(achievementID)
	if def == nil {
		return
	}
	progress, err := db.IncrementAchievementProgress(charID, achievementID, delta)
	if err != nil {
		log.Printf("[ACHV] IncrementAchievementProgress char=%d achievement=%d: %v", charID, achievementID, err)
		return
	}
	if stage := achievementStageForProgress(*def, progress); stage > 0 {
		if err := db.UpdateAchievementStage(charID, achievementID, stage); err != nil {
			log.Printf("[ACHV] UpdateAchievementStage char=%d achievement=%d: %v", charID, achievementID, err)
		}
	}
}

var townPlaylistBit = map[int32]int{
	3: 1 << 0, // Floasm_Small  "Froasm"
	4: 1 << 1, // Union_City  "Union_city"
	5: 1 << 2, // Metropolis  "Metro_city"
}

var townGoalBit = map[string]int{
	"Froasm": 1 << 0, "Union_city": 1 << 1, "Metro_city": 1 << 2,
}

func bumpTownVisited(charID int64, playlistID int32) {
	bit, ok := townPlaylistBit[playlistID]
	if !ok {
		return
	}
	def := achievementByID(achIDEnterTown)
	if def == nil {
		return
	}
	existing, err := db.GetAchievementProgress(charID)
	if err != nil {
		log.Printf("[ACHV] GetAchievementProgress char=%d: %v", charID, err)
		return
	}
	mask := existing[achIDEnterTown].Progress | bit
	if mask == existing[achIDEnterTown].Progress {
		return // already recorded, skip the write
	}
	stage := 0
	for _, s := range def.Stages {
		if goalBit, ok := townGoalBit[s.Goal]; ok && mask&goalBit != 0 && s.StageID > stage {
			stage = s.StageID
		}
	}
	if err := db.SetAchievementBitmask(charID, achIDEnterTown, mask, stage); err != nil {
		log.Printf("[ACHV] SetAchievementBitmask char=%d: %v", charID, err)
	}
}
