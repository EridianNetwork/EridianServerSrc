package photon

// ENet command types
const (
	CTNone           byte = 0
	CTAck            byte = 1
	CTConnect        byte = 2
	CTVerifyConnect  byte = 3
	CTDisconnect     byte = 4
	CTPing           byte = 5
	CTSendReliable   byte = 6
	CTSendUnreliable byte = 7
	CTSendFragment   byte = 8
	CTEGServerTime   byte = 12
)

// Photon message types
const (
	MsgInit        byte = 0
	MsgInitOK      byte = 1
	MsgOpRequest   byte = 2
	MsgOpResponse  byte = 3
	MsgEvent       byte = 4
	MsgInternalReq byte = 6
	MsgInternalRsp byte = 7
)

// Wire type codes
const (
	TNull      byte = 42
	TByte      byte = 98  // 'b'
	TBool      byte = 111 // 'o'
	TShort     byte = 107 // 'k'
	TInt       byte = 105 // 'i'
	TLong      byte = 108 // 'l'
	TFloat     byte = 102 // 'f'
	TDouble    byte = 100 // 'd'
	TString    byte = 115 // 's'
	TByteArray byte = 120 // 'x'
	TArray     byte = 121 // 'y'
	TArrObj    byte = 122 // 'z'
	THash      byte = 104 // 'h'
	TDict      byte = 68  // 'D'
	TArrayList byte = 118 // 'v'
	TStringArr byte = 97  // 'a'
	TEventData byte = 101 // 'e'
	TOpRsp     byte = 112 // 'p'
	TOpReq     byte = 113 // 'q'
	TCustom    byte = 99  // 'c'
	TIntArray  byte = 110 // 'n'
)

// Photon operation codes (client → server)
const (
	OpAuthenticate byte = 230
	OpJoinLobby    byte = 229
	OpLeaveLobby   byte = 228
	OpCreateRoom   byte = 227
	OpJoinRoom     byte = 255
	OpJoinGame     byte = 226
	OpJoinRandom   byte = 225
	OpCancelJoin   byte = 167
	OpLeave        byte = 254
	OpRaiseEvent   byte = 253
	OpSetProps     byte = 252
	OpGetProps     byte = 251
	OpChangeGroups byte = 248

	// Anti-cheat
	OpGPKAuth byte = 62

	// Character management
	OpGetCharacterList     byte = 84
	OpSelectCharacter      byte = 83
	OpCreateCharacter      byte = 82
	OpGetCharacter         byte = 80
	OpGetCharacterByID     byte = 81 // CharacterService.imethod_10, same OnGetCharacterResponse as 80, just charID-arg form
	OpDeleteCharacter      byte = 60
	OpSelectCharacterOLD   byte = 214
	OpUpdateCharacterInfo  byte = 213
	OpUpdateCharacterExp   byte = 216
	OpUpdateCharacterModel byte = 217
	OpUpdateCharacterName  byte = 218

	// Settings / presence
	OpGetSettings           byte = 75
	OpSetSettings           byte = 74
	OpGetCharacterSettings  byte = 53
	OpSetCharacterSettings  byte = 52
	OpGetPresence           byte = 147
	OpGetAllFriendsPresence byte = 146
	OpUpdatePresence        byte = 155
	OpSearchCharacter       byte = 152
	OpGetAntiLevel          byte = 85
	OpGetMyCurrencies       byte = 156
	OpRequestMatchRecord    byte = 148

	// Inventory / warehouse
	OpGetInventory               byte = 196
	OpTidy                       byte = 88
	OpGetWarehouse               byte = 99
	OpSwapInventoryWarehouseSlot byte = 97
	OpSwapWarehouseSlot          byte = 98
	OpUpdateInventory            byte = 177
	OpDropItemFromInventory      byte = 193
	OpAddItemsToInventory        byte = 194
	OpSwapInventorySlots         byte = 195
	OpSplitStack                 byte = 90 // Inventory.method_24 no client side response handler registered
	OpMergeStack                 byte = 89 // Inventory.method_25 shares OnNewInventory response like GetInventory/Tidy

	// Equipment
	OpEquipClassMod   byte = 86
	OpEquipNewWeapon  byte = 189
	OpSetActiveWeapon byte = 190
	OpUnequipWeapon   byte = 191
	OpEquipWeapon     byte = 192
	OpUnequipShield   byte = 134
	OpEquipShield     byte = 135
	OpSwapWeapons     byte = 166 // Inventory.method_7
	OpEquipOutfit     byte = 51  // Inventory.method_32
	OpUnequipOutfit   byte = 50  // Inventory.method_33

	// Instances / quests
	OpGetUnlockedInstances byte = 180
	OpCompleteInstance     byte = 179
	OpUnlockInstance       byte = 178
	OpGetPlayerQuests      byte = 185
	OpUpdateQuestState     byte = 183
	OpGetDailyQuests       byte = 141
	OpCompleteQuest        byte = 142
	OpGetMyCraftedWeapons  byte = 143
	OpBringToNPC           byte = 145
	OpRejectQuest          byte = 215
	OpTalkToNPC            byte = 150
	OpAcceptQuest          byte = 184
	OpClaimQuestRewards    byte = 181

	// Skill points
	OpGetSkillPoints            byte = 173
	OpGetInvestedSkillPoints    byte = 171
	OpInvestSkillPoint          byte = 170
	OpInvestMultipleSkillPoints byte = 169
	OpResetSkillPoints          byte = 168

	// Group / party
	OpGetGroupInfo               byte = 158
	OpRejoinGroup                byte = 151
	OpLeaveGroup                 byte = 159
	OpKickPlayerFromGroup        byte = 160
	OpRejectGroupInvitation      byte = 161
	OpAcceptGroupInvitation      byte = 162 // param[0] = group_invite mail_id
	OpSendGroupInvitation        byte = 163 // param[0] = target char_id
	OpSendGameInvitationsToGroup byte = 157

	// Lobby / matchmaking
	OpCreateLobby               byte = 87
	OpOpenLobby                 byte = 96
	OpFindLobby                 byte = 133
	OpInviteToLobby             byte = 132
	OpAcceptLobbyInvite         byte = 131
	OpRejectLobbyInvite         byte = 130
	OpKickFromLobby             byte = 129
	OpGetLobby                  byte = 126
	OpCloseLobby                byte = 125
	OpStartLobbyGame            byte = 127
	OpSetTeamID                 byte = 122
	OpSendPvPGameInvitations    byte = 123
	OpAcceptLobbyGameInvitation byte = 124
	OpSubmitPvPResults          byte = 56
	OpKickPlayerFromGame        byte = 136
	OpAcceptGameInvitation      byte = 137
	OpCloseGame                 byte = 138

	// Chat
	OpChat byte = 139

	// Friends / social
	OpGetAllFriendsCharacterInfo byte = 144
	OpIsFriendsGameJoinable      byte = 197
	OpJoinSessionInProgress      byte = 198
	OpAcceptGroupGameInvitation  byte = 199
	OpSendGameInvitation         byte = 200
	OpIsUserIgnored              byte = 201
	OpUnignore                   byte = 202
	OpIgnore                     byte = 203
	OpFriendAdd    byte = 164 // param[0] = target char_id
	OpFriendReject byte = 165 // param[0] = friend_request mail_id
	OpFriendRemove byte = 204 // param[0] = friend char_id
	OpFriendAccept byte = 205 // param[0] = friend_request mail_id

	// Mail / messages
	OpDeleteMail       byte = 91
	OpGetMailItem      byte = 92
	OpReadMail         byte = 93
	OpSendMail         byte = 94
	OpGetMail          byte = 95
	OpDeleteMessage    byte = 208
	OpMarkAsRead       byte = 209
	OpRetrieveMessages byte = 210
	OpSendMessage      byte = 211
	OpRequestItemDNA   byte = 212

	// Shop
	OpSellItem   byte = 119
	OpRedeemItem byte = 54
	OpBuyItem    byte = 120
	OpGetShop    byte = 121

	// Guild
	OpGetGuildManagerList      byte = 63
	OpGetMyGuildApplications   byte = 77
	OpActivateGuildSkill       byte = 64
	OpGetMyGuildSkillList      byte = 65
	OpGetGuildSkillList        byte = 66
	OpSubmitPvEFactors         byte = 67
	OpAwardLuckyBox            byte = 70
	OpOpenLuckyBox             byte = 71
	OpDischargeGuildManager    byte = 72
	OpAssignGuildManager       byte = 73
	OpCancelMyGuildApplication byte = 76
	OpGetMyGuildNews           byte = 78
	OpChangeGuildBanner        byte = 100
	OpGetMyGuildInfo           byte = 101
	OpGetGuildInfo             byte = 103
	OpCreateGuild              byte = 104
	OpDismissGuild             byte = 105
	OpNotifyGuildDismissal     byte = 106
	OpChangeGuildMaster        byte = 107
	OpSearchGuild              byte = 108
	OpApplyToGuild             byte = 109
	OpGetGuildApplications     byte = 110
	OpProcessGuildApplications byte = 111
	OpQuitGuild                byte = 112
	OpExpellFromGuild          byte = 113
	OpInviteToGuild            byte = 114
	OpGetMyGuildInvitations    byte = 115
	OpProcessGuildInvitations  byte = 116
	OpGetGuildNews             byte = 117
	OpUpdateGuildNews          byte = 118
	OpCraftWeapon           byte = 153
	OpSmeltWeapon           byte = 154
	OpUpgradeStarLevel      byte = 68
	OpUpgradeStarExperience byte = 69

	// Achievements
	OpGetAchievements byte = 46

	// PvP leaderboard
	OpGetRanking byte = 57

	// Debug / misc (server always returns OK)
	OpRecordClientDebug byte = 61
	OpRetrieveFriends   byte = 207
	OpGetMyGuildId      byte = 102
)


const (
	MailNotExsit          int16 = 32741 // ReadMail/GetMailItem/DeleteMail: mailID doesn't exist / not owned
	MailCharacterNotExist int16 = 32740 // SendMail: recipient not found (also used for self-mail)
	MailInBlock           int16 = 32739 // SendMail: recipient has sender blocked
	MailContentLimit      int16 = 32738 // SendMail: content exceeds MAX_MAIL_CONTENT_LENGTH (300)
	MailInboxFull         int16 = 32737 // SendMail: recipient's mailbox is at MAILBOX_MAXVOLUME (50)
	MailNotItem           int16 = 32736 // GetMailItem: mail has no attachment
	SmeltNoItem            int16 = 32725 // no weapon at that backpack slot
	SmeltNotEnoughCurrency int16 = 32726 // can't afford the melt config's currency cost
	SmeltShopBought        int16 = 32728 // shop-bought weapons (GenerationSource=Shop) can't be melted
)

// Event codes (server → client)
const (
	EvPresenceUpdated          byte = 195
	EvKickPlayer               byte = 163
	EvNotifyAntiAddictionLevel byte = 159
	EvNotifyAntiAddiction      byte = 162
	EvAnnouncements            byte = 160
	EvMailList                 byte = 161
	EvNewMail                  byte = 167
	EvWarehouseUpdated         byte = 168
	EvQuestTaskUpdated         byte = 169
	EvQuestClearDailyQuest     byte = 170
	EvQuestTaskCompleted       byte = 217
	EvQuestStatusUpdated       byte = 220
	EvInventoryAction          byte = 221
	EvInventoryUpdated         byte = 222
	EvCurrenciesUpdated        byte = 218 // CurrencyService: {0: bondCurrency, 1: specialCurrency, 2: circulateCurrency}
	EvExperienceUpdated       byte = 206 // Online peer: OnExperienceUpdatedEvent, params[0]=(int)newExperience
	EvLevelUp                 byte = 204 // Online peer: OnLevelUpEvent, params[0]=charID,[1]=newExp,[2]=newLevel
	EvInstanceUnlocked        byte = 205
	EvInstanceCompleted       byte = 219
	EvNewFriend               byte = 208
	EvUnfriended              byte = 207
	EvRejectFriend            byte = 155 // sent to the original requester when rejected
	EvChat                    byte = 199
	EvSendChatOk              byte = 198
	EvNewMessage              byte = 223
	EvAppStats                byte = 226
	EvMatch                   byte = 227
	EvLobbyCreated            byte = 166
	EvLobbyOpened             byte = 164
	EvLobbyClosed             byte = 191
	EvLobbyOwnerChanged       byte = 190
	EvLobbyGameStarted        byte = 192
	EvPlayerLeftLobby         byte = 193
	EvGroupJoinedLobby        byte = 189
	EvGroupLeaveLobby         byte = 188
	EvGroupMatchmakingStarted byte = 165
	EvGroupDisbanded          byte = 209
	EvGroupGameStarted        byte = 210
	EvGroupMemberLeft         byte = 211
	EvGroupInviteRejected     byte = 212
	EvNewGroupMember          byte = 213
	EvNewGroupLeader          byte = 214
	EvKickedFromGroup         byte = 215
	EvKickedFromGame   byte = 196
	EvGameOwnerChanged byte = 197
	EvGuildInvitation        byte = 231
	EvGuildJoined            byte = 232
	EvGuildExpelled          byte = 233
	EvGuildSkillSlotGiven    byte = 234
	EvGuildSkillSelected     byte = 235
	EvGuildManagerAssigned   byte = 236
	EvGuildManagerDischarged byte = 237
	EvGuildMasterChanged     byte = 238
)

// Well-known parameter keys
const (
	ParamAccountID    byte = 0
	ParamAppVersion   byte = 220
	ParamAppID        byte = 224
	ParamUsername     byte = 104
	ParamPassword     byte = 109
	ParamFlags        byte = 105
	ParamToken        byte = 221
	ParamAuthFailReason byte = 250
	ParamRoomName     byte = 255
	ParamPlayerNr     byte = 254
	ParamActorList    byte = 252
	ParamPlayerProps  byte = 249
	ParamRoomProps    byte = 248
	ParamActors       byte = 250
	ParamTargetActors byte = 251
	ParamEventCode    byte = 244
	ParamEventData    byte = 245
	ParamRedisNr      byte = 253
	ParamServerAddr   byte = 108
	ParamObjArray     byte = 112
)

// Header sizes
const (
	EnetHeaderLen    = 12
	CommandHeaderLen = 12
	AckBodyLen       = 8
	ConnectBodyLen   = 32
	VerifyConnectLen = 32
	FragmentBodyLen  = 20
)

// MaxPlayers the default max players per room
const MaxPlayers = 16

// InitialisedPeerID the first peer ID assigned to a new connection
const InitialisedPeerID = 1

// Avatar defaults
var AvatarDefaultBkID = map[int]uint32{
	0: 1242413768, // SIREN
	1: 1267549099, // SOLDIER
	2: 3556519174, // BERSERKER
	3: 3169502637, // HUNTER
}

const AvatarDefaultParts = "0,0,0,0,0,0,0,0"

var StarterWeaponHT = map[int]any{
	0: 2, 1: 1, 2: 0, 3: 0, 4: 1,
	5: 0, 6: 0, 7: 0, 8: "",
	9: "SMG", 10: 0,
	11: 0, 12: 0, 13: 0, 14: 0, 15: 0, 16: 0, 17: 0, 18: 0, 19: 0,
	20: 0, 21: 0, 22: 0,
}

const CURSCN_OBFUSCATED = "yawSkhXz0eqhCLhCiU88Og=="
