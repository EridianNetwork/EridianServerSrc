package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
)

type Config struct {
	AuthServer   AuthServerConfig   `json:"auth_server"`
	OnlineServer OnlineServerConfig `json:"online_server"`
	PhotonServer PhotonServerConfig `json:"photon_server"`
	MasterServer MasterServerConfig `json:"master_server"`
	Database     DatabaseConfig     `json:"database"`
	Game         GameConfig         `json:"game"`
	Rcon         RconConfig         `json:"rcon"`
}

type RconConfig struct {
	Password string `json:"password"`
}

type AuthServerConfig struct {
	Host      string `json:"host"`
	HTTPPort  int    `json:"http_port"`
	HTTPSPort int    `json:"https_port"`
}

type OnlineServerConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	MaxClients int    `json:"max_clients"`
}

type PhotonServerConfig struct {
	Host    string `json:"host"`
	TCPPort int    `json:"tcp_port"`
	UDPPort int    `json:"udp_port"`
	WSPort  int    `json:"ws_port"`
}

type DatabaseConfig struct {
	Path string `json:"path"`
}

type MasterServerConfig struct {
	ListenHost    string `json:"listen_host"`
	ListenPort    int    `json:"listen_port"`
	ReportEnabled bool   `json:"report_enabled"`
	ReportAddress string `json:"report_address"`
	MagicValue    string `json:"magic_value"`
	ServerName    string `json:"server_name"`
	MaxPlayers    int    `json:"max_players"`
	// HeartbeatIntervalSeconds needs to stay below StaleTimeoutSeconds (roughly 1/3 or less) orthe
	// live server will flick in and out of the launcher's list
	HeartbeatIntervalSeconds int `json:"heartbeat_interval_seconds"`
	// StaleTimeoutSeconds (master-side only read by master-server) is how long a registered
	// server can go without a fresh heartbeat before the master wll delist it, anything less than 0 will fall back to
	// internal/master's defaultStaleServerTimeout of 90s
	StaleTimeoutSeconds int `json:"stale_timeout_seconds"`
}

type GameConfig struct {
	Version           string `json:"version"`
	BuildHash         string `json:"build_hash"`
	MaxPlayersPerRoom int    `json:"max_players_per_room"`
	EnablePvP         bool   `json:"enable_pvp"`
	// we have afk kicking here, but i dont think its tested yet
	AFKKickSeconds         int  `json:"afk_kick_seconds"`
	UnreliableMovementSync bool `json:"unreliable_movement_sync"`
	TutorialFreeActions    bool `json:"tutorial_free_actions"`
	// control here over vrious npc config which we of course need to be using as much as possible
	NpcDifficulty     NpcDifficultyConfig    `json:"npc_difficulty"`
	NpcPerLevelGrowth float64                `json:"npc_per_level_growth"`
	BossHPMultiplier  BossHPMultiplierConfig `json:"boss_hp_multiplier"`
	// XPMultiplier pretty self explanatory
	XPMultiplier float64 `json:"xp_multiplier"`
	// NpcKillXP does pretty much again a self explanatory function
	NpcKillXP NpcKillXPConfig `json:"npc_kill_xp"`
	// PlayerMeleeDamageRatio melee scaling
	PlayerMeleeDamageRatio     float64 `json:"player_melee_damage_ratio"`
	FireBirdCooldownSeconds    float64 `json:"fire_bird_cooldown_seconds"`
	BerserkCooldownSeconds     float64 `json:"berserk_cooldown_seconds"`
	TurretCooldownShortSeconds float64 `json:"turret_cooldown_short_seconds"`
	TurretCooldownLongSeconds  float64 `json:"turret_cooldown_long_seconds"`
	HunterCooldownShortSeconds float64 `json:"hunter_cooldown_short_seconds"`
	HunterCooldownLongSeconds  float64 `json:"hunter_cooldown_long_seconds"`
	// Useful for banning players from the game if necessary for disruptive people
	NewAccountCanPlay bool `json:"new_account_can_play"`
	// full pvp control here, pvp is extremely bugged right now, and somewhat under-developed in a few areas
	PvP PvPConfig `json:"pvp"`
}

type PvPConfig struct {
	MatchDurationSeconds int               `json:"match_duration_seconds"`
	KillTarget           int               `json:"kill_target"`
	Claptrap             ClaptrapPvPConfig `json:"claptrap"`
}

type ClaptrapPvPConfig struct {
	RoundSeconds int `json:"round_seconds"`
	TargetScore  int `json:"target_score"`
}

// this ties into NPC Difficulty below. does not apply to bosses, just normal ads
type NpcDifficultyTier struct {
	HPMultiplier     float64 `json:"hp_multiplier"`
	DamageMultiplier float64 `json:"damage_multiplier"`
}

type NpcDifficultyConfig struct {
	Normal NpcDifficultyTier `json:"normal"`
	Hard   NpcDifficultyTier `json:"hard"`
	Hell   NpcDifficultyTier `json:"hell"`
}

// bosses are here
type BossHPMultiplierConfig struct {
	Hard float64 `json:"hard"`
	Hell float64 `json:"hell"`
}

type NpcKillXPConfig struct {
	Base             float64 `json:"base"`
	BadassMultiplier float64 `json:"badass_multiplier"`
	BossMultiplier   float64 `json:"boss_multiplier"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	loadedPath.Store(path)
	return &cfg, nil
}

var loadedPath atomic.Value // string

func loadedConfigPath() string {
	if p, ok := loadedPath.Load().(string); ok {
		return p
	}
	return ""
}

// LiveNewAccountCanPlay - this is essentially a live tick checking if an account can play. it costs very little
// but worth looking into if this is the cause of certain people having issues in sessions
func LiveNewAccountCanPlay() bool {
	path := loadedConfigPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var probe struct {
		Game struct {
			NewAccountCanPlay bool `json:"new_account_can_play"`
		} `json:"game"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Game.NewAccountCanPlay
}

var configWriteMu sync.Mutex

var (
	reNewAccountCanPlayVal = regexp.MustCompile(`("new_account_can_play"\s*:\s*)(?:true|false)`)
	reGameBlockOpen        = regexp.MustCompile(`("game"\s*:\s*\{)`)
)

func SetNewAccountCanPlay(v bool) error {
	path := loadedConfigPath()
	if path == "" {
		return fmt.Errorf("no config file loaded")
	}
	configWriteMu.Lock()
	defer configWriteMu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	val := strconv.FormatBool(v)
	if reNewAccountCanPlayVal.Match(data) {
		data = reNewAccountCanPlayVal.ReplaceAll(data, []byte("${1}"+val))
	} else if reGameBlockOpen.Match(data) {
		data = reGameBlockOpen.ReplaceAll(data, []byte("${1}\n        \"new_account_can_play\": "+val+","))
	} else {
		return fmt.Errorf(`config has no "game" block to write new_account_can_play into`)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
