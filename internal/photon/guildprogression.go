package photon


import (
	"encoding/xml"
	"log"
	"os"
	"sync"
	"time"
)

type xmlGuildProgressionLevel struct {
	LevelNumber    int `xml:"m_levelNumber"`
	ExpRequirement int `xml:"m_expRequirement"`
}

type xmlGuildPlaytimeFactor struct {
	MinTime  int `xml:"m_minTime"` // minutes
	MaxTime  int `xml:"m_maxTime"` // minutes
	ExpValue int `xml:"m_expValue"`
}

type xmlGuildDifficultyFactor struct {
	Grade    string `xml:"m_grade"`
	ExpValue int    `xml:"m_expValue"`
}

type xmlGuildSkill struct {
	ID          int     `xml:"m_id"`
	UnlockLevel int     `xml:"m_unlockLevel"`
	Type        string  `xml:"m_type"`
	Value       float64 `xml:"m_value"`
}

type xmlGuildProgression struct {
	ProgressionTable  []xmlGuildProgressionLevel `xml:"m_progressionTable>m_level"`
	Skills            []xmlGuildSkill            `xml:"m_guildSkills>m_guildSkill"`
	PlaytimeFactors   []xmlGuildPlaytimeFactor   `xml:"m_guildExpFactors>m_PvEGuildExpFactors>m_playtimeFactors>m_expPlaytime"`
	DifficultyFactors []xmlGuildDifficultyFactor `xml:"m_guildExpFactors>m_PvEGuildExpFactors>m_difficultyFactors>m_expDifficulty"`
	SkillSelectStep   int                        `xml:"m_guildSkillSelectStep"`
}

var (
	guildProgressionOnce sync.Once
	guildProgression     xmlGuildProgression
)

func loadGuildProgressionConfig() {
	guildProgressionOnce.Do(func() {
		raw, err := os.ReadFile("data/guild/GuildProgression.xml")
		if err != nil {
			log.Printf("[GUILD] data/guild/GuildProgression.xml not found: %v", err)
			return
		}
		if err := xml.Unmarshal(raw, &guildProgression); err != nil {
			log.Printf("[GUILD] bad GuildProgression.xml: %v", err)
			return
		}
		log.Printf("[GUILD] loaded GuildProgression.xml: %d levels, %d playtime factors, %d difficulty factors, %d skills",
			len(guildProgression.ProgressionTable), len(guildProgression.PlaytimeFactors),
			len(guildProgression.DifficultyFactors), len(guildProgression.Skills))
	})
}

func guildLevelForExp(exp int) int {
	loadGuildProgressionConfig()
	level := 1
	for _, l := range guildProgression.ProgressionTable {
		if exp >= l.ExpRequirement {
			level = l.LevelNumber
		}
	}
	return level
}

func guildInstanceExp(room *Room, duration time.Duration) int {
	loadGuildProgressionConfig()
	switch gamemodeName(room) {
	case "GamemodePvE", "GameModeSurvival":
	default:
		return 0
	}

	exp := 0
	seconds := duration.Seconds()
	for _, f := range guildProgression.PlaytimeFactors {
		if seconds > float64(f.MinTime*60) && seconds <= float64(f.MaxTime*60) {
			exp = f.ExpValue
			break
		}
	}

	difficulty := int(asInt32(room.Properties["Difficulty"]))
	if idx := difficulty - 1; idx >= 0 && idx < len(guildProgression.DifficultyFactors) {
		exp += guildProgression.DifficultyFactors[idx].ExpValue
	}
	return exp
}
