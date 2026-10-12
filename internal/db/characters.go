package db

import (
	"errors"

	"eridian/internal/domain"
)

const charSelectCols = `
	char_id, account_id, name, level, experience, class_type, health, shield,
	position_x, position_y, position_z, rotation, play_time, money, eridium, seraph_crystals,
	skin_color, eye_color, hair_color, hair_style, face_style, head_style, deco_style,
	is_deleted, created_at, updated_at,
	COALESCE(avatar_parts, '0,0,0,0,0,0,0,0'),
	COALESCE(avatar_materials, '0,0,0,0,0,0,0,0'),
	COALESCE(avatar_id, 0),
	COALESCE(skills, '{}'),
	COALESCE(backpack_size, 60),
	COALESCE(ammo, '{}'),
	COALESCE(active_weapon_slot, 0),
	COALESCE(settings, '{}'),
	COALESCE(last_seen, ''),
	COALESCE(energy, 20),
	COALESCE(energy_updated_at, ''),
	COALESCE(last_town_playlist_id, 10000)`

func scanCharacter(row interface {
	Scan(...any) error
}, c *domain.Character) error {
	return row.Scan(
		&c.CharID, &c.AccountID, &c.Name, &c.Level, &c.Experience, &c.ClassType,
		&c.Health, &c.Shield, &c.PositionX, &c.PositionY, &c.PositionZ, &c.Rotation,
		&c.PlayTime, &c.Money, &c.Eridium, &c.SeraphCrystals,
		&c.SkinColor, &c.EyeColor, &c.HairColor, &c.HairStyle, &c.FaceStyle, &c.HeadStyle, &c.DecoStyle,
		&c.IsDeleted, &c.CreatedAt, &c.UpdatedAt,
		&c.AvatarParts, &c.AvatarMaterials, &c.AvatarID, &c.Skills,
		&c.BackpackSize, &c.Ammo, &c.ActiveWeaponSlot, &c.Settings, &c.LastSeen,
		&c.Energy, &c.EnergyUpdatedAt, &c.LastTownPlaylistID)
}

// saves client sided ui prewferences
func UpdateCharacterSettings(charID int64, settings string) error {
	_, err := DB().Exec("UPDATE characters SET settings=? WHERE char_id=?", settings, charID)
	return err
}

// this is again to do with friend. tells others were they last online 1 hour ago? 2? etc
func UpdateCharacterLastSeen(charID int64) error {
	_, err := DB().Exec("UPDATE characters SET last_seen=datetime('now') WHERE char_id=?", charID)
	return err
}

// responsible for playtime. i think onyl character screen relevant info here. not sure what'd happen if you removed or messed with it
func AddCharacterPlayTime(charID int64, seconds int) error {
	if seconds <= 0 {
		return nil
	}
	_, err := DB().Exec("UPDATE characters SET play_time=play_time+? WHERE char_id=?", seconds, charID)
	return err
}

// energy on the world map for inst
func SetCharacterEnergy(charID int64, energy int32) error {
	_, err := DB().Exec("UPDATE characters SET energy=?, energy_updated_at=datetime('now') WHERE char_id=?", energy, charID)
	return err
}

// grabs the last town the player was present in
func UpdateCharacterLastTown(charID int64, playlistID int32) error {
	_, err := DB().Exec("UPDATE characters SET last_town_playlist_id=? WHERE char_id=?", playlistID, charID)
	return err
}

// hardcoded backpack size for characters
const defaultBackpackSize = 60

func CreateCharacter(c *domain.Character) (int64, error) {
	res, err := DB().Exec(`
		INSERT INTO characters (account_id, name, class_type, skin_color, eye_color, hair_color, hair_style, face_style, head_style, deco_style, backpack_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.AccountID, c.Name, c.ClassType, c.SkinColor, c.EyeColor, c.HairColor, c.HairStyle, c.FaceStyle, c.HeadStyle, c.DecoStyle, defaultBackpackSize)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func CreateCharacterPhoton(accountID int64, name string, avatarType int, avatarID uint32, avatarParts, avatarMaterials, skills string) (int64, error) {
	res, err := DB().Exec(`
		INSERT INTO characters (account_id, name, class_type, avatar_id, avatar_parts, avatar_materials, skills, backpack_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		accountID, name, avatarType, avatarID, avatarParts, avatarMaterials, skills, defaultBackpackSize)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// returns character id
func GetCharacterByID(charID int64) (*domain.Character, error) {
	c := &domain.Character{}
	row := DB().QueryRow(`SELECT `+charSelectCols+` FROM characters WHERE char_id = ? AND is_deleted = 0`, charID)
	if err := scanCharacter(row, c); err != nil {
		return nil, err
	}
	return c, nil
}

// returns all characters belonging to a specific acc
func GetCharactersByAccount(accountID int64) ([]*domain.Character, error) {
	rows, err := DB().Query(`SELECT `+charSelectCols+` FROM characters WHERE account_id = ? AND is_deleted = 0`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chars []*domain.Character
	for rows.Next() {
		c := &domain.Character{}
		if err := scanCharacter(rows, c); err != nil {
			return nil, err
		}
		chars = append(chars, c)
	}
	return chars, rows.Err()
}

// look up a character by exact name (op152 - SearchForCharacter)
func GetCharacterByName(name string) (*domain.Character, error) {
	c := &domain.Character{}
	row := DB().QueryRow(`SELECT `+charSelectCols+` FROM characters WHERE name = ? AND is_deleted = 0`, name)
	if err := scanCharacter(row, c); err != nil {
		return nil, err
	}
	return c, nil
}

func GetTopPvPCharacters(limit int) ([]*domain.Character, error) {
	rows, err := DB().Query(`SELECT `+charSelectCols+` FROM characters WHERE is_deleted = 0
		ORDER BY COALESCE((SELECT rating FROM pvp_stats WHERE pvp_stats.char_id = characters.char_id), 1000) DESC,
			level DESC, experience DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chars []*domain.Character
	for rows.Next() {
		c := &domain.Character{}
		if err := scanCharacter(rows, c); err != nil {
			return nil, err
		}
		chars = append(chars, c)
	}
	return chars, rows.Err()
}

// account view of a character for the RCON tool (internal/photon/rcon.go)
// the full charSelectCols row plus the owning username
type CharacterSummary struct {
	CharID    int64  `json:"char_id"`
	AccountID int64  `json:"account_id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Level     int    `json:"level"`
	ClassType int    `json:"class_type"`
	PlayTime  int    `json:"play_time"`
	LastSeen  string `json:"last_seen"`
	CreatedAt string `json:"created_at"`
}


func ListCharacters(search string, limit, offset int, sortBy string, desc bool) ([]*CharacterSummary, int, error) {
	like := "%" + search + "%"
	orderBy := "c.level DESC, c.name ASC"
	if col, ok := map[string]string{
		"name":      "c.name COLLATE NOCASE",
		"level":     "c.level",
		"play_time": "c.play_time",
		"last_seen": "COALESCE(c.last_seen, '')",
	}[sortBy]; ok {
		dir := "ASC"
		if desc {
			dir = "DESC"
		}
		orderBy = col + " " + dir + ", c.name ASC, c.char_id ASC"
	}
	var total int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM characters c JOIN accounts a ON a.account_id = c.account_id
		WHERE c.is_deleted = 0 AND (c.name LIKE ? OR a.username LIKE ?)`, like, like).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := DB().Query(`SELECT c.char_id, c.account_id, a.username, c.name, c.level, c.class_type,
			c.play_time, COALESCE(c.last_seen, ''), c.created_at
		FROM characters c JOIN accounts a ON a.account_id = c.account_id
		WHERE c.is_deleted = 0 AND (c.name LIKE ? OR a.username LIKE ?)
		ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, like, like, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*CharacterSummary
	for rows.Next() {
		c := &CharacterSummary{}
		if err := rows.Scan(&c.CharID, &c.AccountID, &c.Username, &c.Name, &c.Level, &c.ClassType,
			&c.PlayTime, &c.LastSeen, &c.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func GetCharacterPvPRank(c *domain.Character) (int32, error) {
	st, err := GetPvPStats(c.CharID)
	if err != nil {
		return 0, err
	}
	rating := st.Rating
	if rating == 0 { // no pvp_stats row yet (never finished a match) same 1000 seed as a fresh row/GetTopPvPCharacters
		rating = 1000
	}
	var rank int32
	err = DB().QueryRow(`SELECT COUNT(*) + 1 FROM characters
		WHERE is_deleted = 0 AND COALESCE((SELECT rating FROM pvp_stats WHERE pvp_stats.char_id = characters.char_id), 1000) > ?`,
		rating).Scan(&rank)
	return rank, err
}

// used for updating the char position and rot
func UpdateCharacterPosition(charID int64, x, y, z, rot float64) error {
	_, err := DB().Exec("UPDATE characters SET position_x=?, position_y=?, position_z=?, rotation=?, updated_at=datetime('now') WHERE char_id=?",
		x, y, z, rot, charID)
	return err
}

// same as above except this is for currency, levels, etc
func UpdateCharacterStats(charID int64, level, exp, money, eridium int) error {
	_, err := DB().Exec("UPDATE characters SET level=?, experience=?, money=?, eridium=?, updated_at=datetime('now') WHERE char_id=?",
		level, exp, money, eridium, charID)
	return err
}

// this is jsut a soft delete, not a full deletion of a char
func DeleteCharacter(charID int64) error {
	_, err := DB().Exec("UPDATE characters SET is_deleted=1 WHERE char_id=?", charID)
	return err
}

// returns character count for a specific player acc
func CharacterCount(accountID int64) (int, error) {
	var count int
	err := DB().QueryRow("SELECT COUNT(*) FROM characters WHERE account_id=? AND is_deleted=0", accountID).Scan(&count)
	return count, err
}

func UpdateCharacter(c *domain.Character) error {
	_, err := DB().Exec(`
		UPDATE characters SET
			level=?, experience=?, class_type=?,
			health=?, shield=?,
			position_x=?, position_y=?, position_z=?, rotation=?,
			play_time=?, money=?, eridium=?, seraph_crystals=?,
			skin_color=?, eye_color=?, hair_color=?, hair_style=?,
			face_style=?, head_style=?, deco_style=?,
			updated_at=datetime('now')
		WHERE char_id=? AND is_deleted=0`,
		c.Level, c.Experience, c.ClassType,
		c.Health, c.Shield,
		c.PositionX, c.PositionY, c.PositionZ, c.Rotation,
		c.PlayTime, c.Money, c.Eridium, c.SeraphCrystals,
		c.SkinColor, c.EyeColor, c.HairColor, c.HairStyle,
		c.FaceStyle, c.HeadStyle, c.DecoStyle,
		c.CharID)
	return err
}

func SoftDeleteCharacter(charID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE characters SET is_deleted=1 WHERE char_id=?", charID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM inventory WHERE char_id=?", charID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM quest_progress WHERE char_id=?", charID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM pvp_stats WHERE char_id=?", charID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM achievement_progress WHERE char_id=?", charID); err != nil {
		return err
	}
	return tx.Commit()
}

func UpdateCharacterAvatar(charID int64, avatarParts, avatarMats string, avatarID uint32) error {
	_, err := DB().Exec("UPDATE characters SET avatar_parts=?, avatar_materials=?, avatar_id=?, updated_at=datetime('now') WHERE char_id=?",
		avatarParts, avatarMats, avatarID, charID)
	return err
}

// used to change chracter names
func UpdateCharacterNameDB(charID int64, name string) error {
	_, err := DB().Exec("UPDATE characters SET name=?, updated_at=datetime('now') WHERE char_id=?", name, charID)
	return err
}

// saves character skills into blobs
func UpdateCharacterSkills(charID int64, skills string) error {
	_, err := DB().Exec("UPDATE characters SET skills=?, updated_at=datetime('now') WHERE char_id=?", skills, charID)
	return err
}

func UpdateCharacterExpLevel(charID int64, level, exp int) error {
	_, err := DB().Exec("UPDATE characters SET level=?, experience=?, updated_at=datetime('now') WHERE char_id=?", level, exp, charID)
	return err
}

var ErrInsufficientFunds = errors.New("insufficient funds")

// Adjusts the money column on a character
func AddCharacterMoney(charID int64, delta int) error {
	if delta >= 0 {
		_, err := DB().Exec("UPDATE characters SET money = money + ? WHERE char_id = ?", delta, charID)
		return err
	}
	res, err := DB().Exec("UPDATE characters SET money = money + ? WHERE char_id = ? AND money >= ?", delta, charID, -delta)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInsufficientFunds
	}
	return nil
}

// read, plese read
func DeleteCharacterInventory(charID int64) error {
	_, err := DB().Exec("DELETE FROM inventory WHERE char_id=?", charID)
	return err
}

// will error out if an account does not own the character. important info for debugging a potential issue with acc
var ErrCharacterNotOwned = errors.New("character not found for this account")

// full account deletion, will also clear tables
func DeleteCharacterByAccountID(charID, accountID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM inventory WHERE char_id=?", []any{charID}},
		{"DELETE FROM quest_progress WHERE char_id=?", []any{charID}},
		{"DELETE FROM pvp_stats WHERE char_id=?", []any{charID}},
		{"DELETE FROM achievement_progress WHERE char_id=?", []any{charID}},
		{"DELETE FROM char_friends WHERE char_id=? OR friend_id=?", []any{charID, charID}},
		{"DELETE FROM char_ignores WHERE char_id=? OR ignored_id=?", []any{charID, charID}},
		{"DELETE FROM mail WHERE sender_id=? OR receiver_id=?", []any{charID, charID}},
		{"DELETE FROM messages WHERE sender_id=? OR receiver_id=?", []any{charID, charID}},
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(stmt.query, stmt.args...); err != nil {
			return err
		}
	}
	res, err := tx.Exec("DELETE FROM characters WHERE char_id=? AND account_id=?", charID, accountID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrCharacterNotOwned
	}
	return tx.Commit()
}
