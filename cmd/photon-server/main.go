package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eridian/internal/db"
	"eridian/internal/master"
	"eridian/internal/photon"
	"eridian/pkg/config"
	"eridian/pkg/runlog"
)

func main() {
	if f := runlog.Setup("photon-server"); f != nil {
		defer f.Close()
	}

	host := flag.String("host", "0.0.0.0", "host to listen on")
	port := flag.Int("port", 5056, "port to listen on")
	dbPath := flag.String("db", "borderlands.db", "path to sqlite database")
	configPath := flag.String("config", "config/server.json", "path to server config JSON")
	flag.Parse()

	afkKickSeconds := 0
	unreliableMovementSync := false
	tutorialFreeActions := false
	serverVersionID := ""
	var npcDifficultyTiers map[int32]photon.NpcTierMult
	var npcPerLevelGrowth float64
	var npcBossHPTiers map[int32]float32
	var xpMultiplier float64
	var npcKillXP config.NpcKillXPConfig
	var fireBirdCooldownSec, berserkCooldownSec float64
	var turretCooldownShortSec, turretCooldownLongSec float64
	var hunterCooldownShortSec, hunterCooldownLongSec float64
	var pvpCfg config.PvPConfig
	var masterCfg config.MasterServerConfig
	if cfg, err := config.Load(*configPath); err != nil {
		log.Printf("[PHOTON] config %s not loaded (%v). AFK-kick disabled", *configPath, err)
	} else {
		afkKickSeconds = cfg.Game.AFKKickSeconds
		unreliableMovementSync = cfg.Game.UnreliableMovementSync
		tutorialFreeActions = cfg.Game.TutorialFreeActions
		npcDifficultyTiers = npcTiersFromConfig(cfg.Game.NpcDifficulty)
		npcPerLevelGrowth = cfg.Game.NpcPerLevelGrowth
		npcBossHPTiers = bossHPTiersFromConfig(cfg.Game.BossHPMultiplier)
		xpMultiplier = cfg.Game.XPMultiplier
		npcKillXP = cfg.Game.NpcKillXP
		fireBirdCooldownSec = cfg.Game.FireBirdCooldownSeconds
		berserkCooldownSec = cfg.Game.BerserkCooldownSeconds
		turretCooldownShortSec = cfg.Game.TurretCooldownShortSeconds
		turretCooldownLongSec = cfg.Game.TurretCooldownLongSeconds
		hunterCooldownShortSec = cfg.Game.HunterCooldownShortSeconds
		hunterCooldownLongSec = cfg.Game.HunterCooldownLongSeconds
		pvpCfg = cfg.Game.PvP
		serverVersionID = cfg.Game.Version
		if cfg.Game.BuildHash != "" {
			serverVersionID += " (" + cfg.Game.BuildHash + ")"
		}
		masterCfg = cfg.MasterServer
	}

	db.NewAccountCanPlayFn = config.LiveNewAccountCanPlay

	if err := db.Open(*dbPath); err != nil {
		log.Fatalf("[PHOTON] database open: %v", err)
	}
	defer db.Close()

	if err := db.RunMigrations(); err != nil {
		log.Fatalf("[PHOTON] database migrate: %v", err)
	}
	if err := db.MigrateCharacterAvatar(); err != nil {
		log.Fatalf("[PHOTON] database migrate avatar: %v", err)
	}
	if err := db.MigrateAccountSettings(); err != nil {
		log.Fatalf("[PHOTON] database migrate account settings: %v", err)
	}
	if err := db.MigrateAccountTesterFlags(); err != nil {
		log.Fatalf("[PHOTON] database migrate account tester flags: %v", err)
	}
	if err := db.MigratePvPStats(); err != nil {
		log.Fatalf("[PHOTON] database migrate pvp stats: %v", err)
	}

	svr := photon.NewServer(*host, *port)
	svr.GameHandler = photon.DefaultGameHandlers()
	svr.AFKKickSeconds = afkKickSeconds
	svr.UnreliableMovementSync = unreliableMovementSync
	svr.TutorialFreeActions = tutorialFreeActions
	svr.NpcDifficultyTiers = npcDifficultyTiers
	svr.NpcPerLevelGrowth = float32(npcPerLevelGrowth)
	svr.NpcBossHPTiers = npcBossHPTiers
	svr.XPMultiplier = xpMultiplier
	svr.NpcKillXPBase = npcKillXP.Base
	svr.NpcKillXPBadassMult = npcKillXP.BadassMultiplier
	svr.NpcKillXPBossMult = npcKillXP.BossMultiplier
	svr.ServerVersionID = serverVersionID
	svr.FireBirdCooldownSeconds = fireBirdCooldownSec
	svr.BerserkCooldownSeconds = berserkCooldownSec
	svr.TurretCooldownShortSeconds = turretCooldownShortSec
	svr.TurretCooldownLongSeconds = turretCooldownLongSec
	svr.HunterCooldownShortSeconds = hunterCooldownShortSec
	svr.HunterCooldownLongSeconds = hunterCooldownLongSec
	svr.PvPMatchDurationSeconds = pvpCfg.MatchDurationSeconds
	svr.PvPKillTarget = int32(pvpCfg.KillTarget)
	svr.ClaptrapRoundSeconds = pvpCfg.Claptrap.RoundSeconds
	svr.ClaptrapTargetScore = int32(pvpCfg.Claptrap.TargetScore)
	svr.MaxPlayersTotal = masterCfg.MaxPlayers

	var reporter *master.Reporter
	if masterCfg.ReportEnabled {
		name := masterCfg.ServerName
		if name == "" {
			name = "Borderlands Online Server"
		}
		maxPlayers := masterCfg.MaxPlayers
		if maxPlayers <= 0 {
			maxPlayers = 100
		}
		interval := time.Duration(masterCfg.HeartbeatIntervalSeconds) * time.Second
		var err error
		reporter, err = master.NewReporter(masterCfg.ReportAddress, masterCfg.MagicValue, *port, interval, func() (string, int, int) {
			return name, svr.LivePlayerCount(), maxPlayers
		})
		if err != nil {
			log.Printf("[PHOTON] master reporter not started (%v), this instance won't appear in the launcher's server list", err)
		} else {
			log.Printf("[PHOTON] reporting to master %s as %q", masterCfg.ReportAddress, name)
			go reporter.Run()
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("[PHOTON] received %v, shutting down...", sig)
		if reporter != nil {
			reporter.Stop()
		}
		svr.Shutdown()
	}()

	log.Printf("[PHOTON] starting photon server on %s:%d", *host, *port)
	if err := svr.Serve(); err != nil {
		log.Fatalf("[PHOTON] server error: %v", err)
	}
}

func npcTiersFromConfig(cfg config.NpcDifficultyConfig) map[int32]photon.NpcTierMult {
	if cfg.Normal == (config.NpcDifficultyTier{}) && cfg.Hard == (config.NpcDifficultyTier{}) && cfg.Hell == (config.NpcDifficultyTier{}) {
		return nil
	}
	return map[int32]photon.NpcTierMult{
		0: {HP: float32(cfg.Normal.HPMultiplier), Dmg: float32(cfg.Normal.DamageMultiplier)},
		1: {HP: float32(cfg.Hard.HPMultiplier), Dmg: float32(cfg.Hard.DamageMultiplier)},
		2: {HP: float32(cfg.Hell.HPMultiplier), Dmg: float32(cfg.Hell.DamageMultiplier)},
	}
}

func bossHPTiersFromConfig(cfg config.BossHPMultiplierConfig) map[int32]float32 {
	if cfg.Hard == 0 && cfg.Hell == 0 {
		return nil
	}
	out := map[int32]float32{}
	if cfg.Hard != 0 {
		out[1] = float32(cfg.Hard)
	}
	if cfg.Hell != 0 {
		out[2] = float32(cfg.Hell)
	}
	return out
}
