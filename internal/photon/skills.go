package photon

import (
	"encoding/json"
	"encoding/xml"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"eridian/internal/db"
	"eridian/internal/domain"
)

type xmlSkillEffect struct {
	Type       string  `xml:"m_type"`
	Operation  string  `xml:"m_operation"`
	FirstGrade float64 `xml:"m_firstGrade"`
	PerGrade   float64 `xml:"m_perGrade"`
}

type xmlSkillNode struct {
	ID       int              `xml:"m_id"`
	MaxGrade int              `xml:"m_maxGrade"`
	Effects  []xmlSkillEffect `xml:"m_effectList>m_effect"`
}

type xmlSkillSetup struct {
	Skills []xmlSkillNode `xml:"m_skills>m_skill"`
}

type xmlSkillTreeNode struct {
	ID int `xml:"m_id"`
}

type xmlSkillNodeGroup struct {
	Nodes []xmlSkillTreeNode `xml:"m_nodeList>m_node"`
}

type xmlSkillTier struct {
	NodeGroups []xmlSkillNodeGroup `xml:"m_nodeGroupList>m_nodeGroup"`
}

type xmlSkillTreeEntry struct {
	AvatarType string         `xml:"m_avatarType"`
	Tiers      []xmlSkillTier `xml:"m_tierList>m_tier"`
}

type xmlSkillTreeSetup struct {
	Trees []xmlSkillTreeEntry `xml:"m_trees>m_tree"`
}

var avatarTypeToClass = map[string]int{
	"SIREN": 0, "SOLDIER": 1, "BERSERKER": 2, "HUNTER": 3,
}

var (
	skillsOnce     sync.Once
	skillMaxGrade  map[int]int
	skillClassType map[int]int
	skillEffects   map[int][]xmlSkillEffect
)

func loadSkillCatalog() {
	skillsOnce.Do(func() {
		skillMaxGrade = map[int]int{}
		skillClassType = map[int]int{}
		skillEffects = map[int][]xmlSkillEffect{}
		if raw, err := os.ReadFile("data/skills/SkillNode.xml"); err != nil {
			log.Printf("[PHOTON] data/skills/SkillNode.xml not loaded: %v", err)
		} else {
			var setup xmlSkillSetup
			if err := xml.Unmarshal(raw, &setup); err != nil {
				log.Printf("[PHOTON] bad SkillNode.xml: %v", err)
			} else {
				for _, sk := range setup.Skills {
					skillMaxGrade[sk.ID] = sk.MaxGrade
					if len(sk.Effects) > 0 {
						skillEffects[sk.ID] = sk.Effects
					}
				}
			}
		}
		if raw, err := os.ReadFile("data/skills/SkillTree.xml"); err != nil {
			log.Printf("[PHOTON] data/skills/SkillTree.xml not loaded: %v", err)
		} else {
			var setup xmlSkillTreeSetup
			if err := xml.Unmarshal(raw, &setup); err != nil {
				log.Printf("[PHOTON] bad SkillTree.xml: %v", err)
			} else {
				for _, tree := range setup.Trees {
					classType, ok := avatarTypeToClass[tree.AvatarType]
					if !ok {
						continue
					}
					for _, tier := range tree.Tiers {
						for _, grp := range tier.NodeGroups {
							for _, node := range grp.Nodes {
								skillClassType[node.ID] = classType
							}
						}
					}
				}
			}
		}
		log.Printf("[PHOTON] loaded skill catalog: %d skills", len(skillMaxGrade))
	})
}

func earnedSkillPoints(level int) int {
	loadLevelingConfig()
	if level > len(levelingTable) {
		level = len(levelingTable)
	}
	total := 0
	for i := 0; i < level; i++ {
		total += levelingTable[i].SkillPointGain
	}
	return total
}

func investedSkillPoints(ch *domain.Character) map[int]int32 {
	out := map[int]int32{}
	if ch == nil || ch.Skills == "" || ch.Skills == "{}" {
		return out
	}
	var raw map[string]int32
	if err := json.Unmarshal([]byte(ch.Skills), &raw); err != nil {
		return out
	}
	for k, v := range raw {
		id, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		out[id] = v
	}
	return out
}

func persistInvestedSkills(charID int64, invested map[int]int32) error {
	raw := make(map[string]int32, len(invested))
	for id, grade := range invested {
		raw[strconv.Itoa(id)] = grade
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return db.UpdateCharacterSkills(charID, string(b))
}

func totalInvestedSkillPoints(invested map[int]int32) int32 {
	total := int32(0)
	for _, g := range invested {
		total += g
	}
	return total
}

func fullClassSkillPoints(ch *domain.Character) map[int]int32 {
	loadSkillCatalog()
	invested := investedSkillPoints(ch)
	full := make(map[int]int32, len(skillClassType))
	for id, classType := range skillClassType {
		if classType == ch.ClassType {
			full[id] = invested[id]
		}
	}
	return full
}

func skillTreeStatusStream(ch *domain.Character) string {
	var b strings.Builder
	for id, grade := range fullClassSkillPoints(ch) {
		b.WriteString(strconv.Itoa(id))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(int(grade)))
		b.WriteByte(';')
	}
	return b.String()
}

func effectValue(ch *domain.Character, effectType string) float64 {
	if ch == nil {
		return 0
	}
	loadSkillCatalog()
	total := 0.0
	for skillID, grade := range investedSkillPoints(ch) {
		if grade <= 0 {
			continue
		}
		if classType, ok := skillClassType[skillID]; !ok || classType != ch.ClassType {
			continue
		}
		for _, eff := range skillEffects[skillID] {
			if eff.Type != effectType {
				continue
			}
			total += eff.FirstGrade + float64(grade-1)*eff.PerGrade
		}
	}
	return total
}

func grenadeCapacityBonus(ch *domain.Character) int32 {
	return int32(effectValue(ch, "GrenadeCapacity"))
}

func maxHPScale(ch *domain.Character) float64 {
	return effectValue(ch, "EnlargeMaxHP")
}

func maxShieldScale(ch *domain.Character) float64 {
	return effectValue(ch, "EnlargeMaxShield")
}

func weaponDamageScale(ch *domain.Character) float64 {
	return effectValue(ch, "WeaponDamage")
}

func criticalHitDamageScale(ch *domain.Character) float64 {
	return effectValue(ch, "CriticalHitDamage")
}

func turretWeaponDamageMult(sess *ClientSession) float64 {
	if sess == nil || !sess.turretActive {
		return 1
	}
	return 1 + weaponDamageScale(sess.hostChar)
}

func skillCooldownScale(ch *domain.Character, effectType string) float64 {
	return effectValue(ch, effectType)
}

func multiMarkCountBonus(ch *domain.Character) int32 {
	return int32(effectValue(ch, "MultiMarkCount"))
}
