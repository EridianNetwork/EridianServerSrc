package photon

import (
	"encoding/xml"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type npcPoisonPhaseWeights struct {
	Dash, Spit, Crush float64
}

type npcKnoxxPhaseWeights struct {
	Melee, Railgun, Jet, Charge, Mortar, LaserSweep float64
}

type npcConfig struct {
	HealthPoint  int32
	MeleeDamage  int32
	MeleeRange   float64
	WalkSpeed    float64
	CombatSpeed  float64
	RunSpeed     float64
	DetectionRng float64

	ChargeSpeed float64

	ChargeDamage      int32
	ChargeCheckRadius float64

	JumpDamage int32

	ChargeSubType int32
	JumpSubType   int32
	MeleeSubTypes []int32

	PoisonDashWeight, PoisonDashCooldown, PoisonDashTriggerDist, PoisonDashDist, PoisonDashAngle float64
	PoisonDashDamage                                                                             int32
	PoisonSpitWeight, PoisonSpitCooldown, PoisonSpitDist, PoisonSpitAngle                        float64
	PoisonSpitDamage                                                                             int32
	PoisonCrushWeight, PoisonCrushCooldown, PoisonCrushDist                                      float64
	PoisonCrushDamage                                                                            int32
	PoisonPhaseWeights                                                                           []npcPoisonPhaseWeights

	FireRange float64

	BestAttackDistanceMax float64
	BestAttackDistanceMin float64

	SelectTargetTimer float64

	BurstSingleMin      float64
	BurstSingleMax      float64
	BurstChainedFireMin float64
	BurstChainedFireMax float64
	BurstChainedIdleMin float64
	BurstChainedIdleMax float64

	InitialCoverChance    float64
	DangerCoverChance     float64
	SeekCoverDistance     float64
	LeaveCoverDistance    float64
	CoverIdleDangerMin    float64
	CoverIdleDangerMax    float64
	CoverPeakOutCombatMin float64
	CoverPeakOutCombatMax float64

	SpacingRangeMin float64
	SpacingRangeMax float64

	PatrolRadius float64
	StayIdleMin  float64
	StayIdleMax  float64

	TurretDamage       int32
	TurretDamageRadius float64
	TurretFireInterval float64

	JumpWeight      float64
	JumpDistanceMin float64
	JumpDistanceMax float64
	JumpCooldown    float64

	ChargeWeight               float64
	ChargeDistanceMin          float64
	ChargeDistanceMax          float64
	ChargeDistanceBehindTarget float64

	MeleeWeight float64

	RailgunWeight      float64
	PlasmaBallDamage   float64
	PlasmaBallSpeed    float64
	PlasmaBallRadius   float64
	PlasmaBallDuration float64

	JetWeight       float64
	JetAttackDamage int32
	JetDistanceMin  float64
	JetDistanceMax  float64
	JetDamageRadius float64
	JetFlyingSpeed  float64
	JetIdleTime     float64

	MortarWeight      float64
	MortarShellDamage int32
	MortarShellAmount int32
	MortarShellRadius float64
	MortarShellSpeed  float64

	LaserSweepDamage int32

	KnoxxPhaseWeights []npcKnoxxPhaseWeights

	SpitWeight      float64
	SpitDistanceMin float64
	SpitDistanceMax float64
	SpitIdleTime    float64

	ThrowAxeDistanceMin float64
	ThrowAxeDistanceMax float64

	NormalDeathAnimCount int32

	HeadShotDeathAnimCount int32

	SuicideDistance float64

	BlowUpDamage int32
	BlowUpRadius float64

	ThrowGrenadeChance float64

	WarcryDamage int32
	WarcryTime   float64

	StunDamage   int32
	StunTime     float64
	StunCooldown float64

	WebTrapWeight   float64
	WebTrapCooldown float64

	FuryHealthPoint float64
}

type xmlSettings struct {
	Type              string  `xml:"http"`
	AttackDamage      int32   `xml:"m_attackDamage"`
	AttackRange       float64 `xml:"m_attackRange"`
	DistanceToTarget  float64 `xml:"m_distanceToTarget"`
	WalkSpeed         float64 `xml:"m_walkSpeed"`
	CombatSpeed       float64 `xml:"m_walkCombatSpeed"`
	RunSpeed          float64 `xml:"m_runSpeed"`
	ChargeSpeed       float64 `xml:"m_chargeSpeed"`
	CheckRadius       float64 `xml:"m_checkRadius"`
	HealthPoint       int32   `xml:"m_healthPoint"`
	VisualRange       float64 `xml:"m_visualDetectionRange"`
	FireRange         float64 `xml:"m_fireRange"`
	BestAttackDistMax float64 `xml:"m_bestAttackDistanceMax"`
	BestAttackDistMin float64 `xml:"m_bestAttackDistanceMin"`
	SelectTargetTimer float64 `xml:"m_selectTargetTimer"`

	BurstSingleMin      float64 `xml:"m_burstSingle_FireTimeMin"`
	BurstSingleMax      float64 `xml:"m_burstSingle_FireTimeMax"`
	BurstChainedFireMin float64 `xml:"m_burstChained_FireTimeMin"`
	BurstChainedFireMax float64 `xml:"m_burstChained_FireTimeMax"`
	BurstChainedIdleMin float64 `xml:"m_burstChained_IdleTimeMin"`
	BurstChainedIdleMax float64 `xml:"m_burstChained_IdleTimeMax"`

	InitialCoverChance    float64 `xml:"m_initialCoverChance"`
	DangerCoverChance     float64 `xml:"m_dangerCoverChance"`
	SeekCoverDistance     float64 `xml:"m_seekCoverDistance"`
	LeaveCoverDistance    float64 `xml:"m_leaveCoverDistance"`
	CoverIdleDangerMin    float64 `xml:"m_coverIdleCombatDangerTimeMin"`
	CoverIdleDangerMax    float64 `xml:"m_coverIdleCombatDangerTimeMax"`
	CoverPeakOutCombatMin float64 `xml:"m_coverPeakOutCombatTimeMin"`
	CoverPeakOutCombatMax float64 `xml:"m_coverPeakOutCombatTimeMax"`

	SpacingRangeMin float64 `xml:"m_spacingRangeMin"`
	SpacingRangeMax float64 `xml:"m_spacingRangeMax"`

	PatrolRadius float64 `xml:"m_patrolRadius"`
	StayIdleMin  float64 `xml:"m_stayIdleTimeMin"`
	StayIdleMax  float64 `xml:"m_stayIdleTimeMax"`

	TurretDamage       int32   `xml:"m_damage"`
	TurretDamageRadius float64 `xml:"m_damageRadius"`
	TurretFireInterval float64 `xml:"m_fireInterval"`

	AttackChanceWeight         float64 `xml:"m_attackChanceWeight"`
	JumpDistanceMin            float64 `xml:"m_jumpDistanceMin"`
	JumpDistanceMax            float64 `xml:"m_jumpDistanceMax"`
	ChargeDistanceMin          float64 `xml:"m_chargeDistanceMin"`
	ChargeDistanceMax          float64 `xml:"m_chargeDistanceMax"`
	ChargeDistanceBehindTarget float64 `xml:"m_distanceBehindTarget"`
	SpitDistanceMin            float64 `xml:"m_attackDistanceMin"`
	SpitDistanceMax            float64 `xml:"m_attackDistanceMax"`
	SpitIdleTime               float64 `xml:"m_attackIdleTime"`

	ThrowAxeDistanceMin float64 `xml:"m_throwAxeDistanceMin"`
	ThrowAxeDistanceMax float64 `xml:"m_throwAxeDistanceMax"`

	HammerJumpCooldown float64 `xml:"m_hammerJumpCooltime"`

	AttackSubType     string   `xml:"m_attackSubType"`
	AttackSubTypeList []string `xml:"m_attackSubTypeList>m_attackSubType"`

	CoolDownTime          float64 `xml:"m_coolDownTime"`
	TriggerAttackDistance float64 `xml:"m_triggerAttackDistance"`
	PoisonAttackAngle     float64 `xml:"m_attackAngle"`
	PoisonAttackDistance  float64 `xml:"m_attackDistance"`
	DamagePoint           int32   `xml:"m_damagePoint"`
	PhaseID               int32   `xml:"m_phaseId"`
	ExitPhaseHP           float64 `xml:"m_exitPhase_hp"`
	PoisonDashWeightXML   float64 `xml:"m_poisonDashWeight"`
	PoisonSpitWeightXML   float64 `xml:"m_poisonSpitWeight"`
	PoisonCrushWeightXML  float64 `xml:"m_poisonCrushWeight"`

	ThrowGrenadeChance float64 `xml:"m_throwGrenadeChance"`

	DamageAmount int32   `xml:"m_damageAmount"`
	EffectTime   float64 `xml:"m_time"`

	StartFuryHealthPoint float64 `xml:"m_startFuryHealthPoint"`

	PlasmaBallDamageXML   float64 `xml:"m_plasmaBallDamage"`
	PlasmaBallSpeedXML    float64 `xml:"m_plasmaBallSpeed"`
	PlasmaBallRadiusXML   float64 `xml:"m_plasmaBallDamageRadius"`
	PlasmaBallDurationXML float64 `xml:"m_plasmaBallDamageDuration"`
	JetAttackDamageXML    int32   `xml:"m_jetAttackDamage"`
	JetFlyingSpeedXML     float64 `xml:"m_jetFlyingSpeed"`
	JetAttackIdleTimeXML  float64 `xml:"m_jetAttackIdleTime"`
	ShellDamageXML        int32   `xml:"m_shellDamage"`
	ShellAmountXML        int32   `xml:"m_shellAmount"`
	ShellDamageRadiusXML  float64 `xml:"m_shellDamageRadius"`
	ShellFlyingSpeedXML   float64 `xml:"m_shellFlyingSpeed"`
	LaserDamageXML        int32   `xml:"m_laserDamage"`

	MeleeAttackWeightXML   float64 `xml:"m_meleeAttackWeight"`
	RailgunAttackWeightXML float64 `xml:"m_railgunAttackWeight"`
	JetAttackWeightXML     float64 `xml:"m_jetAttackWeight"`
	ChargeAttackWeightXML  float64 `xml:"m_chargeAttackWeight"`
	MortarAttackWeightXML  float64 `xml:"m_mortarAttackWeight"`
	LaserSweepWeightXML    float64 `xml:"m_laserSweepWeight"`
}

type xmlAnimationManagerSettings struct {
	NormalDeathAnimCount   int32 `xml:"m_normalDeathAnimCount"`
	HeadShotDeathAnimCount int32 `xml:"m_headShotDeathAnimCount"`
}

type xmlAnimationManagerSetup struct {
	Settings xmlAnimationManagerSettings `xml:"m_settings"`
}

type xmlTaskManagerSetup struct {
	EntityType string        `xml:"m_entityType"`
	Settings   []xmlSettings `xml:"m_settings>m_taskSettings"`
}

type xmlBrainSetup struct {
	FsmType       string        `xml:"m_fsmType"`
	Settings      []xmlSettings `xml:"m_BHVPropertySettingsList>m_BHVPropertySettings"`
	PhaseSettings []xmlSettings `xml:"m_BHVPropertySettingsPhaseList>m_BHVPropertySettingsPhase"`
}

var (
	npcConfigsOnce sync.Once
	npcConfigs     map[string]*npcConfig
)

func stripCHR(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "CHR_")
}

var attackSubTypeOrdinals = map[string]int32{
	"MeleeGeneric": 21,
	"MeleeBite":    23,
	"MeleePaw":     24,
	"MeleeAxe":     25,
	"MeleeClaw":    26,
	"MeleeHunter":  27,
}

func attackSubTypeOrdinal(name string) int32 {
	if o, ok := attackSubTypeOrdinals[name]; ok {
		return o
	}
	return attackSubTypeOrdinals["MeleeGeneric"]
}

func loadNpcConfigs() {
	npcConfigsOnce.Do(func() {
		npcConfigs = map[string]*npcConfig{}
		files, _ := filepath.Glob("data/npc_configs/Config_*_TaskManager.xml")
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var tm xmlTaskManagerSetup
			if err := xml.Unmarshal(raw, &tm); err != nil {
				log.Printf("[NPC] bad TaskManager config %s: %v", f, err)
				continue
			}
			if stripCHR(tm.EntityType) == "" {
				continue
			}
			kit := stripCHR(tm.EntityType)
			cfg := &npcConfig{}
			for _, s := range tm.Settings {
				switch s.Type {
				case "BHVMeleeAttackSettings":
					cfg.MeleeDamage = s.AttackDamage
					cfg.MeleeRange = s.AttackRange
					for _, st := range s.AttackSubTypeList {
						cfg.MeleeSubTypes = append(cfg.MeleeSubTypes, attackSubTypeOrdinal(st))
					}
				case "BHVChargeAttackSettings":
					cfg.ChargeDamage = s.AttackDamage
					cfg.ChargeCheckRadius = s.CheckRadius
					cfg.ChargeSubType = attackSubTypeOrdinal(s.AttackSubType)
				case "BHVJumpAttackSettings":
					cfg.JumpDamage = s.AttackDamage
					cfg.JumpSubType = attackSubTypeOrdinal(s.AttackSubType)
				case "BHVNavAgentSettings":
					cfg.WalkSpeed = s.WalkSpeed
					cfg.CombatSpeed = s.CombatSpeed
					cfg.RunSpeed = s.RunSpeed
					cfg.ChargeSpeed = s.ChargeSpeed
				case "BHVFireStyleSettings":
					cfg.BurstSingleMin = s.BurstSingleMin
					cfg.BurstSingleMax = s.BurstSingleMax
					cfg.BurstChainedFireMin = s.BurstChainedFireMin
					cfg.BurstChainedFireMax = s.BurstChainedFireMax
					cfg.BurstChainedIdleMin = s.BurstChainedIdleMin
					cfg.BurstChainedIdleMax = s.BurstChainedIdleMax
				case "BHVTaskTurretRocketSetting":
					cfg.TurretDamage = s.TurretDamage
					cfg.TurretDamageRadius = s.TurretDamageRadius
					cfg.TurretFireInterval = s.TurretFireInterval
				case "BHVBlowUpAttackSettings":
					cfg.BlowUpDamage = s.AttackDamage
					cfg.BlowUpRadius = s.AttackRange
				case "BHVMortarAttackSettings":
					cfg.MortarShellDamage = s.ShellDamageXML
					cfg.MortarShellAmount = s.ShellAmountXML
					cfg.MortarShellRadius = s.ShellDamageRadiusXML
					cfg.MortarShellSpeed = s.ShellFlyingSpeedXML
				case "BHVLaserSweepSetting":
					cfg.LaserSweepDamage = s.LaserDamageXML
				}
			}
			if raw, err := os.ReadFile(strings.Replace(f, "_TaskManager.xml", "_Brain.xml", 1)); err == nil {
				var br xmlBrainSetup
				if xml.Unmarshal(raw, &br) == nil {
					for _, s := range br.Settings {
						switch s.Type {
						case "BHVFSMSettingsBasic":
							cfg.HealthPoint = s.HealthPoint
						case "BHVFSMSettingsDetection":
							cfg.DetectionRng = s.VisualRange
						case "BHVFSMSettingsRangeAttack":
							cfg.FireRange = s.FireRange
							cfg.BestAttackDistanceMax = s.BestAttackDistMax
							cfg.BestAttackDistanceMin = s.BestAttackDistMin
						case "BHVFSMSettingsTimers":
							cfg.SelectTargetTimer = s.SelectTargetTimer
						case "BHVFSMSettingsTakeCover":
							cfg.InitialCoverChance = s.InitialCoverChance
							cfg.DangerCoverChance = s.DangerCoverChance
						case "BHVFSMSettingsCover":
							cfg.SeekCoverDistance = s.SeekCoverDistance
							cfg.LeaveCoverDistance = s.LeaveCoverDistance
							cfg.CoverIdleDangerMin = s.CoverIdleDangerMin
							cfg.CoverIdleDangerMax = s.CoverIdleDangerMax
							cfg.CoverPeakOutCombatMin = s.CoverPeakOutCombatMin
							cfg.CoverPeakOutCombatMax = s.CoverPeakOutCombatMax
						case "BHVFSMSettingsSpacing":
							cfg.SpacingRangeMin = s.SpacingRangeMin
							cfg.SpacingRangeMax = s.SpacingRangeMax
						case "BHVFSMSettingsPatrol":
							cfg.PatrolRadius = s.PatrolRadius
							cfg.StayIdleMin = s.StayIdleMin
							cfg.StayIdleMax = s.StayIdleMax
						case "BHVFSMSettingsJumpAttack":
							cfg.JumpWeight = s.AttackChanceWeight
							cfg.JumpDistanceMin = s.JumpDistanceMin
							cfg.JumpDistanceMax = s.JumpDistanceMax
						case "BHVFSMSettingsChargeAttack":
							cfg.ChargeWeight = s.AttackChanceWeight
							cfg.ChargeDistanceMin = s.ChargeDistanceMin
							cfg.ChargeDistanceMax = s.ChargeDistanceMax
							cfg.ChargeDistanceBehindTarget = s.ChargeDistanceBehindTarget
						case "BHVFSMSettingsMeleeAttack":
							cfg.MeleeWeight = s.AttackChanceWeight
						case "BHVFSMSettingsRailGunAttack":
							cfg.RailgunWeight = s.AttackChanceWeight
							cfg.PlasmaBallDamage = s.PlasmaBallDamageXML
							cfg.PlasmaBallSpeed = s.PlasmaBallSpeedXML
							cfg.PlasmaBallRadius = s.PlasmaBallRadiusXML
							cfg.PlasmaBallDuration = s.PlasmaBallDurationXML
						case "BHVFSMSettingsJetAttack":
							cfg.JetWeight = s.AttackChanceWeight
							cfg.JetAttackDamage = s.JetAttackDamageXML
							cfg.JetDistanceMin = s.SpitDistanceMin
							cfg.JetDistanceMax = s.SpitDistanceMax
							cfg.JetDamageRadius = s.TurretDamageRadius
							cfg.JetFlyingSpeed = s.JetFlyingSpeedXML
							cfg.JetIdleTime = s.JetAttackIdleTimeXML
						case "BHVFSMSettingsMortarAttack":
							cfg.MortarWeight = s.AttackChanceWeight
						case "BHVFSMSettingsSpitAttack":
							cfg.SpitWeight = s.AttackChanceWeight
							cfg.SpitDistanceMin = s.SpitDistanceMin
							cfg.SpitDistanceMax = s.SpitDistanceMax
							cfg.SpitIdleTime = s.SpitIdleTime
						case "BHVFSMSettingsThrowAxe":
							cfg.ThrowAxeDistanceMin = s.ThrowAxeDistanceMin
							cfg.ThrowAxeDistanceMax = s.ThrowAxeDistanceMax
						case "BHVFSMSettingsSuicide":
							cfg.SuicideDistance = s.DistanceToTarget
						case "BHVFSMSettingsPoisonDash":
							cfg.PoisonDashWeight = s.AttackChanceWeight
							cfg.PoisonDashCooldown = s.CoolDownTime
							cfg.PoisonDashTriggerDist = s.TriggerAttackDistance
							cfg.PoisonDashDist = s.PoisonAttackDistance
							cfg.PoisonDashAngle = s.PoisonAttackAngle
							cfg.PoisonDashDamage = s.DamagePoint
						case "BHVFSMSettingsPoisonSpit":
							cfg.PoisonSpitWeight = s.AttackChanceWeight
							cfg.PoisonSpitCooldown = s.CoolDownTime
							cfg.PoisonSpitDist = s.PoisonAttackDistance
							cfg.PoisonSpitAngle = s.PoisonAttackAngle
							cfg.PoisonSpitDamage = s.DamagePoint
						case "BHVFSMSettingsPoisonCrush":
							cfg.PoisonCrushWeight = s.AttackChanceWeight
							cfg.PoisonCrushCooldown = s.CoolDownTime
							cfg.PoisonCrushDist = s.PoisonAttackDistance
							cfg.PoisonCrushDamage = s.DamagePoint
						case "BHVFSMSettingsThrowGrenade":
							cfg.ThrowGrenadeChance = s.ThrowGrenadeChance
						case "BHVFSMSettingsWarcry":
							cfg.WarcryDamage = s.DamageAmount
							cfg.WarcryTime = s.EffectTime
						case "BHVFSMSettingsStun":
							cfg.StunDamage = s.DamageAmount
							cfg.StunTime = s.EffectTime
							cfg.StunCooldown = s.CoolDownTime
						case "BHVFSMSettingsWebTrap":
							cfg.WebTrapWeight = s.AttackChanceWeight
							cfg.WebTrapCooldown = s.CoolDownTime
						case "BHVFSMSettingsFury":
							cfg.FuryHealthPoint = s.StartFuryHealthPoint
						}
					}
					for _, s := range br.PhaseSettings {
						if s.HammerJumpCooldown > 0 && cfg.JumpCooldown == 0 {
							cfg.JumpCooldown = s.HammerJumpCooldown
						}
						if s.PhaseID > 0 {
							cfg.PoisonPhaseWeights = append(cfg.PoisonPhaseWeights, npcPoisonPhaseWeights{
								Dash: s.PoisonDashWeightXML, Spit: s.PoisonSpitWeightXML, Crush: s.PoisonCrushWeightXML,
							})
							cfg.KnoxxPhaseWeights = append(cfg.KnoxxPhaseWeights, npcKnoxxPhaseWeights{
								Melee: s.MeleeAttackWeightXML, Railgun: s.RailgunAttackWeightXML, Jet: s.JetAttackWeightXML,
								Charge: s.ChargeAttackWeightXML, Mortar: s.MortarAttackWeightXML, LaserSweep: s.LaserSweepWeightXML,
							})
						}
					}
				}
			}
			if raw, err := os.ReadFile(strings.Replace(f, "_TaskManager.xml", "_AnimationManager.xml", 1)); err == nil {
				var am xmlAnimationManagerSetup
				if xml.Unmarshal(raw, &am) == nil {
					cfg.NormalDeathAnimCount = am.Settings.NormalDeathAnimCount
					cfg.HeadShotDeathAnimCount = am.Settings.HeadShotDeathAnimCount
				}
			}
			if cfg.MeleeWeight <= 0 {
				cfg.MeleeWeight = 0.8
			}
			npcConfigs[kit] = cfg
		}
		if len(npcConfigs) > 0 {
			log.Printf("[NPC] loaded %d NPC configs from data/npc_configs", len(npcConfigs))
		}
	})
}

func npcConfigFor(kit string) *npcConfig {
	loadNpcConfigs()
	return npcConfigs[kit]
}

func npcPoisonPhaseWeightsFor(cfg *npcConfig, phase int) (dash, spit, crush float64) {
	if cfg == nil {
		return 0, 0, 0
	}
	if phase >= 1 && phase <= len(cfg.PoisonPhaseWeights) {
		w := cfg.PoisonPhaseWeights[phase-1]
		return w.Dash, w.Spit, w.Crush
	}
	return cfg.PoisonDashWeight, cfg.PoisonSpitWeight, cfg.PoisonCrushWeight
}

func npcKnoxxPhaseWeightsFor(cfg *npcConfig, phase int) (melee, railgun, jet, charge, mortar, laserSweep float64) {
	if cfg == nil {
		return 0, 0, 0, 0, 0, 0
	}
	if phase >= 1 && phase <= len(cfg.KnoxxPhaseWeights) {
		w := cfg.KnoxxPhaseWeights[phase-1]
		return w.Melee, w.Railgun, w.Jet, w.Charge, w.Mortar, w.LaserSweep
	}
	return cfg.MeleeWeight, cfg.RailgunWeight, cfg.JetWeight, cfg.ChargeWeight, cfg.MortarWeight, 0
}
