package photon

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode"

	"eridian/internal/db"
	"eridian/internal/domain"
)


func guildTagFor(accountID int64) string {
	guildID, err := db.GetAccountGuildID(accountID)
	if err != nil || guildID == 0 {
		return ""
	}
	g, err := db.GetGuild(guildID)
	if err != nil || g == nil || g.Name == "" {
		return ""
	}
	tag := guildInitials(g.Name)
	if tag == "" {
		return ""
	}
	return "[" + tag + "]"
}

func guildInitials(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) == 1 {
		r := []rune(strings.ToUpper(fields[0]))
		if len(r) > 3 {
			r = r[:3]
		}
		return string(r)
	}
	max := len(fields)
	if max > 4 {
		max = 4
	}
	var b strings.Builder
	for _, w := range fields[:max] {
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		b.WriteRune(unicode.ToUpper(r[0]))
	}
	return b.String()
}


func (s *EnetServer) opSearchGuild(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	pref, limit := 0, 50
	if len(arr) > 0 {
		pref = int(asInt32(arr[0]))
	}
	if len(arr) > 1 {
		limit = int(asInt32(arr[1]))
	}
	guilds, err := db.SearchGuilds(pref, limit)
	if err != nil {
		log.Printf("[PHOTON] SearchGuild user=%s error: %v", sess.userID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] SearchGuild user=%s pref=%d → %d guilds", sess.userID, pref, len(guilds))

	result := map[byte]any{}
	for i, g := range guilds {
		if i > 255 {
			break
		}
		result[byte(i)] = guildToHashtable(g)
	}
	s.sendOpResponse(sess, opCode, 0, result)
}


func (s *EnetServer) opGetMyGuildApplications(sess *ClientSession, opCode byte, params map[byte]any) {
	guildIDs, err := db.GetMyGuildApplications(sess.dbAccountID)
	if err != nil {
		log.Printf("[PHOTON] GetMyGuildApplications user=%s error: %v", sess.userID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	result := map[byte]any{}
	i := 0
	for _, guildID := range guildIDs {
		if i > 255 {
			break
		}
		g, err := db.GetGuild(guildID)
		if err != nil {
			log.Printf("[PHOTON] GetMyGuildApplications: guild %d missing: %v", guildID, err)
			continue
		}
		result[byte(i)] = guildToHashtable(g)
		i++
	}
	log.Printf("[PHOTON] GetMyGuildApplications user=%s → %d guilds", sess.userID, i)
	s.sendOpResponse(sess, opCode, 0, result)
}

func (s *EnetServer) opApplyToGuild(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID := int64(asInt32(arr[0]))
	if err := db.ApplyToGuild(guildID, sess.dbAccountID, sess.selectedCharID); err != nil {
		log.Printf("[PHOTON] ApplyToGuild user=%s guild=%d error: %v", sess.userID, guildID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] ApplyToGuild user=%s → guild=%d", sess.userID, guildID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(guildID)})
}


func (s *EnetServer) opGetGuildApplications(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID := int64(asInt32(arr[0]))
	g, err := db.GetGuild(guildID)
	if err != nil || int64(g.MasterAccountID) != sess.dbAccountID {
		log.Printf("[PHOTON] GetGuildApplications user=%s guild=%d denied (err=%v)", sess.userID, guildID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	apps, err := db.GetGuildApplicants(guildID)
	if err != nil {
		log.Printf("[PHOTON] GetGuildApplications guild=%d error: %v", guildID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}

	result := map[byte]any{}
	i := 0
	for _, a := range apps {
		if i > 255 {
			break
		}
		ch, err := db.GetCharacterByID(a.CharID)
		if err != nil {
			log.Printf("[PHOTON] GetGuildApplications: applicant char %d missing: %v", a.CharID, err)
			continue
		}
		result[byte(i)] = s.presenceHT(ch.CharID, charToHashtable(ch, realInventoryHashtable(ch.CharID)), s.isCharOnline(ch.CharID), charLastSeenUnix(ch))
		i++
	}
	log.Printf("[PHOTON] GetGuildApplications user=%s guild=%d → %d applicants", sess.userID, guildID, i)
	s.sendOpResponse(sess, opCode, 0, result)
}


func (s *EnetServer) opGetMyGuildNews(sess *ClientSession, opCode byte, params map[byte]any) {
	guildID, err := db.GetAccountGuildID(sess.dbAccountID)
	if err != nil || guildID == 0 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	news, err := db.GetGuildNews(guildID)
	if err != nil {
		log.Printf("[PHOTON] GetMyGuildNews user=%s guild=%d error: %v", sess.userID, guildID, err)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}

	result := map[byte]any{}
	for i, n := range news {
		if i > 255 {
			break
		}
		ht := map[int]any{
			0: int32(n.PosterAccountID),
			1: int32(n.PosterCharacterID),
			2: int32(n.Reason),
		}
		if n.Objective.Valid {
			ht[3] = int32(n.Objective.Int64)
		}
		if n.PosterName.Valid {
			ht[4] = n.PosterName.String
		}
		if n.ObjectiveName.Valid {
			ht[5] = n.ObjectiveName.String
		}
		result[byte(i)] = ht
	}
	log.Printf("[PHOTON] GetMyGuildNews user=%s guild=%d → %d entries", sess.userID, guildID, len(news))
	s.sendOpResponse(sess, opCode, 0, result)
}

func hashLookup(m map[any]any, i int) (any, bool) {
	if v, ok := m[byte(i)]; ok {
		return v, true
	}
	if v, ok := m[Short(i)]; ok {
		return v, true
	}
	if v, ok := m[int32(i)]; ok {
		return v, true
	}
	return nil, false
}

func (s *EnetServer) opProcessGuildApplications(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID := int64(asInt32(arr[0]))
	g, err := db.GetGuild(guildID)
	if err != nil || int64(g.MasterAccountID) != sess.dbAccountID {
		log.Printf("[PHOTON] ProcessGuildApplications user=%s guild=%d denied (err=%v)", sess.userID, guildID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	entries, _ := arr[1].(map[any]any)
	approved := 0
	for _, v := range entries {
		entry, ok := v.(map[any]any)
		if !ok {
			continue
		}
		charIDRaw, ok := hashLookup(entry, 0)
		if !ok {
			continue
		}
		charID := int64(asInt32(charIDRaw))
		allow, _ := func() (bool, bool) {
			raw, ok := hashLookup(entry, 1)
			if !ok {
				return false, false
			}
			b, ok := raw.(bool)
			return b, ok
		}()
		ch, err := db.GetCharacterByID(charID)
		if err != nil {
			log.Printf("[PHOTON] ProcessGuildApplications: applicant char %d missing: %v", charID, err)
			continue
		}
		if allow {
			if err := db.ApproveGuildApplication(guildID, ch.AccountID); err != nil {
				log.Printf("[PHOTON] ProcessGuildApplications approve error: %v", err)
				continue
			}
			approved++
			for _, other := range s.sessions {
				if other.selectedCharID == charID {
					s.sendEventTo(other, EvGuildJoined, map[byte]any{0: int32(guildID), 1: g.Name, 2: true})
				}
			}
		} else {
			if err := db.RejectGuildApplication(guildID, ch.AccountID); err != nil {
				log.Printf("[PHOTON] ProcessGuildApplications reject error: %v", err)
			}
		}
	}
	memberCount, maxMembers := int32(0), int32(0)
	if g2, err := db.GetGuild(guildID); err == nil {
		memberCount, maxMembers = g2.MemberCount, int32(g2.MaxMemberCount)
	}
	log.Printf("[PHOTON] ProcessGuildApplications user=%s guild=%d approved=%d", sess.userID, guildID, approved)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: memberCount, 1: maxMembers})
}


func (s *EnetServer) opGetMyGuildID(sess *ClientSession, opCode byte, params map[byte]any) {
	guildID, err := db.GetAccountGuildID(sess.dbAccountID)
	if err != nil {
		log.Printf("[PHOTON] GetMyGuildId user=%s error: %v", sess.userID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	if guildID == 0 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(guildID)})
}

func (s *EnetServer) opGetMyGuildInfo(sess *ClientSession, opCode byte, params map[byte]any) {
	var guildID int64
	if arr := toSlice(params[ParamObjArray]); len(arr) > 0 {
		guildID = int64(asInt32(arr[0]))
	} else {
		guildID, _ = db.GetAccountGuildID(sess.dbAccountID)
	}
	g, err := db.GetGuild(guildID)
	if err != nil {
		log.Printf("[PHOTON] GetGuildInfo user=%s guild=%d error: %v", sess.userID, guildID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	ht := guildToHashtable(g)
	ht[15] = s.guildMemberPresenceList(guildID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(g.GuildID),
		1: ht,
	})
}

func (s *EnetServer) guildMemberPresenceList(guildID int64) TypedArray {
	accountIDs, err := db.GetGuildMemberAccountIDs(guildID)
	if err != nil {
		log.Printf("[PHOTON] guildMemberPresenceList guild=%d error: %v", guildID, err)
		return TypedArray{ElementType: THash, Items: []any{}}
	}
	list := make([]any, 0, len(accountIDs))
	for _, accID := range accountIDs {
		chars, err := db.GetCharactersByAccount(accID)
		if err != nil || len(chars) == 0 {
			continue
		}
		ch := chars[0]
		for _, c := range chars[1:] {
			if c.Level > ch.Level {
				ch = c
			}
		}
		list = append(list, s.presenceHT(ch.CharID, charToHashtable(ch, realInventoryHashtable(ch.CharID)), s.isCharOnline(ch.CharID), charLastSeenUnix(ch)))
	}
	return TypedArray{ElementType: THash, Items: list}
}

func guildToHashtable(g *domain.Guild) map[int]any {
	return map[int]any{
		0:  int32(g.GuildID),
		1:  g.Name,
		2:  g.Banner,
		3:  int32(g.Level),
		4:  int32(g.Experience),
		5:  g.MemberCount,
		6:  g.Preference&1 != 0, // Social
		7:  g.Preference&2 != 0, // Quest
		8:  g.Preference&4 != 0, // PvP
		9:  g.Preference&8 != 0, // PvE
		10: g.FreeSkillCounts,
		11: g.HasVacancy,
		12: int32(g.MaxMemberCount),
		13: g.MasterAccountID + 100000,
		14: g.MasterCharacterID,
		15: nil,
	}
}

const (
	guildCreateSuccess           = 0
	guildCreateAlreadyMember     = 1
	guildCreateNameExists        = 2
	guildCreateNameProfanity     = 3
	guildCreateInsufficientLevel = 4
	guildCreateInsufficientName  = 5
	guildCreateInsufficientMoney = 6
	guildCreateInvalidAccount    = 7
	guildCreateBannerProfanity   = 8
	guildCreateFailureETC        = 9
)

const (
	guildInviteSuccess                  = 0
	guildInviteInvalidAccount           = 1
	guildInviteRecursive                = 2
	guildInviteInsufficientRight        = 3
	guildInviteInsufficientVacancy      = 4
	guildInviteInsufficientInviteeLevel = 5
	guildInviteAlreadyInvited           = 6
	guildInviteAlreadyHasGuild          = 7
	guildInviteFailureETC               = 8
)

func (s *EnetServer) sessionsByAccount(accountID int64) []*ClientSession {
	var out []*ClientSession
	for _, sess := range s.sessions {
		if sess.dbAccountID == accountID {
			out = append(out, sess)
		}
	}
	return out
}

func (s *EnetServer) opCreateGuild(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 || arr[0] == nil {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(guildCreateFailureETC)})
		return
	}
	name := fmt.Sprintf("%v", arr[0])
	banner := ""
	if len(arr) > 1 && arr[1] != nil {
		banner = fmt.Sprintf("%v", arr[1])
	}
	pref := int32(15)
	if len(arr) > 2 {
		pref = asInt32(arr[2])
	}
	g, err := db.CreateGuild(sess.dbAccountID, sess.selectedCharID, name, banner, pref)
	if err != nil {
		result := guildCreateFailureETC
		switch {
		case errors.Is(err, db.ErrAlreadyInGuild):
			result = guildCreateAlreadyMember
		case errors.Is(err, db.ErrGuildNameTaken):
			result = guildCreateNameExists
		}
		log.Printf("[PHOTON] CreateGuild user=%s name=%q error: %v", sess.userID, name, err)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(result)})
		return
	}
	log.Printf("[PHOTON] CreateGuild user=%s → guild=%d %q", sess.userID, g.GuildID, name)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(guildCreateSuccess), 1: guildToHashtable(g)})
}

func (s *EnetServer) opDismissGuild(sess *ClientSession, opCode byte, params map[byte]any) {
	guildID, err := db.GetAccountGuildID(sess.dbAccountID)
	if err != nil || guildID == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	g, _ := db.GetGuild(guildID)
	memberAccounts, _ := db.GetGuildMemberAccountIDs(guildID)
	if err := db.DismissGuild(guildID, sess.dbAccountID); err != nil {
		log.Printf("[PHOTON] DismissGuild user=%s guild=%d error: %v", sess.userID, guildID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] DismissGuild user=%s guild=%d", sess.userID, guildID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	guildName := ""
	if g != nil {
		guildName = g.Name
	}
	for _, accID := range memberAccounts {
		for _, other := range s.sessionsByAccount(accID) {
			s.sendEventTo(other, EvGuildExpelled, map[byte]any{0: int32(guildID), 1: guildName, 2: true})
		}
	}
}

func (s *EnetServer) opChangeGuildMaster(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	newMasterCharID := int64(asInt32(arr[0]))
	newMasterAccountID, err := db.ChangeGuildMaster(sess.dbAccountID, newMasterCharID)
	if err != nil {
		log.Printf("[PHOTON] ChangeGuildMaster user=%s newMaster=%d error: %v", sess.userID, newMasterCharID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID, _ := db.GetAccountGuildID(sess.dbAccountID)
	log.Printf("[PHOTON] ChangeGuildMaster user=%s guild=%d newMasterChar=%d", sess.userID, guildID, newMasterCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(newMasterCharID)})
	for _, other := range s.sessionsByAccount(newMasterAccountID) {
		s.sendEventTo(other, EvGuildMasterChanged, map[byte]any{0: int32(guildID)})
	}
}

func (s *EnetServer) opQuitGuild(sess *ClientSession, opCode byte, params map[byte]any) {
	guildID, err := db.QuitGuild(sess.dbAccountID)
	if err != nil {
		log.Printf("[PHOTON] QuitGuild user=%s error: %v", sess.userID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] QuitGuild user=%s guild=%d", sess.userID, guildID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opExpellFromGuild(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID := int64(asInt32(arr[0]))
	targetCharID := int64(asInt32(arr[1]))
	g, _ := db.GetGuild(guildID)
	targetAccountID, err := db.ExpelFromGuild(guildID, sess.dbAccountID, targetCharID)
	if err != nil {
		log.Printf("[PHOTON] ExpellFromGuild user=%s guild=%d target=%d error: %v", sess.userID, guildID, targetCharID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] ExpellFromGuild user=%s guild=%d target=%d", sess.userID, guildID, targetCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(targetCharID)})
	guildName := ""
	if g != nil {
		guildName = g.Name
	}
	for _, other := range s.sessionsByAccount(targetAccountID) {
		s.sendEventTo(other, EvGuildExpelled, map[byte]any{0: int32(guildID), 1: guildName, 2: false})
	}
}

func (s *EnetServer) opChangeGuildBanner(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID := int64(asInt32(arr[0]))
	banner := fmt.Sprintf("%v", arr[1])
	if err := db.ChangeGuildBanner(guildID, sess.dbAccountID, banner); err != nil {
		log.Printf("[PHOTON] ChangeGuildBanner user=%s guild=%d error: %v", sess.userID, guildID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] ChangeGuildBanner user=%s guild=%d", sess.userID, guildID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opCancelMyGuildApplication(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID := int64(asInt32(arr[0]))
	if err := db.CancelMyGuildApplication(guildID, sess.dbAccountID); err != nil {
		log.Printf("[PHOTON] CancelMyGuildApplication user=%s guild=%d error: %v", sess.userID, guildID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] CancelMyGuildApplication user=%s guild=%d", sess.userID, guildID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opInviteToGuild(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(guildInviteFailureETC)})
		return
	}
	guildID := int64(asInt32(arr[0]))
	targetCharID := int64(asInt32(arr[1]))
	g, gErr := db.GetGuild(guildID)
	target, tErr := db.GetCharacterByID(targetCharID)
	if err := db.InviteToGuild(guildID, sess.dbAccountID, targetCharID); err != nil {
		result := guildInviteFailureETC
		switch {
		case errors.Is(err, db.ErrNotGuildManager):
			result = guildInviteInsufficientRight
		case errors.Is(err, db.ErrGuildFull):
			result = guildInviteInsufficientVacancy
		case errors.Is(err, db.ErrAlreadyInGuild):
			result = guildInviteAlreadyHasGuild
		}
		log.Printf("[PHOTON] InviteToGuild user=%s guild=%d target=%d error: %v", sess.userID, guildID, targetCharID, err)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(result)})
		return
	}
	log.Printf("[PHOTON] InviteToGuild user=%s guild=%d target=%d", sess.userID, guildID, targetCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(guildInviteSuccess)})
	if gErr == nil && tErr == nil {
		for _, other := range s.sessionsByAccount(target.AccountID) {
			s.sendEventTo(other, EvGuildInvitation, map[byte]any{0: int32(guildID), 1: sess.charName, 2: g.Name})
		}
	}
}

func (s *EnetServer) opGetMyGuildInvitations(sess *ClientSession, opCode byte, params map[byte]any) {
	invites, err := db.GetMyGuildInvitations(sess.dbAccountID)
	if err != nil {
		log.Printf("[PHOTON] GetMyGuildInvitations user=%s error: %v", sess.userID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	result := map[byte]any{}
	for i, inv := range invites {
		if i > 255 {
			break
		}
		result[byte(i)] = map[int]any{
			0: int32(inv.GuildID),
			1: inv.MasterName,
			2: inv.GuildName,
			3: int32(inv.InviterCharID),
		}
	}
	log.Printf("[PHOTON] GetMyGuildInvitations user=%s → %d invitations", sess.userID, len(invites))
	s.sendOpResponse(sess, opCode, 0, result)
}

func (s *EnetServer) opProcessGuildInvitations(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	entries, _ := arr[0].(map[any]any)
	var joined *domain.Guild
	for _, v := range entries {
		entry, ok := v.(map[any]any)
		if !ok {
			continue
		}
		guildIDRaw, ok := hashLookup(entry, 0)
		if !ok {
			continue
		}
		guildID := int64(asInt32(guildIDRaw))
		accept := false
		if raw, ok := hashLookup(entry, 1); ok {
			accept, _ = raw.(bool)
		}
		if err := db.ProcessGuildInvitation(guildID, sess.dbAccountID, accept); err != nil {
			log.Printf("[PHOTON] ProcessGuildInvitations user=%s guild=%d error: %v", sess.userID, guildID, err)
			continue
		}
		if accept && joined == nil {
			if g, err := db.GetGuild(guildID); err == nil {
				joined = g
			}
		}
	}
	if joined != nil {
		log.Printf("[PHOTON] ProcessGuildInvitations user=%s → joined guild=%d", sess.userID, joined.GuildID)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: guildToHashtable(joined)})
		return
	}
	log.Printf("[PHOTON] ProcessGuildInvitations user=%s → no acceptance", sess.userID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opNotifyGuildDismissal(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] NotifyGuildDismissal user=%s (unexpected, client build never sends this op)", sess.userID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}

func (s *EnetServer) opAssignGuildManager(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID := int64(asInt32(arr[0]))
	targetCharID := int64(asInt32(arr[1]))
	targetAccountID, err := db.SetGuildManager(guildID, sess.dbAccountID, targetCharID, true)
	if err != nil {
		log.Printf("[PHOTON] AssignGuildManager user=%s guild=%d target=%d error: %v", sess.userID, guildID, targetCharID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] AssignGuildManager user=%s guild=%d target=%d", sess.userID, guildID, targetCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(targetCharID)})
	for _, other := range s.sessionsByAccount(targetAccountID) {
		s.sendEventTo(other, EvGuildManagerAssigned, map[byte]any{0: int32(guildID)})
	}
}

func (s *EnetServer) opDischargeGuildManager(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) < 2 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID := int64(asInt32(arr[0]))
	targetCharID := int64(asInt32(arr[1]))
	targetAccountID, err := db.SetGuildManager(guildID, sess.dbAccountID, targetCharID, false)
	if err != nil {
		log.Printf("[PHOTON] DischargeGuildManager user=%s guild=%d target=%d error: %v", sess.userID, guildID, targetCharID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] DischargeGuildManager user=%s guild=%d target=%d", sess.userID, guildID, targetCharID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(targetCharID)})
	for _, other := range s.sessionsByAccount(targetAccountID) {
		s.sendEventTo(other, EvGuildManagerDischarged, map[byte]any{0: int32(guildID)})
	}
}

func (s *EnetServer) opGetGuildManagerList(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[ParamObjArray])
	if len(arr) == 0 {
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	guildID := int64(asInt32(arr[0]))
	managers, err := db.GetGuildManagers(guildID)
	if err != nil {
		log.Printf("[PHOTON] GetGuildManagerList user=%s guild=%d error: %v", sess.userID, guildID, err)
		s.sendOpResponse(sess, opCode, -1, map[byte]any{})
		return
	}
	ht := map[int]any{}
	for i, charID := range managers {
		ht[i] = int32(charID)
	}
	log.Printf("[PHOTON] GetGuildManagerList user=%s guild=%d → %d managers", sess.userID, guildID, len(managers))
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: ht})
}
