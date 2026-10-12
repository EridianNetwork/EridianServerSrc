package photon

import (
	"encoding/xml"
	"log"
	"os"
	"slices"
	"sync"

	"eridian/internal/db"
)

type xmlLuckyBoxRank struct {
	Rank     string `xml:"m_rank"`
	KeyRank  string `xml:"m_keyRank"`
	KeyCount int    `xml:"m_keyCount"`
}

type xmlLuckyBoxReward struct {
	Grade        string `xml:"m_grade"`
	NormalRank   string `xml:"m_normalRank"`
	NormalReward string `xml:"m_normalReward"`
	LockedRank   string `xml:"m_lockedRank"`
	LockedReward string `xml:"m_lockedReward"`
}

type xmlLuckyBoxDifficultyRewards struct {
	Difficulty string              `xml:"m_difficulty"`
	Rewards    []xmlLuckyBoxReward `xml:"m_rewards>m_reward"`
}

type xmlLuckyBoxEntry struct {
	Playlists     []int32                        `xml:"m_playlists>m_id"`
	PerDifficulty []xmlLuckyBoxDifficultyRewards `xml:"m_perDificultyList>m_rewardPerDifficulty"`
}

type xmlLuckyBoxesSetup struct {
	BoxRanks []xmlLuckyBoxRank  `xml:"m_boxRanks>m_box"`
	Boxes    []xmlLuckyBoxEntry `xml:"m_luckyBoxes>m_luckyBox"`
}

var (
	luckyBoxConfigOnce sync.Once
	luckyBoxSetup      xmlLuckyBoxesSetup
)

func loadLuckyBoxConfig() {
	luckyBoxConfigOnce.Do(func() {
		raw, err := os.ReadFile("data/luckybox/LuckyBoxes.xml")
		if err != nil {
			log.Printf("[LUCKYBOX] data/luckybox/LuckyBoxes.xml not found: %v", err)
			return
		}
		if err := xml.Unmarshal(raw, &luckyBoxSetup); err != nil {
			log.Printf("[LUCKYBOX] bad LuckyBoxes.xml: %v", err)
			return
		}
		log.Printf("[LUCKYBOX] loaded LuckyBoxes.xml: %d box ranks, %d lucky boxes",
			len(luckyBoxSetup.BoxRanks), len(luckyBoxSetup.Boxes))
	})
}

var luckyBoxDifficultyNames = []string{"Normal", "Hard", "Hell"}

func findLuckyBoxReward(playlistID, difficulty int32, grade string) *xmlLuckyBoxReward {
	loadLuckyBoxConfig()
	diffName := "Normal"
	if difficulty >= 0 && int(difficulty) < len(luckyBoxDifficultyNames) {
		diffName = luckyBoxDifficultyNames[difficulty]
	}
	for _, box := range luckyBoxSetup.Boxes {
		found := slices.Contains(box.Playlists, playlistID)
		if !found {
			continue
		}
		for _, pd := range box.PerDifficulty {
			if pd.Difficulty != diffName {
				continue
			}
			for i := range pd.Rewards {
				if pd.Rewards[i].Grade == grade {
					return &pd.Rewards[i]
				}
			}
		}
	}
	return nil
}

var luckyBoxRankValues = map[string]int{
	"None": 0, "Platinum": 1, "Gold": 2, "Silver": 3, "Bronze": 4, "Iron": 5,
	"Diamond": 6, "Ruby": 7, "Sapphire": 8, "Emerald": 9, "Garnet": 10,
}

func luckyKeyExpiringHashtable(charID int64) map[int]any {
	have, err := db.LuckyKeyQuantity(charID, luckyBoxRankValues["Diamond"])
	if err != nil || have <= 0 {
		return map[int]any{}
	}
	const effectivelyPermanentSeconds = 10 * 365 * 24 * 60 * 60
	return map[int]any{
		luckyBoxRankValues["Diamond"]: map[int]any{0: int32(have), 1: float32(effectivelyPermanentSeconds)},
	}
}

func luckyBoxRequiredKeyRank(rank string) (keyRank string, keyCount int) {
	loadLuckyBoxConfig()
	for _, r := range luckyBoxSetup.BoxRanks {
		if r.Rank == rank {
			if r.KeyRank == "None" {
				return "", 0
			}
			return r.KeyRank, r.KeyCount
		}
	}
	return "", 0
}
