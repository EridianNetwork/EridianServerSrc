package photon

import (
	"fmt"
	"log"
	"strconv"

	"eridian/internal/db"
)

func (s *EnetServer) opAuthenticate(sess *ClientSession, opCode byte, params map[byte]any) {
	flags := int32(1)
	if v, ok := params[ParamFlags]; ok {
		flags = asInt32(v)
	}
	appID, _ := params[ParamAppID].(string)
	appVer, _ := params[ParamAppVersion].(string)

	if flags == 2 {
		claimedID := int64(asInt32(params[110]))
		if claimedID <= 0 {
			if tok, _ := params[ParamToken].(string); tok != "" {
				if id, err := strconv.ParseInt(tok, 10, 64); err == nil {
					claimedID = id
				}
			}
		}
		if claimedID <= 0 {
			log.Printf("[PHOTON] Authenticate(online) REJECTED missing accountID")
			s.sendOpResponse(sess, opCode, -1, map[byte]any{})
			return
		}
		dbID := claimedID - 100000
		if dbID <= 0 {
			dbID = claimedID
		}
		log.Printf("[PHOTON] Authenticate(online) accountID=%d dbID=%d app=%s ver=%s", claimedID, dbID, appID, appVer)
		sess.accountID = claimedID
		sess.dbAccountID = dbID
		if sess.userID == "" {
			sess.userID = fmt.Sprintf("acct_%d", claimedID)
		}
		if sess.accountUsername == "" {
			if acc, err := db.GetAccountByID(dbID); err == nil && acc != nil {
				sess.accountUsername = acc.Username
			}
		}
		sess.guildTag = guildTagFor(dbID)
		sess.authenticated = true
		s.sendOpResponse(sess, opCode, 0, map[byte]any{
			ParamAccountID: int32(claimedID),
		})
		return
	}

	username := "player"
	if v, ok := params[ParamUsername]; ok {
		username = fmt.Sprintf("%v", v)
	}

	password := ""
	if v, ok := params[ParamPassword]; ok {
		password = fmt.Sprintf("%v", v)
	}

	// Auto create on first login for an unknown username, reeplace the deleted
	// db.GetOrCreateAccount which never saved the submitted password and left permanent
	// passwordless rows, refetch via GetAccountByUsername, not GetAccountByID
	account, err := db.GetAccountByUsername(username)
	if err != nil {
		if password == "" {
			// Same "no ghost account" reasoning the old bug was fixed for, refuse to create one with
			// nothing to check a future login against.
			log.Printf("[PHOTON] Authenticate: unknown user=%s, no password given, not auto-creating", username)
			s.sendOpResponse(sess, opCode, -3, map[byte]any{ParamAuthFailReason: "Unknown username or password."})
			return
		}
		if _, createErr := db.CreateAccount(username, password, ""); createErr != nil {
			log.Printf("[PHOTON] Authenticate: failed to auto-create user=%s: %v", username, createErr)
			s.sendOpResponse(sess, opCode, -3, map[byte]any{ParamAuthFailReason: "Unknown username or password."})
			return
		}
		account, err = db.GetAccountByUsername(username)
		if err != nil {
			log.Printf("[PHOTON] Authenticate: failed to load auto-created user=%s: %v", username, err)
			s.sendOpResponse(sess, opCode, -3, map[byte]any{ParamAuthFailReason: "Unknown username or password."})
			return
		}
		log.Printf("[PHOTON] Authenticate: auto-created new account user=%s dbID=%d", username, account.AccountID)
	}

	// Validate password
	if !db.CheckPassword(account, password) {
		log.Printf("[PHOTON] Authenticate: invalid password for user=%s", username)
		s.sendOpResponse(sess, opCode, -2, map[byte]any{ParamAuthFailReason: "Unknown username or password."})
		return
	}

	if !account.CanPlay {
		log.Printf("[PHOTON] Authenticate: user=%s rejected (can_play=false, is_tester=%v)", username, account.IsTester)
		s.sendOpResponse(sess, opCode, -4, map[byte]any{ParamAuthFailReason: account.CanPlayRejectMessage()})
		return
	}

	// Server full gate..: only a NEW account is refused entry, one already connected (second peer, relaunch
	// before its old session timed out) never counts against the cap twice.
	if s.MaxPlayersTotal > 0 && len(s.sessionsByAccount(account.AccountID)) == 0 && s.LivePlayerCount() >= s.MaxPlayersTotal {
		log.Printf("[PHOTON] Authenticate: user=%s rejected (server full %d/%d)", username, s.LivePlayerCount(), s.MaxPlayersTotal)
		s.sendOpResponse(sess, opCode, -4, map[byte]any{ParamAuthFailReason: "Server is full. Please try again later."})
		return
	}

	dbID := account.AccountID
	accountID := dbID + 100000
	delete(s.accountMapGen, dbID)
	_ = db.UpdateAccountToken(dbID, fmt.Sprintf("tok_%d", dbID))
	log.Printf("[PHOTON] Authenticate(main) user=%s dbID=%d photonID=%d app=%s ver=%s",
		username, dbID, accountID, appID, appVer)
	sess.accountID = accountID
	sess.dbAccountID = dbID
	sess.userID = account.Username
	sess.accountUsername = account.Username
	sess.authenticated = true

	advertisedHost := s.AdvertisedHost
	if advertisedHost == "" {
		advertisedHost = s.host // fallback to bind address
	}
	serverAddr := fmt.Sprintf("%s:%d", advertisedHost, s.port)
	resp := map[byte]any{
		ParamAccountID: int32(accountID),
		110:            int32(accountID),
		221:            fmt.Sprintf("%d", accountID),
		108:            serverAddr,
	}

	if s.ServerVersionID != "" {
		resp[220] = s.ServerVersionID
	}
	s.sendOpResponse(sess, opCode, 0, resp)
}

func (s *EnetServer) opGpkAuth(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] GpkAuth user=%s", sess.userID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}
