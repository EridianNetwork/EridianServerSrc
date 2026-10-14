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
	"eridian/internal/online"
	"eridian/internal/online/services"
	"eridian/internal/photon"
	"eridian/pkg/config"
	"eridian/pkg/runlog"
)

func main() {
	if f := runlog.Setup("borderlands"); f != nil {
		defer f.Close()
	}

	host := flag.String("host", "127.0.0.1", "host to listen on")
	onlinePort := flag.Int("online-port", 5056, "online server TCP port")
	photonPort := flag.Int("photon-port", 5055, "photon server UDP port")
	authPort := flag.Int("auth-port", 443, "auth server TLS port")
	maxConns := flag.Int("max-conns", 100, "maximum concurrent connections")
	dbPath := flag.String("db", "borderlands.db", "path to sqlite database")
	certFile := flag.String("cert", "cert.pem", "TLS certificate file")
	keyFile := flag.String("key", "key.pem", "TLS key file")
	local := flag.Bool("local", false, "use local network IP instead of 127.0.0.1")
	configPath := flag.String("config", "config/server.json", "path to server config JSON")
	debugPosPort := flag.Int("debugpos-port", 5099, "live player-position debug stream HTTP port for level_editor (0 disables)")
	rconPort := flag.Int("rcon-port", 5000, "visual RCON admin API HTTP port for ../rcon (0 disables)")
	perfLog := flag.Bool("perf", false, "log the periodic [PERF] tick-timing line every 15s (also toggleable at runtime with 'perf on'/'perf off')")
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
	var playerMeleeDmgRatio float64
	var fireBirdCooldownSec, berserkCooldownSec float64
	var turretCooldownShortSec, turretCooldownLongSec float64
	var hunterCooldownShortSec, hunterCooldownLongSec float64
	var pvpCfg config.PvPConfig
	var masterCfg config.MasterServerConfig
	rconPassword := ""
	if cfg, err := config.Load(*configPath); err != nil {
		log.Printf("[MAIN] config %s not loaded (%v). AFK-kick disabled", *configPath, err)
	} else {
		afkKickSeconds = cfg.Game.AFKKickSeconds
		unreliableMovementSync = cfg.Game.UnreliableMovementSync
		rconPassword = cfg.Rcon.Password
		tutorialFreeActions = cfg.Game.TutorialFreeActions
		npcDifficultyTiers = npcTiersFromConfig(cfg.Game.NpcDifficulty)
		npcPerLevelGrowth = cfg.Game.NpcPerLevelGrowth
		npcBossHPTiers = bossHPTiersFromConfig(cfg.Game.BossHPMultiplier)
		xpMultiplier = cfg.Game.XPMultiplier
		npcKillXP = cfg.Game.NpcKillXP
		playerMeleeDmgRatio = cfg.Game.PlayerMeleeDamageRatio
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

	// If -local flag is set, detect the machines local IP
	if *local {
		localIP := photon.GetLocalIP()
		log.Printf("[MAIN] -local flag set, using local network IP: %s", localIP)
		host = &localIP
	}

	// initialize database
	if err := db.Open(*dbPath); err != nil {
		log.Fatalf("[MAIN] database open: %v", err)
	}
	defer db.Close()

	if err := db.RunMigrations(); err != nil {
		log.Fatalf("[MAIN] database migrate: %v", err)
	}
	if err := db.MigrateCharacterAvatar(); err != nil {
		log.Fatalf("[MAIN] database migrate avatar: %v", err)
	}
	if err := db.MigratePasswordHashes(); err != nil {
		log.Fatalf("[MAIN] database hash passwords: %v", err)
	}
	if err := db.MigrateAccountSettings(); err != nil {
		log.Fatalf("[MAIN] database migrate account settings: %v", err)
	}
	if err := db.MigrateAccountTesterFlags(); err != nil {
		log.Fatalf("[MAIN] database migrate account tester flags: %v", err)
	}
	if err := db.MigratePvPStats(); err != nil {
		log.Fatalf("[MAIN] database migrate pvp stats: %v", err)
	}
	photon.BackfillWeaponExtras()

	log.Println("[MAIN] Borderlands Online Server starting...")
	log.Printf("[MAIN] Advertised address for clients: %s", *host)

	bindAddr := "0.0.0.0"

	// online server
	onlineSvr := online.NewServer(bindAddr, *onlinePort, *maxConns)
	onlineSvr.PhotonHost = *host
	onlineSvr.PhotonPort = *photonPort
	services.RegisterCharacterService(onlineSvr)
	services.RegisterInventoryService(onlineSvr)
	services.RegisterLobbyService(onlineSvr)
	services.RegisterChatService(onlineSvr)
	services.RegisterFriendService(onlineSvr)

	go func() {
		log.Printf("[MAIN] online server listening on %s:%d", bindAddr, *onlinePort)
		if err := onlineSvr.Serve(); err != nil {
			log.Fatalf("[MAIN] online server error: %v", err)
		}
	}()

	photonSvr := photon.NewServer(bindAddr, *photonPort)
	photonSvr.AdvertisedHost = *host
	photonSvr.GameHandler = photon.DefaultGameHandlers()
	photonSvr.AFKKickSeconds = afkKickSeconds
	photonSvr.UnreliableMovementSync = unreliableMovementSync
	photonSvr.TutorialFreeActions = tutorialFreeActions
	photonSvr.NpcDifficultyTiers = npcDifficultyTiers
	photonSvr.NpcPerLevelGrowth = float32(npcPerLevelGrowth)
	photonSvr.NpcBossHPTiers = npcBossHPTiers
	photonSvr.XPMultiplier = xpMultiplier
	photonSvr.NpcKillXPBase = npcKillXP.Base
	photonSvr.NpcKillXPBadassMult = npcKillXP.BadassMultiplier
	photonSvr.NpcKillXPBossMult = npcKillXP.BossMultiplier
	photonSvr.PlayerMeleeDamageRatio = playerMeleeDmgRatio
	photonSvr.FireBirdCooldownSeconds = fireBirdCooldownSec
	photonSvr.BerserkCooldownSeconds = berserkCooldownSec
	photonSvr.TurretCooldownShortSeconds = turretCooldownShortSec
	photonSvr.TurretCooldownLongSeconds = turretCooldownLongSec
	photonSvr.HunterCooldownShortSeconds = hunterCooldownShortSec
	photonSvr.HunterCooldownLongSeconds = hunterCooldownLongSec
	photonSvr.PvPMatchDurationSeconds = pvpCfg.MatchDurationSeconds
	photonSvr.PvPKillTarget = int32(pvpCfg.KillTarget)
	photonSvr.ClaptrapRoundSeconds = pvpCfg.Claptrap.RoundSeconds
	photonSvr.ClaptrapTargetScore = int32(pvpCfg.Claptrap.TargetScore)
	photonSvr.MaxPlayersTotal = masterCfg.MaxPlayers
	photonSvr.PerfLog = *perfLog
	photonSvr.ServerVersionID = serverVersionID

	go func() {
		log.Printf("[MAIN] photon server listening on %s:%d (advertised as %s:%d)", bindAddr, *photonPort, *host, *photonPort)
		if err := photonSvr.Serve(); err != nil {
			log.Fatalf("[MAIN] photon server error: %v", err)
		}
	}()

	photonSvr.StartDebugPosServer("127.0.0.1", *debugPosPort)

	photonSvr.StartRconServer("127.0.0.1", *rconPort, rconPassword)

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
		reporter, err = master.NewReporter(masterCfg.ReportAddress, masterCfg.MagicValue, *photonPort, interval, func() (string, int, int) {
			return name, photonSvr.LivePlayerCount(), maxPlayers
		})
		if err != nil {
			log.Printf("[MAIN] master reporter not started (%v), this instance won't appear in the launcher's server list", err)
		} else {
			log.Printf("[MAIN] reporting to master %s as %q", masterCfg.ReportAddress, name)
			go reporter.Run()
		}
	}

	_ = authPort
	_ = certFile
	_ = keyFile

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("[MAIN] received %v, shutting down...", sig)

	if reporter != nil {
		reporter.Stop()
	}
	onlineSvr.Shutdown()
	photonSvr.Shutdown()

	log.Println("[MAIN] all servers stopped")
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
