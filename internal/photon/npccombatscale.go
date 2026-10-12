package photon

type NpcTierMult struct{ HP, Dmg float32 }

var defaultNpcDifficultyTiers = map[int32]NpcTierMult{
	0: {HP: 5, Dmg: 5},   // Normal
	1: {HP: 10, Dmg: 10}, // Hard
	2: {HP: 20, Dmg: 20}, // Hell
}

const defaultNpcPerLevelGrowth = 0.03

func (s *EnetServer) npcDifficultyTiers() map[int32]NpcTierMult {
	if len(s.NpcDifficultyTiers) > 0 {
		return s.NpcDifficultyTiers
	}
	return defaultNpcDifficultyTiers
}

func (s *EnetServer) npcPerLevelGrowth() float32 {
	if s.NpcPerLevelGrowth != 0 {
		return s.NpcPerLevelGrowth
	}
	return defaultNpcPerLevelGrowth
}

func roomInstanceLevel(room *Room) int {
	if room == nil {
		return 1
	}
	recLevel := 1
	if pl := playlistInfo(asInt32(room.Properties["PlaylistId"])); pl != nil {
		switch asInt32(room.Properties["Difficulty"]) {
		case 0:
			recLevel = pl.NormalLevelMin
		case 1:
			recLevel = pl.HardLevelMin
		case 2:
			recLevel = pl.HellLevelMin
		}
	}
	return recLevel
}

const npcOverlevelScale = 0.8

func (s *EnetServer) roomEffectiveInstanceLevel(room *Room) int {
	recLevel := roomInstanceLevel(room)
	if playerLevel := s.roomMaxCharLevel(room); playerLevel > recLevel {
		return playerLevel
	}
	return recLevel
}

func (s *EnetServer) npcHPPowerCurve(room *Room) float32 {
	rating := float32(powerRatingForLevel(s.roomEffectiveInstanceLevel(room)))
	if rating < 1 {
		rating = 1
	}
	return rating
}

func (s *EnetServer) npcCombatScale(room *Room) (hpMult, dmgMult float32) {
	if room == nil {
		return 1, 1
	}
	difficulty := asInt32(room.Properties["Difficulty"])
	tier, ok := s.npcDifficultyTiers()[difficulty]
	if !ok {
		return 1, 1
	}
	hpMult = tier.HP * s.npcHPPowerCurve(room)
	dmgMult = tier.Dmg
	if s.roomMaxCharLevel(room) > roomInstanceLevel(room) {
		dmgMult *= npcOverlevelScale
	}
	return hpMult, dmgMult
}

var defaultNpcBossHPTiers = map[int32]float32{
	1: 2, // Hard
	2: 5, // Hell
}

func (s *EnetServer) npcBossHPTiers() map[int32]float32 {
	if len(s.NpcBossHPTiers) > 0 {
		return s.NpcBossHPTiers
	}
	return defaultNpcBossHPTiers
}

func isMainBossKit(kit string) bool {
	return bossKits[kit] || bossHudKits[kit]
}

func (s *EnetServer) npcHPScaleForKit(room *Room, kit string) float32 {
	hpMult, _ := s.npcCombatScale(room)
	if room == nil || !isMainBossKit(kit) {
		return hpMult
	}
	difficulty := asInt32(room.Properties["Difficulty"])
	bossHP, ok := s.npcBossHPTiers()[difficulty]
	if !ok {
		return hpMult
	}
	regularHP := s.npcDifficultyTiers()[difficulty].HP
	if regularHP == 0 {
		return hpMult
	}
	levelFactor := hpMult / regularHP
	return bossHP * levelFactor
}

var defaultNpcWaveCountMult = map[int32]int{
	0: 1, // Normal
	1: 1, // Hard / "Hero"
	2: 1, // Hell
}

func npcWaveCountMult(room *Room) int {
	if room == nil {
		return 1
	}
	if mult, ok := defaultNpcWaveCountMult[asInt32(room.Properties["Difficulty"])]; ok {
		return mult
	}
	return 1
}

func (s *EnetServer) roomMaxCharLevel(room *Room) int {
	max := 1
	for _, uid := range room.Players {
		if sess := s.sessionInRoom(uid, room.Name); sess != nil && sess.hostChar != nil && sess.hostChar.Level > max {
			max = sess.hostChar.Level
		}
	}
	return max
}
