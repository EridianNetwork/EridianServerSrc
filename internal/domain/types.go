package domain

type Account struct {
	AccountID int64  `json:"account_id"`
	Username  string `json:"username"`
	Password  string `json:"-"`
	Email     string `json:"email"`
	Token     string `json:"token"`
	IsBanned  bool   `json:"is_banned"`
	IsGM      bool   `json:"is_gm"`
	IsTester bool `json:"is_tester"`
	CanPlay   bool   `json:"can_play"`
	CreatedAt string `json:"created_at"`
	LastLogin string `json:"last_login"`
}

// purely for beta testers when period is over
func (a *Account) CanPlayRejectMessage() string {
	if a.IsTester {
		return "The testing period has ended, thanks for your help"
	}
	return "This account is not currently allowed to play, contact support if you believe this is a mistake."
}

type Character struct {
	CharID          int64   `json:"char_id"`
	AccountID       int64   `json:"account_id"`
	Name            string  `json:"name"`
	Level           int     `json:"level"`
	Experience      int     `json:"experience"`
	ClassType       int     `json:"class_type"` // maps to avatar_type (0=SIREN,1=SOLDIER,2=BERSERKER,3=HUNTER)
	Health          float64 `json:"health"`
	Shield          float64 `json:"shield"`
	PositionX       float64 `json:"position_x"`
	PositionY       float64 `json:"position_y"`
	PositionZ       float64 `json:"position_z"`
	Rotation        float64 `json:"rotation"`
	PlayTime        int     `json:"play_time"`
	Money           int     `json:"money"`
	Eridium         int     `json:"eridium"`
	SeraphCrystals  int     `json:"seraph_crystals"`
	SkinColor       int     `json:"skin_color"`
	EyeColor        int     `json:"eye_color"`
	HairColor       int     `json:"hair_color"`
	HairStyle       int     `json:"hair_style"`
	FaceStyle       int     `json:"face_style"`
	HeadStyle       int     `json:"head_style"`
	DecoStyle       int     `json:"deco_style"`
	AvatarParts     string  `json:"avatar_parts"`
	AvatarMaterials string  `json:"avatar_materials"`
	AvatarID        uint32  `json:"avatar_id"`
	Skills          string  `json:"skills"`
	IsDeleted       bool    `json:"is_deleted"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	BackpackSize     int    `json:"backpack_size"`
	Ammo             string `json:"ammo"`
	ActiveWeaponSlot int    `json:"active_weapon_slot"`
	Settings string `json:"settings"`
	LastSeen string `json:"last_seen"`
	Energy int `json:"energy"`
	EnergyUpdatedAt string `json:"energy_updated_at"`
	LastTownPlaylistID int32 `json:"last_town_playlist_id"`
}

type InventoryItem struct {
	ItemID        int64   `json:"item_id"`
	CharID        int64   `json:"char_id"`
	Slot          int     `json:"slot"`
	ResourceID    int     `json:"resource_id"`
	Quantity      int     `json:"quantity"`
	Flags         int     `json:"flags"`
	Rarity        int     `json:"rarity"`
	Damage        float64 `json:"damage"`
	Accuracy      float64 `json:"accuracy"`
	FireRate      float64 `json:"fire_rate"`
	ReloadSpeed   float64 `json:"reload_speed"`
	Magazine      int     `json:"magazine"`
	ElementType   int     `json:"element_type"`
	ElementDmg    float64 `json:"element_damage"`
	ElementChance float64 `json:"element_chance"`
	Parts         int     `json:"parts"`
	Seed          int     `json:"seed"`
	IsEquipped    bool    `json:"is_equipped"`
	IsWeapon      bool    `json:"is_weapon"`

	// item DNA fields
	ItemKind         int    `json:"item_kind"`
	Level            int    `json:"level"`
	UID              string `json:"uid"`
	ItemName         string `json:"item_name"`
	GenerationSource int    `json:"generation_source"`
	Price            int    `json:"price"`
	Expiration       int    `json:"expiration"`
	OwnerID          int    `json:"owner_id"`
	PartA            int    `json:"part_a"`
	PartB            int    `json:"part_b"`
	PartC            int    `json:"part_c"`
	PartD            int    `json:"part_d"`
	PartE            int    `json:"part_e"`
	PartF            int    `json:"part_f"`
	Description      string `json:"description"`
	StarLevel      int `json:"star_level"`
	StarExperience int `json:"star_experience"`
	Subparts       string `json:"subparts"`
	ExtraEffects   string `json:"extra_effects"`
}

type WarehouseItem struct {
	ItemID        int64   `json:"item_id"`
	AccountID     int64   `json:"account_id"`
	Slot          int     `json:"slot"`
	ResourceID    int     `json:"resource_id"`
	Quantity      int     `json:"quantity"`
	Rarity        int     `json:"rarity"`
	Damage        float64 `json:"damage"`
	Accuracy      float64 `json:"accuracy"`
	FireRate      float64 `json:"fire_rate"`
	ReloadSpeed   float64 `json:"reload_speed"`
	Magazine      int     `json:"magazine"`
	ElementType   int     `json:"element_type"`
	ElementDmg    float64 `json:"element_damage"`
	ElementChance float64 `json:"element_chance"`
	Parts         string  `json:"parts"`
	Seed          int     `json:"seed"`
	IsWeapon      bool    `json:"is_weapon"`
	CreatedAt     string  `json:"created_at"`
}

type Friend struct {
	AccountID int64  `json:"account_id"`
	FriendID  int64  `json:"friend_id"`
	Username  string `json:"username"`
	Status    int    `json:"status"`
	CreatedAt string `json:"created_at"`
}

type Guild struct {
	GuildID           int64  `json:"guild_id"`
	Name              string `json:"name"`
	Banner            string `json:"banner"`
	Level             int    `json:"level"`
	Experience        int    `json:"experience"`
	MemberCount       int32  `json:"member_count"`
	HasVacancy        bool   `json:"has_vacancy"`
	MaxMemberCount    int    `json:"max_member_count"`
	MasterAccountID   int32  `json:"master_account_id"`
	MasterCharacterID int32  `json:"master_character_id"`
	FreeSkillCounts   int32  `json:"free_skill_counts"`
	Preference        int    `json:"preference"`
}

type GuildMember struct {
	GuildID   int64  `json:"guild_id"`
	AccountID int64  `json:"account_id"`
	Role      int    `json:"role"`
	JoinedAt  string `json:"joined_at"`
}

type Mail struct {
	MailID     int64  `json:"mail_id"`
	SenderID   int64  `json:"sender_id"`
	ReceiverID int64  `json:"receiver_id"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`

	// item specific mail vars
	ItemType  int    `json:"item_type"`
	ItemData  string `json:"item_data"`
	IsRead    bool   `json:"is_read"`
	IsClaimed bool   `json:"is_claimed"`
	SentAt    string `json:"sent_at"`

	MailType int `json:"mail_type"`
}

const (
	ItemTypeWeapon    = 2
	ItemTypeShield    = 5
	ItemTypeMaterial  = 6
	ItemTypeClassMode = 8
	ItemTypeLuckyKey  = 9
	ItemTypeOutfit    = 11
)

const (
	GenerationSourceLoot  = 0
	GenerationSourceShop  = 1
	GenerationSourceCraft = 2
)


const (
	AmmoTypeSMG          = 0
	AmmoTypeSniperRifle  = 1
	AmmoTypeGrenade      = 2
	AmmoTypeShotGun      = 3
	AmmoTypePistol       = 4
	AmmoTypeAssaultRifle = 5
)

const (
	AvatarSubPartHair   = 0
	AvatarSubPartHead   = 1
	AvatarSubPartMask   = 2
	AvatarSubPartBody   = 3
	AvatarSubPartPants  = 4
	AvatarSubPartShoe   = 5
	AvatarSubPartCM     = 6
	AvatarSubPartGlover = 7
	AvatarSubPartTotal  = 8
)

type MailItem struct {
	// Weapon (ItemTypeWeapon)
	WeaponID   int    `json:"weapon_id,omitempty"`
	WeaponName string `json:"weapon_name,omitempty"`
	WeaponType int    `json:"weapon_type,omitempty"`
	Level      int    `json:"level,omitempty"`
	Rarity     int    `json:"rarity,omitempty"`

	// Material (ItemTypeMaterial)
	MaterialType int `json:"material_type,omitempty"`
	Quantity     int `json:"quantity,omitempty"`
}

type QuestProgress struct {
	ProgressID   int64  `json:"progress_id"`
	CharID       int64  `json:"char_id"`
	QuestID      int    `json:"quest_id"`
	Status       int    `json:"status"`
	CurrentTask  int    `json:"current_task"`
	TaskProgress int    `json:"task_progress"`
	Objectives   string `json:"objectives"`
	StartedAt    string `json:"started_at"`
	CompletedAt  string `json:"completed_at"`
}


type AchievementProgress struct {
	CharID        int64 `json:"char_id"`
	AchievementID int   `json:"achievement_id"`
	Stage         int   `json:"stage"`
	Progress      int   `json:"progress"`
	UpdatedAt     int64 `json:"updated_at"` // unix seconds, 0 = never updated
}

// QuestState value
const (
	QuestStateUnavailable   = 0
	QuestStateAvailable     = 1
	QuestStateInProgress    = 2
	QuestStateCompleted     = 3
	QuestStateRewardClaimed = 4
	QuestStateFailed        = 5
)

type PvPStats struct {
	CharID  int64 `json:"char_id"`
	Matches int   `json:"matches"`
	Wins    int   `json:"wins"`
	Losses  int   `json:"losses"`
	Kills   int   `json:"kills"`
	Deaths  int   `json:"deaths"`
	Honor   int   `json:"honor"`
	Rating  int   `json:"rating"`
}

// FriendRequest represents a pending friend request
type FriendRequest struct {
	AccountID int64  `json:"account_id"`
	Username  string `json:"username"`
	Status    int    `json:"status"`
	CreatedAt string `json:"created_at"`
}

// server.json file representation

type ServerConfig struct {
	AuthServer struct {
		Host      string `json:"host"`
		HTTPPort  int    `json:"http_port"`
		HTTPSPort int    `json:"https_port"`
		SecretKey string `json:"secret_key"`
	} `json:"auth_server"`
	OnlineServer struct {
		Host       string `json:"host"`
		Port       int    `json:"port"`
		MaxClients int    `json:"max_clients"`
	} `json:"online_server"`
	PhotonServer struct {
		Host    string `json:"host"`
		TCPort  int    `json:"tcp_port"`
		UDPPort int    `json:"udp_port"`
		WSPort  int    `json:"ws_port"`
	} `json:"photon_server"`
	Game struct {
		Version             string `json:"version"`
		EnablePVP           bool   `json:"enable_pvp"`
		AFKKickSeconds      int    `json:"afk_kick_seconds"`
		TutorialFreeActions bool   `json:"tutorial_free_actions"`
	} `json:"game"`
}
