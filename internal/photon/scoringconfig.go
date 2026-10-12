package photon

import (
	"encoding/xml"
	"log"
	"os"
	"sync"
)

type xmlLevelAdjustment struct {
	InstanceCharacterLevelDiff int     `xml:"m_instanceCharacterLevelDiff"`
	Modifier                   float64 `xml:"m_modifier"`
}

type xmlScoringBase struct {
	MoneyStandardReward      float64              `xml:"m_moneyStandardReward"`
	ModeModifierEXP          float64              `xml:"m_modeModifierEXP"`
	ModeModifierSoftCurrency float64              `xml:"m_modeModifierSoftCurrency"`
	WinBonusEXP              float64              `xml:"m_winBonusEXP"`
	WinBonusSoftCurrency     float64              `xml:"m_winBonusSoftCurrency"`
	MoneyRewardRatePerLevel  []float64            `xml:"m_moneyRewardRatePerLevel>m_value"`
	ExpRewardRatePerLevel    []float64            `xml:"m_expRewardRatePerLevel>m_value"`
	LevelAdjustments         []xmlLevelAdjustment `xml:"m_levelAdjustments>m_levelAdjustment"`
	TimeScoreConversionRate  float64              `xml:"m_timeScoreConversionRate"`
}

type xmlScoringSetup struct {
	Base xmlScoringBase `xml:"m_ScoringSetup"`
}

var (
	scoringConfigOnce sync.Once
	scoringPVE        xmlScoringBase
)

func loadScoringConfig() {
	scoringConfigOnce.Do(func() {
		raw, err := os.ReadFile("data/scoring/ScoringPVE.xml")
		if err != nil {
			log.Printf("[SCORING] data/scoring/ScoringPVE.xml not found: %v", err)
			return
		}
		var setup xmlScoringSetup
		if err := xml.Unmarshal(raw, &setup); err != nil {
			log.Printf("[SCORING] bad ScoringPVE.xml: %v", err)
			return
		}
		scoringPVE = setup.Base
		log.Printf("[SCORING] loaded ScoringPVE.xml: %d money-rate levels, %d exp-rate levels, %d level adjustments",
			len(scoringPVE.MoneyRewardRatePerLevel), len(scoringPVE.ExpRewardRatePerLevel), len(scoringPVE.LevelAdjustments))
	})
}

func rewardRate(curve []float64, level int) float64 {
	if len(curve) == 0 {
		return 1
	}
	if level < 1 {
		level = 1
	}
	if level > len(curve) {
		level = len(curve)
	}
	return curve[level-1]
}

func levelDiffModifier(instanceLevel, charLevel int) float64 {
	if len(scoringPVE.LevelAdjustments) == 0 {
		return 1
	}
	diff := instanceLevel - charLevel
	if diff < 0 {
		diff = -diff
	}
	for i := 0; i < len(scoringPVE.LevelAdjustments)-1; i++ {
		if diff <= scoringPVE.LevelAdjustments[i].InstanceCharacterLevelDiff {
			return scoringPVE.LevelAdjustments[i].Modifier
		}
	}
	return scoringPVE.LevelAdjustments[len(scoringPVE.LevelAdjustments)-1].Modifier
}
