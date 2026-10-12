package photon

import ()

func (s *EnetServer) opHandler(opCode byte) func(*ClientSession, byte, map[byte]any) {
	switch opCode {
	case OpAuthenticate:
		return s.opAuthenticate
	case OpJoinLobby:
		return s.opJoinLobby
	case OpLeaveLobby:
		return s.opLeaveLobby
	case OpCreateRoom:
		return s.opCreateRoom
	case OpJoinRoom, OpJoinGame:
		return s.opJoinRoom
	case OpJoinRandom:
		return s.opJoinRandom
	case OpCancelJoin:
		return s.opCancelJoin
	case OpLeave:
		return s.opLeave
	case OpRaiseEvent:
		return s.opRaiseEvent
	case OpSetProps:
		return s.opSetProps
	case OpGetProps:
		return s.opGetProps
	case OpChangeGroups:
		return s.opChangeGroups
	case OpGPKAuth:
		return s.opGpkAuth
	case OpGetCharacterList:
		return s.opGetCharacterList
	case OpSelectCharacter:
		return s.opSelectCharacter
	case OpCreateCharacter:
		return s.opCreateCharacter
	case OpGetCharacter, OpGetCharacterByID:
		return s.opGetCharacter
	case OpDeleteCharacter:
		return s.opDelCharacter
	case OpSelectCharacterOLD:
		return s.opNoOp
	case OpUpdateCharacterInfo:
		return s.opUpdateCharacterInfo
	case OpUpdateCharacterExp:
		return s.opUpdateCharacterExp
	case OpUpdateCharacterModel:
		return s.opUpdateCharacterModel
	case OpUpdateCharacterName:
		return s.opUpdateCharacterName
	case OpGetSettings:
		return s.opGetSettings
	case OpSetSettings:
		return s.opSetSettings
	case OpGetCharacterSettings:
		return s.opGetCharacterSettings
	case OpSetCharacterSettings:
		return s.opSetCharacterSettings
	case OpGetPresence:
		return s.opGetPresence
	case OpGetAllFriendsPresence:
		return s.opGetAllFriendsPresence
	case OpSearchCharacter:
		return s.opSearchCharacter
	case OpUpdatePresence:
		return s.opUpdatePresence
	case OpGetAntiLevel:
		return s.opGetAntiLevel
	case OpGetMyCurrencies:
		return s.opGetMyCurrencies
	case OpRequestMatchRecord:
		return s.opRequestMatchRecord
	case OpGetInventory:
		return s.opGetInventory
	case OpTidy:
		return s.opTidy
	case OpGetWarehouse:
		return s.opGetWarehouse
	case OpSwapInventoryWarehouseSlot:
		return s.opSwapInventoryWarehouseSlot
	case OpSwapWarehouseSlot:
		return s.opSwapWarehouseSlot
	case OpUpdateInventory:
		return s.opNoOp
	case OpDropItemFromInventory:
		return s.opDropItemFromInventory
	case OpAddItemsToInventory:
		return s.opNoOp
	case OpSwapInventorySlots:
		return s.opSwapInventorySlots
	case OpSplitStack:
		return s.opSplitStack
	case OpMergeStack:
		return s.opMergeStack
	case OpEquipClassMod:
		return s.opEquipClassMod
	case OpEquipShield:
		return s.opEquipShield
	case OpUnequipShield:
		return s.opNoOp
	case OpEquipNewWeapon:
		return s.opEquipNewWeapon
	case OpSetActiveWeapon:
		return s.opSetActiveWeapon
	case OpUnequipWeapon:
		return s.opUnequipWeapon
	case OpEquipWeapon:
		return s.opEquipWeapon
	case OpSwapWeapons:
		return s.opSwapWeapons
	case OpEquipOutfit:
		return s.opEquipOutfit
	case OpUnequipOutfit:
		return s.opUnequipOutfit
	case OpGetUnlockedInstances:
		return s.opGetUnlockedInstances
	case OpCompleteInstance:
		return s.opCompleteInstance
	case OpUnlockInstance:
		return s.opNoOp
	case OpGetPlayerQuests:
		return s.opGetPlayerQuests
	case OpUpdateQuestState:
		return s.opUpdateQuestRequirement
	case OpGetDailyQuests:
		return s.opGetDailyQuests
	case OpTalkToNPC, OpBringToNPC:
		return s.opTalkToNPC
	case OpAcceptQuest:
		return s.opAcceptQuest
	case OpClaimQuestRewards:
		return s.opClaimQuestRewards
	case OpCompleteQuest:
		return s.opCompleteQuest
	case OpRejectQuest:
		return s.opRejectQuest
	case OpGetMyCraftedWeapons:
		return s.opGetMyCraftedWeapons
	case OpCraftWeapon:
		return s.opCraftWeapon
	case OpSmeltWeapon:
		return s.opSmeltWeapon
	case OpUpgradeStarExperience:
		return s.opUpgradeStarExperience
	case OpUpgradeStarLevel:
		return s.opUpgradeStarLevel
	case OpGetSkillPoints:
		return s.opGetSkillPoints
	case OpGetInvestedSkillPoints:
		return s.opGetInvestedSkillPoints
	case OpInvestSkillPoint:
		return s.opInvestSkillPoint
	case OpInvestMultipleSkillPoints:
		return s.opNoOp
	case OpResetSkillPoints:
		return s.opResetSkillPoints
	case OpGetGroupInfo:
		return s.opGetGroupInfo
	case OpRejoinGroup:
		return s.opRejoinGroup
	case OpSendGroupInvitation:
		return s.opSendGroupInvitation
	case OpAcceptGroupInvitation:
		return s.opAcceptGroupInvitation
	case OpRejectGroupInvitation:
		return s.opRejectGroupInvitation
	case OpLeaveGroup:
		return s.opLeaveGroup
	case OpKickPlayerFromGroup:
		return s.opKickPlayerFromGroup
	case OpSendGameInvitationsToGroup:
		return s.opSendGameInvitationsToGroup
	case OpCreateLobby:
		return s.opCreateLobby
	case OpOpenLobby:
		return s.opOpenLobby
	case OpStartLobbyGame:
		return s.opStartLobbyGame
	case OpGetLobby:
		return s.opGetLobby
	case OpCloseLobby:
		return s.opCloseLobby
	case OpFindLobby:
		return s.opNoOp
	case OpInviteToLobby:
		return s.opInviteToLobby
	case OpAcceptLobbyInvite:
		return s.opAcceptLobbyInvite
	case OpRejectLobbyInvite:
		return s.opRejectLobbyInvite
	case OpKickFromLobby:
		return s.opKickFromLobby
	case OpSetTeamID, OpSendPvPGameInvitations, OpSubmitPvPResults:
		return s.opNoOp
	case OpAcceptLobbyGameInvitation:
		return s.opAcceptLobbyGameInvitation
	case OpKickPlayerFromGame:
		return s.opKickPlayerFromGame
	case OpAcceptGameInvitation, OpCloseGame:
		return s.opNoOp
	case OpChat:
		return s.opChat
	case OpGetAllFriendsCharacterInfo:
		return s.opGetAllFriendsCharacterInfo
	case OpIsFriendsGameJoinable, OpJoinSessionInProgress:
		return s.opNoOp
	case OpAcceptGroupGameInvitation:
		return s.opAcceptGroupGameInvitation
	case OpSendGameInvitation:
		return s.opSendGameInvitation
	case OpIsUserIgnored:
		return s.opIsUserIgnored
	case OpUnignore:
		return s.opUnignore
	case OpIgnore:
		return s.opIgnore
	case OpFriendAdd:
		return s.opFriendAdd
	case OpFriendAccept:
		return s.opFriendAccept
	case OpFriendReject:
		return s.opFriendReject
	case OpFriendRemove:
		return s.opFriendRemove
	case OpGetMail:
		return s.opGetMail
	case OpSendMail:
		return s.opSendMail
	case OpReadMail:
		return s.opReadMail
	case OpGetMailItem:
		return s.opGetMailItem
	case OpDeleteMail:
		return s.opDeleteMail
	case OpDeleteMessage:
		return s.opDeleteMessage
	case OpMarkAsRead, OpRetrieveMessages, OpSendMessage, OpRequestItemDNA:
		return s.opNoOp
	case OpSellItem:
		return s.opSellItem
	case OpRedeemItem:
		return s.opRedeemItem
	case OpBuyItem:
		return s.opBuyItem
	case OpGetShop:
		return s.opGetShop
	case OpActivateGuildSkill, OpGetMyGuildSkillList, OpGetGuildSkillList, OpSubmitPvEFactors,
		OpAwardLuckyBox,
		OpGetGuildNews, OpUpdateGuildNews:
		return s.opNoOp
	case OpOpenLuckyBox:
		return s.opOpenLuckyBox
	case OpGetMyGuildNews:
		return s.opGetMyGuildNews

	case OpSearchGuild:
		return s.opSearchGuild
	case OpApplyToGuild:
		return s.opApplyToGuild
	case OpGetGuildApplications:
		return s.opGetGuildApplications
	case OpProcessGuildApplications:
		return s.opProcessGuildApplications
	case OpGetMyGuildId:
		return s.opGetMyGuildID
	case OpGetMyGuildInfo, OpGetGuildInfo:
		return s.opGetMyGuildInfo
	case OpCreateGuild:
		return s.opCreateGuild
	case OpDismissGuild:
		return s.opDismissGuild
	case OpChangeGuildMaster:
		return s.opChangeGuildMaster
	case OpQuitGuild:
		return s.opQuitGuild
	case OpExpellFromGuild:
		return s.opExpellFromGuild
	case OpChangeGuildBanner:
		return s.opChangeGuildBanner
	case OpCancelMyGuildApplication:
		return s.opCancelMyGuildApplication
	case OpGetMyGuildApplications:
		return s.opGetMyGuildApplications
	case OpInviteToGuild:
		return s.opInviteToGuild
	case OpGetMyGuildInvitations:
		return s.opGetMyGuildInvitations
	case OpProcessGuildInvitations:
		return s.opProcessGuildInvitations
	case OpNotifyGuildDismissal:
		return s.opNotifyGuildDismissal
	case OpAssignGuildManager:
		return s.opAssignGuildManager
	case OpDischargeGuildManager:
		return s.opDischargeGuildManager
	case OpGetGuildManagerList:
		return s.opGetGuildManagerList
	case OpGetAchievements:
		return s.opGetAchievements
	case OpGetRanking:
		return s.opGetRanking
	case OpRecordClientDebug:
		return s.opNoOp
	case OpRetrieveFriends:
		return s.opRetrieveFriends
	}
	return nil
}

func (s *EnetServer) opNoOp(sess *ClientSession, opCode byte, params map[byte]any) {
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opSilent(sess *ClientSession, opCode byte, params map[byte]any) {}

func actorList(room *Room) []int32 {
	arr := make([]int32, 0, len(room.Players))
	for nr := range room.Players {
		arr = append(arr, nr)
	}
	return arr
}

func anyMapToAny(m map[any]any) map[any]any {
	if m == nil {
		return make(map[any]any)
	}
	return m
}

func toSlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case nil:
		return nil
	default:
		return []any{v}
	}
}

func asInt32(v any) int32 {
	switch t := v.(type) {
	case int32:
		return t
	case int:
		return int32(t)
	case int64:
		return int32(t)
	case byte:
		return int32(t)
	case int16:
		return int32(t)
	case float64:
		return int32(t)
	}
	return 0
}

func DefaultGameHandlers() *GameHandlers {
	return &GameHandlers{
		EventHandlers: map[byte]func(*EnetPeer, map[byte]any, bool) ([]*CommandPacket, error){},
	}
}
