package db

import (
	"database/sql"
	"errors"

	"eridian/internal/domain"
)

// rejects applications to guilds without vacancies
var ErrGuildFull = errors.New("guild has no vacancy")

var (
	ErrAlreadyInGuild     = errors.New("account already belongs to a guild")
	ErrGuildNameTaken     = errors.New("guild name already exists")
	ErrNotGuildMaster     = errors.New("account is not the guild's master")
	ErrNotGuildManager    = errors.New("account has no guild management rights")
	ErrNotGuildMember     = errors.New("account is not a member of this guild")
	ErrTargetNotInGuild   = errors.New("target character is not in this guild")
	ErrCannotExpelSelf    = errors.New("master cannot expel themselves")
	ErrMasterMustTransfer = errors.New("guild master must transfer mastery before quitting")
)


const (
	GuildRoleMember  = 0
	GuildRoleManager = 1
)

const guildColumns = `guild_id, name, banner, level, experience, member_count, has_vacancy,
       free_skill_counts, max_member_count, master_account_id, master_character_id, preference`

func scanGuild(row interface{ Scan(...any) error }) (*domain.Guild, error) {
	g := &domain.Guild{}
	if err := row.Scan(
		&g.GuildID, &g.Name, &g.Banner, &g.Level, &g.Experience, &g.MemberCount, &g.HasVacancy,
		&g.FreeSkillCounts, &g.MaxMemberCount, &g.MasterAccountID, &g.MasterCharacterID, &g.Preference,
	); err != nil {
		return nil, err
	}
	return g, nil
}

// return a guild information by ID
func GetGuild(guildID int64) (*domain.Guild, error) {
	return scanGuild(DB().QueryRow(
		`SELECT `+guildColumns+` FROM guilds WHERE guild_id = ?`, guildID))
}

// records a membership application
func ApplyToGuild(guildID, accountID, charID int64) error {
	g, err := GetGuild(guildID)
	if err != nil {
		return err
	}
	if !g.HasVacancy || g.MemberCount >= int32(g.MaxMemberCount) {
		return ErrGuildFull
	}
	_, err = DB().Exec(`
		INSERT INTO guild_applications (guild_id, account_id, char_id) VALUES (?, ?, ?)
		ON CONFLICT(guild_id, account_id) DO UPDATE SET char_id = excluded.char_id,
		    created_at = datetime('now')`, guildID, accountID, charID)
	return err
}


func GetAccountGuildID(accountID int64) (int64, error) {
	var id int64
	err := DB().QueryRow(`
		SELECT guild_id FROM guilds WHERE master_account_id = ?
		UNION
		SELECT guild_id FROM guild_members WHERE account_id = ?
		LIMIT 1`, accountID, accountID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// is pending membership application
type GuildApplicant struct {
	AccountID int64
	CharID    int64
}

func GetGuildApplicants(guildID int64) ([]GuildApplicant, error) {
	rows, err := DB().Query(`
		SELECT account_id, char_id FROM guild_applications
		WHERE guild_id = ? ORDER BY created_at`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []GuildApplicant
	for rows.Next() {
		var a GuildApplicant
		if err := rows.Scan(&a.AccountID, &a.CharID); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

func GetMyGuildApplications(accountID int64) ([]int64, error) {
	rows, err := DB().Query(`
		SELECT guild_id FROM guild_applications
		WHERE account_id = ? ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var guildIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		guildIDs = append(guildIDs, id)
	}
	return guildIDs, rows.Err()
}


func ApproveGuildApplication(guildID, accountID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`INSERT INTO guild_members (guild_id, account_id, role) VALUES (?, ?, 0)
		 ON CONFLICT(guild_id, account_id) DO NOTHING`, guildID, accountID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM guild_applications WHERE guild_id = ? AND account_id = ?`, guildID, accountID); err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); n > 0 {
		if _, err := tx.Exec(`
			UPDATE guilds SET member_count = member_count + 1,
			    has_vacancy = CASE WHEN member_count + 1 >= max_member_count THEN 0 ELSE 1 END
			WHERE guild_id = ?`, guildID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// clears an application without adding the applicant
func RejectGuildApplication(guildID, accountID int64) error {
	_, err := DB().Exec(`DELETE FROM guild_applications WHERE guild_id = ? AND account_id = ?`, guildID, accountID)
	return err
}

func SearchGuilds(pref, limit int) ([]*domain.Guild, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT ` + guildColumns + ` FROM guilds`
	args := []any{}
	if pref != 0 && pref != 15 {
		query += ` WHERE (preference & ?) != 0`
		args = append(args, pref)
	}
	query += ` ORDER BY level DESC, member_count DESC LIMIT ?`
	args = append(args, limit)

	rows, err := DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var guilds []*domain.Guild
	for rows.Next() {
		g, err := scanGuild(rows)
		if err != nil {
			return nil, err
		}
		guilds = append(guilds, g)
	}
	return guilds, rows.Err()
}

func CreateGuild(accountID, charID int64, name, banner string, preference int32) (*domain.Guild, error) {
	if existing, err := GetAccountGuildID(accountID); err != nil {
		return nil, err
	} else if existing != 0 {
		return nil, ErrAlreadyInGuild
	}
	var nameExists bool
	if err := DB().QueryRow(`SELECT EXISTS(SELECT 1 FROM guilds WHERE name = ?)`, name).Scan(&nameExists); err != nil {
		return nil, err
	}
	if nameExists {
		return nil, ErrGuildNameTaken
	}

	tx, err := DB().Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO guilds (name, banner, master_account_id, master_character_id, preference)
		VALUES (?, ?, ?, ?, ?)`, name, banner, accountID, charID, preference)
	if err != nil {
		return nil, err
	}
	guildID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO guild_members (guild_id, account_id, role) VALUES (?, ?, ?)`,
		guildID, accountID, GuildRoleManager); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetGuild(guildID)
}

// slight naming issue here disbands a guild, only its owner may do this
func DismissGuild(guildID, accountID int64) error {
	g, err := GetGuild(guildID)
	if err != nil {
		return err
	}
	if int64(g.MasterAccountID) != accountID {
		return ErrNotGuildMaster
	}
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"guild_members", "guild_applications", "guild_invitations", "guild_news"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE guild_id = ?`, guildID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM guilds WHERE guild_id = ?`, guildID); err != nil {
		return err
	}
	return tx.Commit()
}


func ChangeGuildMaster(accountID, newMasterCharID int64) (newMasterAccountID int64, err error) {
	guildID, err := GetAccountGuildID(accountID)
	if err != nil {
		return 0, err
	}
	if guildID == 0 {
		return 0, ErrNotGuildMember
	}
	g, err := GetGuild(guildID)
	if err != nil {
		return 0, err
	}
	if int64(g.MasterAccountID) != accountID {
		return 0, ErrNotGuildMaster
	}
	ch, err := GetCharacterByID(newMasterCharID)
	if err != nil {
		return 0, err
	}
	var isMember bool
	if err := DB().QueryRow(`SELECT EXISTS(SELECT 1 FROM guild_members WHERE guild_id = ? AND account_id = ?)`,
		guildID, ch.AccountID).Scan(&isMember); err != nil {
		return 0, err
	}
	if !isMember {
		return 0, ErrTargetNotInGuild
	}

	tx, err := DB().Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE guilds SET master_account_id = ?, master_character_id = ? WHERE guild_id = ?`,
		ch.AccountID, newMasterCharID, guildID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE guild_members SET role = ? WHERE guild_id = ? AND account_id = ?`,
		GuildRoleManager, guildID, ch.AccountID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE guild_members SET role = ? WHERE guild_id = ? AND account_id = ?`,
		GuildRoleMember, guildID, accountID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return ch.AccountID, nil
}


func QuitGuild(accountID int64) (guildID int64, err error) {
	guildID, err = GetAccountGuildID(accountID)
	if err != nil {
		return 0, err
	}
	if guildID == 0 {
		return 0, ErrNotGuildMember
	}
	g, err := GetGuild(guildID)
	if err != nil {
		return 0, err
	}
	if int64(g.MasterAccountID) == accountID {
		return 0, ErrMasterMustTransfer
	}
	if err := removeGuildMember(guildID, accountID); err != nil {
		return 0, err
	}
	return guildID, nil
}


func ExpelFromGuild(guildID, byAccountID, targetCharID int64) (targetAccountID int64, err error) {
	isMgr, err := IsGuildManager(guildID, byAccountID)
	if err != nil {
		return 0, err
	}
	if !isMgr {
		return 0, ErrNotGuildManager
	}
	ch, err := GetCharacterByID(targetCharID)
	if err != nil {
		return 0, err
	}
	if ch.AccountID == byAccountID {
		return 0, ErrCannotExpelSelf
	}
	if err := removeGuildMember(guildID, ch.AccountID); err != nil {
		return 0, err
	}
	return ch.AccountID, nil
}

func removeGuildMember(guildID, accountID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM guild_members WHERE guild_id = ? AND account_id = ?`, guildID, accountID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTargetNotInGuild
	}
	if _, err := tx.Exec(`
		UPDATE guilds SET member_count = MAX(0, member_count - 1), has_vacancy = 1
		WHERE guild_id = ?`, guildID); err != nil {
		return err
	}
	return tx.Commit()
}

func ChangeGuildBanner(guildID, accountID int64, banner string) error {
	g, err := GetGuild(guildID)
	if err != nil {
		return err
	}
	if int64(g.MasterAccountID) != accountID {
		return ErrNotGuildMaster
	}
	_, err = DB().Exec(`UPDATE guilds SET banner = ? WHERE guild_id = ?`, banner, guildID)
	return err
}

func AddGuildExperience(guildID int64, delta int) (newExp int, err error) {
	tx, err := DB().Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE guilds SET experience = experience + ? WHERE guild_id = ?`, delta, guildID); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(`SELECT experience FROM guilds WHERE guild_id = ?`, guildID).Scan(&newExp); err != nil {
		return 0, err
	}
	return newExp, tx.Commit()
}

func SetGuildLevel(guildID int64, level int) error {
	_, err := DB().Exec(`UPDATE guilds SET level = ? WHERE guild_id = ?`, level, guildID)
	return err
}

// reports whether accountID is guildIDs master or a membe
func IsGuildManager(guildID, accountID int64) (bool, error) {
	g, err := GetGuild(guildID)
	if err != nil {
		return false, err
	}
	if int64(g.MasterAccountID) == accountID {
		return true, nil
	}
	var role int
	err = DB().QueryRow(`SELECT role FROM guild_members WHERE guild_id = ? AND account_id = ?`, guildID, accountID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return role >= GuildRoleManager, nil
}


func GetGuildManagers(guildID int64) ([]int64, error) {
	rows, err := DB().Query(`
		SELECT c.char_id FROM guild_members gm
		JOIN characters c ON c.account_id = gm.account_id
		WHERE gm.guild_id = ? AND gm.role >= ?`, guildID, GuildRoleManager)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetGuildManager promotes/demotes targetCharID between GuildRoleMember the guild's master/manafger
func SetGuildManager(guildID, byAccountID, targetCharID int64, manager bool) (targetAccountID int64, err error) {
	g, err := GetGuild(guildID)
	if err != nil {
		return 0, err
	}
	if int64(g.MasterAccountID) != byAccountID {
		return 0, ErrNotGuildMaster
	}
	ch, err := GetCharacterByID(targetCharID)
	if err != nil {
		return 0, err
	}
	role := GuildRoleMember
	if manager {
		role = GuildRoleManager
	}
	res, err := DB().Exec(`UPDATE guild_members SET role = ? WHERE guild_id = ? AND account_id = ?`, role, guildID, ch.AccountID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrTargetNotInGuild
	}
	return ch.AccountID, nil
}

func CancelMyGuildApplication(guildID, accountID int64) error {
	return RejectGuildApplication(guildID, accountID)
}

type GuildInvitation struct {
	GuildID       int64
	GuildName     string
	MasterName    string
	InviterCharID int64
}

func InviteToGuild(guildID, byAccountID, targetCharID int64) error {
	isMgr, err := IsGuildManager(guildID, byAccountID)
	if err != nil {
		return err
	}
	if !isMgr {
		return ErrNotGuildManager
	}
	g, err := GetGuild(guildID)
	if err != nil {
		return err
	}
	if !g.HasVacancy || g.MemberCount >= int32(g.MaxMemberCount) {
		return ErrGuildFull
	}
	ch, err := GetCharacterByID(targetCharID)
	if err != nil {
		return err
	}
	if existing, err := GetAccountGuildID(ch.AccountID); err != nil {
		return err
	} else if existing != 0 {
		return ErrAlreadyInGuild
	}
	inviterCharID := int64(0)
	if byChars, err := GetCharactersByAccount(byAccountID); err == nil && len(byChars) > 0 {
		inviterCharID = byChars[0].CharID
	}
	_, err = DB().Exec(`
		INSERT INTO guild_invitations (guild_id, account_id, char_id, inviter_char_id) VALUES (?, ?, ?, ?)
		ON CONFLICT(guild_id, account_id) DO UPDATE SET inviter_char_id = excluded.inviter_char_id,
		    created_at = datetime('now')`, guildID, ch.AccountID, targetCharID, inviterCharID)
	return err
}

// GetMyGuildInvitations lists pending invitations addressed to accountID
func GetMyGuildInvitations(accountID int64) ([]GuildInvitation, error) {
	rows, err := DB().Query(`
		SELECT gi.guild_id, g.name, g.master_account_id, gi.inviter_char_id
		FROM guild_invitations gi JOIN guilds g ON g.guild_id = gi.guild_id
		WHERE gi.account_id = ? ORDER BY gi.created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []GuildInvitation
	for rows.Next() {
		var inv GuildInvitation
		var masterAccountID int64
		if err := rows.Scan(&inv.GuildID, &inv.GuildName, &masterAccountID, &inv.InviterCharID); err != nil {
			return nil, err
		}
		if chars, err := GetCharactersByAccount(masterAccountID); err == nil && len(chars) > 0 {
			inv.MasterName = chars[0].Name
		}
		invites = append(invites, inv)
	}
	return invites, rows.Err()
}


func ProcessGuildInvitation(guildID, accountID int64, accept bool) error {
	var exists bool
	if err := DB().QueryRow(`SELECT EXISTS(SELECT 1 FROM guild_invitations WHERE guild_id = ? AND account_id = ?)`,
		guildID, accountID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrTargetNotInGuild
	}
	if accept {
		if err := ApproveGuildApplication(guildID, accountID); err != nil {
			return err
		}
	}
	_, err := DB().Exec(`DELETE FROM guild_invitations WHERE guild_id = ? AND account_id = ?`, guildID, accountID)
	return err
}

func GetGuildMemberAccountIDs(guildID int64) ([]int64, error) {
	rows, err := DB().Query(`SELECT account_id FROM guild_members WHERE guild_id = ?`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
