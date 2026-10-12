package photon

import (
	"encoding/xml"
	"log"
	"os"
	"sync"

	"eridian/internal/domain"
)

type xmlProgressionLevel struct {
	RequiredXp     int     `xml:"m_requiredXp"`
	SkillPointGain int     `xml:"m_skillPointGain"`
	PowerRating    float64 `xml:"m_powerRating"`
}

type xmlPlayerProgressionSetup struct {
	Levels []xmlProgressionLevel `xml:"m_experienceTable>m_level"`
}

var (
	levelingOnce  sync.Once
	levelingTable []xmlProgressionLevel
)

func loadLevelingConfig() {
	levelingOnce.Do(func() {
		raw, err := os.ReadFile("data/leveling/PlayerProgression.xml")
		if err != nil {
			log.Printf("[LEVEL] data/leveling/PlayerProgression.xml not found: %v", err)
			return
		}
		var setup xmlPlayerProgressionSetup
		if err := xml.Unmarshal(raw, &setup); err != nil {
			log.Printf("[LEVEL] bad PlayerProgression.xml: %v", err)
			return
		}
		levelingTable = setup.Levels
		log.Printf("[LEVEL] loaded %d levels from data/leveling/PlayerProgression.xml", len(levelingTable))
	})
}

func levelForExp(exp int) int {
	loadLevelingConfig()
	if len(levelingTable) == 0 {
		return 1
	}
	for i := 1; i < len(levelingTable); i++ {
		if exp < levelingTable[i].RequiredXp {
			return i
		}
	}
	return len(levelingTable)
}

func maxLevel() int {
	loadLevelingConfig()
	if len(levelingTable) == 0 {
		return 1
	}
	return len(levelingTable)
}

func maxLifeForLevel(level int) int32 {
	loadLevelingConfig()
	if len(levelingTable) == 0 {
		return 120
	}
	if level < 1 {
		level = 1
	}
	if level > len(levelingTable) {
		level = len(levelingTable)
	}
	const baseHP = 100.0
	return int32(baseHP * levelingTable[level-1].PowerRating)
}

func maxLifeForCharacter(ch *domain.Character) int32 {
	if ch == nil {
		return maxLifeForLevel(1)
	}
	return int32(float64(maxLifeForLevel(ch.Level)) * (1 + maxHPScale(ch)))
}

func powerRatingForLevel(level int) float64 {
	loadLevelingConfig()
	if len(levelingTable) == 0 {
		return 1.0
	}
	if level < 1 {
		level = 1
	}
	if level > len(levelingTable) {
		level = len(levelingTable)
	}
	return levelingTable[level-1].PowerRating
}
