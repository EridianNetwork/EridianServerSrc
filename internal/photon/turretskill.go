package photon

import (
	"log"
	"time"
)

const (
	turretSpawnDelay = 500 * time.Millisecond

	turretLifetime = 30 * time.Second

	turretArmDuration = 1 * time.Second

	turretFireRangeFallback    = 60.0
	turretFireIntervalFallback = 1.5
	turretDamageFallback       = int32(15)
	turretDamageRadiusFallback = 4.0

	turretBaseDamagePct = 0.45

	turretDeployExplodeDamage = 80

	turretAOERadius       = float32(4)
	turretAOETickInterval = 2 * time.Second
)

func turretStats() (fireRange, fireInterval float64, damage int32, damageRadius float64) {
	fireRange, fireInterval, damage, damageRadius = turretFireRangeFallback, turretFireIntervalFallback, turretDamageFallback, turretDamageRadiusFallback
	if cfg := npcConfigFor("SoldierTurretBasic"); cfg != nil {
		if cfg.FireRange > 0 {
			fireRange = cfg.FireRange
		}
		if cfg.TurretFireInterval > 0 {
			fireInterval = cfg.TurretFireInterval
		}
		if cfg.TurretDamage > 0 {
			damage = cfg.TurretDamage
		}
		if cfg.TurretDamageRadius > 0 {
			damageRadius = cfg.TurretDamageRadius
		}
	}
	return
}

func (s *EnetServer) hostSpawnTurret(room *Room, sess *ClientSession) {
	pos, ok := room.playerPos[sess.avatarBase+1]
	if !ok {
		return
	}

	deployDist := float32(2) * (1 + float32(effectValue(sess.hostChar, "TurretDeployFar")))
	spawnPos := [3]float32{pos[0] + deployDist, pos[1], pos[2]}
	base := s.hostSpawnNpcAt(room, m2level(room), "SoldierTurretBasic", spawnPos, []any{int32(0), sess.avatarBase, int32(1)}, 3)
	if n := len(room.npcs); n > 0 && room.npcs[n-1].viewBase == base {
		room.npcs[n-1].friendly = true

		if hpScale := effectValue(sess.hostChar, "TurretHP"); hpScale > 0 {
			room.npcs[n-1].maxHP = int32(float64(room.npcs[n-1].maxHP) * (1 + hpScale))
			room.npcs[n-1].hp = room.npcs[n-1].maxHP
		}
	}

	if explodeBonus := effectValue(sess.hostChar, "TurretDeployExplode"); explodeBonus > 0 {
		dmg := int32(turretDeployExplodeDamage * (1 + explodeBonus))
		s.hostAreaDamageNpcs(room, sess.avatarBase, spawnPos, float32(turretDamageRadiusFallback), dmg, elementExplosive, false)
	}
	sess.turretView = base
	sess.turretActive = true
	sess.turretEndAt = time.Now().Add(turretLifetime)
	sess.turretArmedAt = time.Now().Add(turretArmDuration)
	sess.turretLastFireAt = time.Time{}
	sess.turretBaseDamage = s.playerWeaponDamageAtLevel(sess)

	s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
		int32(1), sess.avatarBase, int32(skillEventTurretSpawned),
		float32(turretLifetime.Seconds()), vector3Custom(spawnPos[0], spawnPos[1], spawnPos[2]),
	})
	log.Printf("[HOST] actor view=%d deployed Turret view=%d room=%s", sess.avatarBase, base, room.Name)
}

func (s *EnetServer) hostRetrieveTurret(room *Room, sess *ClientSession, manual bool) {
	if !sess.turretActive {
		return
	}
	sess.turretActive = false
	cooldown := s.turretCooldownLong(sess.hostChar)
	if manual {
		cooldown = s.turretCooldownShort()
	} else {

		s.hostResetSkillPhaseAfterTimeout(room, sess)
	}
	sess.skillCooldownEndAt = time.Now().Add(cooldown)
	s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
		int32(0), sess.avatarBase, int32(skillEventTurretCooldownStart),
		float32(cooldown.Seconds()), vector3Custom(0, 0, 0),
	})
	if sess.turretView != 0 {
		for i := range room.npcs {
			if room.npcs[i].viewBase == sess.turretView {
				room.npcs[i].hp = 0
				break
			}
		}
		room.deferredDestroys = append(room.deferredDestroys, deferredDestroy{view: sess.turretView, at: time.Now()})
		sess.turretView = 0
	}
	log.Printf("[HOST] actor view=%d retrieved Turret room=%s", sess.avatarBase, room.Name)
}

func (s *EnetServer) hostRunPendingTurretSpawns(room *Room) {
	if len(room.pendingTurretSpawns) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingTurretSpawns[:0]
	for _, p := range room.pendingTurretSpawns {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase != 0 && !p.sess.isDead {
			s.hostSpawnTurret(room, p.sess)
		}
	}
	room.pendingTurretSpawns = remaining
}

func (s *EnetServer) hostTickTurrets(room *Room) {
	now := time.Now()
	fireRange, fireInterval, _, damageRadius := turretStats()
	fireIntervalDur := time.Duration(fireInterval * float64(time.Second))
	r2 := float32(fireRange * fireRange)
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || !sess.turretActive {
			continue
		}
		if now.After(sess.turretEndAt) {
			s.hostRetrieveTurret(room, sess, false)
			continue
		}
		if now.Before(sess.turretArmedAt) {
			continue
		}
		var turret *spawnedNpc
		for i := range room.npcs {
			if room.npcs[i].viewBase == sess.turretView {
				turret = &room.npcs[i]
				break
			}
		}
		if turret == nil || turret.hp <= 0 {
			sess.turretActive, sess.turretView = false, 0
			continue
		}

		if now.Sub(sess.turretLastAOEAt) >= turretAOETickInterval {
			healPct := effectValue(sess.hostChar, "HealthRegenAOE")
			ammoPct := effectValue(sess.hostChar, "AmmoRegenAOE")
			if healPct > 0 || ammoPct > 0 {
				sess.turretLastAOEAt = now
				s.hostTurretAOERegen(room, turret, healPct, ammoPct)
			}
		}
		bestIdx, bestD := -1, r2
		for i := range room.npcs {
			cand := &room.npcs[i]
			if cand.hp <= 0 || cand.friendly {
				continue
			}
			dx, dz := cand.pos[0]-turret.pos[0], cand.pos[2]-turret.pos[2]
			if d := dx*dx + dz*dz; d <= bestD {
				bestD, bestIdx = d, i
			}
		}
		if bestIdx < 0 {

			npcSetTask(turret, bhvTaskIdle, -1, turret.pos, -1)
			continue
		}
		target := &room.npcs[bestIdx]
		npcSetTask(turret, bhvTaskRocketAttack, target.viewBase, turret.pos, -1)
		if now.Sub(sess.turretLastFireAt) < fireIntervalDur {
			continue
		}
		sess.turretLastFireAt = now
		dmg := int32(sess.turretBaseDamage * turretBaseDamagePct)

		dmg = int32(float64(dmg) * (1 + effectValue(sess.hostChar, "TurretRocket")))

		dmgElement := int32(elementExplosive)
		if electroBonus := effectValue(sess.hostChar, "TurretElectroStr"); electroBonus > 0 {
			dmgElement = elementShock
			dmg = int32(float64(dmg) * (1 + electroBonus))
		}
		s.hostAreaDamageNpcs(room, sess.avatarBase, target.pos, float32(damageRadius), dmg, dmgElement, false)
	}
}

func (s *EnetServer) hostTurretAOERegen(room *Room, turret *spawnedNpc, healPct, ammoPct float64) {
	r2 := turretAOERadius * turretAOERadius
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.avatarBase == 0 || target.isDead {
			continue
		}
		pos, ok := playerStreamPos(room, target.avatarBase)
		if !ok {
			pos = room.playerPos[target.avatarBase+1]
		}
		dx, dz := pos[0]-turret.pos[0], pos[2]-turret.pos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		changed := false
		if healPct > 0 && target.maxHP > 0 && target.hp < target.maxHP {
			target.hp += int32(float64(target.maxHP) * healPct)
			if target.hp > target.maxHP {
				target.hp = target.maxHP
			}
			changed = true
		}
		if changed {
			s.hostSendRPC(room, target.avatarBase, "RPC_RequestAllLifeAndShield", []any{
				map[int]any{0: int32(1), 1: target.avatarBase, 2: playerLifeTable(target.hp, target.maxHP, target.shield, target.maxShield)},
			})
		}
		if ammoPct > 0 && target.ammoEst < 360 {
			target.ammoEst += int(360 * ammoPct)
			if target.ammoEst > 360 {
				target.ammoEst = 360
			}
			s.hostSendRPC(room, target.avatarBase+3, "RPC_OnReceiveAmmos", []any{ammoPouchesForSession(target, true)})
		}
	}
}
