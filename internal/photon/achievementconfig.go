package photon


import (
	"encoding/xml"
	"log"
	"os"
	"strconv"
	"sync"
)

type xmlAchievementStage struct {
	StageID     int    `xml:"m_stageId"`
	RewardPoint int    `xml:"m_stageRewardPoint"`
	Goal        string `xml:"m_stageGoal"`
}

type xmlAchievement struct {
	ID     int                   `xml:"m_achievementId"`
	Name   string                `xml:"m_achievementName"`
	Stages []xmlAchievementStage `xml:"m_achievementStages>m_stage"`
}

type xmlAchievements struct {
	Achievements []xmlAchievement `xml:"m_achievementTable>m_achievement"`
}

var (
	achievementConfigOnce sync.Once
	achievementDefs       []xmlAchievement
)

func loadAchievementConfig() {
	achievementConfigOnce.Do(func() {
		raw, err := os.ReadFile("data/achievements/Achievements.xml")
		if err != nil {
			log.Printf("[ACHV] data/achievements/Achievements.xml not found: %v", err)
			return
		}
		var setup xmlAchievements
		if err := xml.Unmarshal(raw, &setup); err != nil {
			log.Printf("[ACHV] bad Achievements.xml: %v", err)
			return
		}
		achievementDefs = setup.Achievements
		log.Printf("[ACHV] loaded %d achievements from data/achievements/Achievements.xml", len(achievementDefs))
	})
}

func allAchievementDefs() []xmlAchievement {
	loadAchievementConfig()
	return achievementDefs
}

func achievementStageForProgress(a xmlAchievement, progress int) int {
	stage := 0
	for _, s := range a.Stages {
		goal, err := strconv.Atoi(s.Goal)
		if err != nil {
			continue
		}
		if progress >= goal {
			stage = s.StageID
		}
	}
	return stage
}
