package photon

import (
	"log"
	"time"

	"eridian/internal/db"
)

const maxScoringDuration = 45 * time.Minute

func (s *EnetServer) grantInstanceRewards(room *Room, sess *ClientSession) {
	if room == nil || sess.hostChar == nil {
		return
	}
	loadScoringConfig()
	playlistID := asInt32(room.Properties["PlaylistId"])
	pl := playlistInfo(playlistID)
	if pl == nil {
		return
	}
	
	difficulty := asInt32(room.Properties["Difficulty"])
	instanceLevel, expReward := pl.NormalLevelMin, pl.NormalExp
	switch difficulty {
	case 1:
		instanceLevel, expReward = pl.HardLevelMin, pl.HardExp
	case 2:
		instanceLevel, expReward = pl.HellLevelMin, pl.HellExp
	}

	charLevel := sess.hostChar.Level
	duration := time.Since(room.CreatedAt)
	if duration > maxScoringDuration {
		duration = maxScoringDuration
	}
	moneyLevel := charLevel
	if instanceLevel < moneyLevel {
		moneyLevel = instanceLevel
	}
	money := duration.Seconds() * scoringPVE.MoneyStandardReward *
		rewardRate(scoringPVE.MoneyRewardRatePerLevel, moneyLevel) *
		scoringPVE.ModeModifierSoftCurrency * scoringPVE.WinBonusSoftCurrency

	exp := float64(expReward) * levelDiffModifier(instanceLevel, charLevel)

	charID := sess.hostChar.CharID
	if money >= 1 {
		if err := db.AddCharacterMoney(charID, int(money)); err != nil {
			log.Printf("[SCORING] AddCharacterMoney char=%d: %v", charID, err)
		} else if online := s.sessionOnline(sess.userID); online != nil {
			s.sendCurrencyUpdate(online, charID)
		}
	}
	if exp >= 1 {
		s.hostGrantExp(sess, int(exp))
	}
	log.Printf("[SCORING] instance complete char=%d playlist=%d difficulty=%d duration=%.0fs -> money=%d exp=%d",
		charID, playlistID, difficulty, duration.Seconds(), int(money), int(exp))

	sess.statExtraExp = int32(exp)
	sess.statExtraMoney = int32(money)
	log.Printf("[SCORING] resending real session stats char=%d kills=%d critKills=%d badass=%d boss=%d deaths=%d hits=%d critHits=%d totalDmg=%d critDmg=%d skillUses=%d extraExp=%d extraMoney=%d",
		charID, sess.statKills, sess.statCriticalKills, sess.statBadassKills, sess.statBossKills, sess.statDeaths,
		sess.statHits, sess.statCriticalHits, sess.statTotalDamage, sess.statCritDmg, sess.statSkillUses, sess.statExtraExp, sess.statExtraMoney)
	s.hostSendRPCTo(sess, sessionInfoViewID, "RPC_RequestSendSessionStats", []any{s.sessionStatsPayload(room)})

	s.grantGuildExp(room, sess, duration)
}

func (s *EnetServer) grantGuildExp(room *Room, sess *ClientSession, duration time.Duration) {
	guildID, err := db.GetAccountGuildID(sess.dbAccountID)
	if err != nil || guildID == 0 {
		return
	}
	gain := guildInstanceExp(room, duration)
	if gain <= 0 {
		return
	}
	g, err := db.GetGuild(guildID)
	if err != nil {
		log.Printf("[GUILD] grantGuildExp guild=%d: %v", guildID, err)
		return
	}
	newExp, err := db.AddGuildExperience(guildID, gain)
	if err != nil {
		log.Printf("[GUILD] AddGuildExperience guild=%d: %v", guildID, err)
		return
	}
	newLevel := guildLevelForExp(newExp)
	log.Printf("[GUILD] guild=%d gained %d exp (char=%d) total=%d level=%d", guildID, gain, sess.hostChar.CharID, newExp, newLevel)
	if newLevel <= g.Level {
		return
	}
	if err := db.SetGuildLevel(guildID, newLevel); err != nil {
		log.Printf("[GUILD] SetGuildLevel guild=%d: %v", guildID, err)
		return
	}
	log.Printf("[GUILD] guild=%d leveled up %d -> %d", guildID, g.Level, newLevel)
	if err := db.AddGuildLevelUpNews(guildID, sess.dbAccountID, sess.hostChar.CharID, sess.hostChar.Name, newLevel); err != nil {
		log.Printf("[GUILD] AddGuildLevelUpNews guild=%d: %v", guildID, err)
	}
}
