package photon

import (
	"encoding/xml"
	"log"
	"os"
	"sync"
)

type xmlLuckyLootTable struct {
	Name        string                 `xml:"m_lootingTableName"`
	LevelRanges []xmlLootingLevelRange `xml:"m_lootingLevelRangeList>m_lootingLevelRange"`
}

type xmlLuckyLootSetup struct {
	Tables []xmlLuckyLootTable `xml:"m_lootingTables>m_lootingTable"`
}

var (
	luckyLootConfigOnce sync.Once
	luckyLootSetup      xmlLuckyLootSetup
)

func loadLuckyLootConfig() {
	luckyLootConfigOnce.Do(func() {
		raw, err := os.ReadFile("data/luckybox/LuckyBoxesLoot.xml")
		if err != nil {
			log.Printf("[LUCKYBOX] data/luckybox/LuckyBoxesLoot.xml not found: %v", err)
			return
		}
		if err := xml.Unmarshal(raw, &luckyLootSetup); err != nil {
			log.Printf("[LUCKYBOX] bad LuckyBoxesLoot.xml: %v", err)
			return
		}
		log.Printf("[LUCKYBOX] loaded LuckyBoxesLoot.xml: %d tables", len(luckyLootSetup.Tables))
	})
}

func findLuckyLootRange(tableName string, level int) *xmlLootingLevelRange {
	loadLuckyLootConfig()
	for i := range luckyLootSetup.Tables {
		t := &luckyLootSetup.Tables[i]
		if t.Name != tableName {
			continue
		}
		for j := range t.LevelRanges {
			lr := &t.LevelRanges[j]
			if level >= lr.LevelMin && level <= lr.LevelMax {
				return lr
			}
		}
		if len(t.LevelRanges) == 0 {
			return nil
		}
		best := &t.LevelRanges[0]
		for j := range t.LevelRanges {
			if level > t.LevelRanges[j].LevelMax {
				best = &t.LevelRanges[j]
			}
		}
		return best
	}
	return nil
}

func rollLuckyBoxSlot(tableName string, level int) *rolledLoot {
	lr := findLuckyLootRange(tableName, level)
	if lr == nil {
		return nil
	}
	rolled := rollFromRange(lr)
	if len(rolled) == 0 {
		return nil
	}
	return &rolled[0]
}
