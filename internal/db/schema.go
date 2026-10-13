package db

import (
	"database/sql"
	"log"
)


func tableColumns(table string) (map[string]bool, error) {
	rows, err := DB().Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
    account_id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL DEFAULT '',
    is_banned INTEGER NOT NULL DEFAULT 0,
    is_gm INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_login TEXT
);

CREATE TABLE IF NOT EXISTS characters (
    char_id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    level INTEGER NOT NULL DEFAULT 1,
    experience INTEGER NOT NULL DEFAULT 0,
    class_type INTEGER NOT NULL DEFAULT 0,
    health REAL NOT NULL DEFAULT 100.0,
    shield REAL NOT NULL DEFAULT 100.0,
    position_x REAL NOT NULL DEFAULT 0.0,
    position_y REAL NOT NULL DEFAULT 0.0,
    position_z REAL NOT NULL DEFAULT 0.0,
    rotation REAL NOT NULL DEFAULT 0.0,
    play_time INTEGER NOT NULL DEFAULT 0,
    money INTEGER NOT NULL DEFAULT 0,
    eridium INTEGER NOT NULL DEFAULT 0,
    seraph_crystals INTEGER NOT NULL DEFAULT 0,
    skin_color INTEGER NOT NULL DEFAULT 0,
    eye_color INTEGER NOT NULL DEFAULT 0,
    hair_color INTEGER NOT NULL DEFAULT 0,
    hair_style INTEGER NOT NULL DEFAULT 0,
    face_style INTEGER NOT NULL DEFAULT 0,
    head_style INTEGER NOT NULL DEFAULT 0,
    deco_style INTEGER NOT NULL DEFAULT 0,
    is_deleted INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

CREATE TABLE IF NOT EXISTS inventory (
    item_id INTEGER PRIMARY KEY AUTOINCREMENT,
    char_id INTEGER NOT NULL,
    slot INTEGER NOT NULL DEFAULT 0,
    resource_id INTEGER NOT NULL DEFAULT 0,
    quantity INTEGER NOT NULL DEFAULT 1,
    flags INTEGER NOT NULL DEFAULT 0,
    rarity INTEGER NOT NULL DEFAULT 0,
    damage REAL NOT NULL DEFAULT 0.0,
    accuracy REAL NOT NULL DEFAULT 0.0,
    fire_rate REAL NOT NULL DEFAULT 0.0,
    reload_speed REAL NOT NULL DEFAULT 0.0,
    magazine INTEGER NOT NULL DEFAULT 0,
    element_type INTEGER NOT NULL DEFAULT 0,
    element_damage REAL NOT NULL DEFAULT 0.0,
    element_chance REAL NOT NULL DEFAULT 0.0,
    parts INTEGER NOT NULL DEFAULT 0,
    seed INTEGER NOT NULL DEFAULT 0,
    is_equipped INTEGER NOT NULL DEFAULT 0,
    is_weapon INTEGER NOT NULL DEFAULT 1,
    -- Full ItemDNA fields (see ItemDNA.method_10 in the decompile) needed to
    -- serialize Shield/ClassMode/Outfit items, not just Weapon/Material.
    -- item_kind is XsdSettings.ItemType (Weapon=2, Shield=5, Material=6,
    -- ClassMode=8, Outfit=11). part_a..f are generic sub-DNA slots whose
    -- meaning depends on item_kind:
    --   Weapon:    a=weaponType
    --   Shield:    a=manufacturer b=bodyIdx c=batteryIdx d=capacitorIdx e=accessoryIdx f=materialIdx
    --   ClassMode: a=cmType b=adapterIdx c=bodyIdx d=chassisIdx e=drumIdx f=materialIdx
    --   Outfit:    a=avatarType b=avatarSubpart c=index
    -- slot doubles as the equip sub-slot when is_equipped=1 (weapon 0-3,
    -- outfit AvatarSubPart 0-7, shield/classmod always 0) instead of a
    -- backpack slot.
    item_kind INTEGER NOT NULL DEFAULT 2,
    level INTEGER NOT NULL DEFAULT 1,
    uid TEXT NOT NULL DEFAULT '',
    item_name TEXT NOT NULL DEFAULT '',
    generation_source INTEGER NOT NULL DEFAULT 0,
    price INTEGER NOT NULL DEFAULT 0,
    expiration INTEGER NOT NULL DEFAULT 0,
    owner_id INTEGER NOT NULL DEFAULT 0,
    part_a INTEGER NOT NULL DEFAULT 0,
    part_b INTEGER NOT NULL DEFAULT 0,
    part_c INTEGER NOT NULL DEFAULT 0,
    part_d INTEGER NOT NULL DEFAULT 0,
    part_e INTEGER NOT NULL DEFAULT 0,
    part_f INTEGER NOT NULL DEFAULT 0,
    description TEXT NOT NULL DEFAULT '',
    star_level INTEGER NOT NULL DEFAULT 0,
    star_experience INTEGER NOT NULL DEFAULT 0,
    subparts TEXT NOT NULL DEFAULT '',
    extra_effects TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (char_id) REFERENCES characters(char_id)
);

CREATE TABLE IF NOT EXISTS warehouse (
    item_id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    slot INTEGER NOT NULL DEFAULT 0,
    resource_id INTEGER NOT NULL DEFAULT 0,
    quantity INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

CREATE TABLE IF NOT EXISTS friends (
    account_id INTEGER NOT NULL,
    friend_id INTEGER NOT NULL,
    status INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (account_id, friend_id),
    FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    FOREIGN KEY (friend_id) REFERENCES accounts(account_id)
);

CREATE TABLE IF NOT EXISTS char_friends (
    char_id INTEGER NOT NULL,
    friend_id INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (char_id, friend_id),
    FOREIGN KEY (char_id) REFERENCES characters(char_id),
    FOREIGN KEY (friend_id) REFERENCES characters(char_id)
);

CREATE TABLE IF NOT EXISTS char_ignores (
    char_id INTEGER NOT NULL,
    ignored_id INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (char_id, ignored_id),
    FOREIGN KEY (char_id) REFERENCES characters(char_id),
    FOREIGN KEY (ignored_id) REFERENCES characters(char_id)
);

CREATE TABLE IF NOT EXISTS guilds (
    guild_id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    banner TEXT NOT NULL DEFAULT '',
    level INTEGER NOT NULL DEFAULT 1,
    experience INTEGER NOT NULL DEFAULT 0,
    member_count INTEGER NOT NULL DEFAULT 1,
    has_vacancy INTEGER NOT NULL DEFAULT 1,
    free_skill_counts INTEGER NOT NULL DEFAULT 0,
    max_member_count INTEGER NOT NULL DEFAULT 50,
    master_account_id INTEGER NOT NULL,
    master_character_id INTEGER NOT NULL DEFAULT 0,
    -- GuildPreference bitmask: Social=1, Quest=2, PvP=4, PvE=8
    preference INTEGER NOT NULL DEFAULT 15,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (master_account_id) REFERENCES accounts(account_id)
);

CREATE TABLE IF NOT EXISTS guild_applications (
    guild_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL,
    char_id INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (guild_id, account_id),
    FOREIGN KEY (guild_id) REFERENCES guilds(guild_id),
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

CREATE TABLE IF NOT EXISTS guild_members (
    guild_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL,
    role INTEGER NOT NULL DEFAULT 0,
    joined_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (guild_id, account_id),
    FOREIGN KEY (guild_id) REFERENCES guilds(guild_id),
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

-- Master/manager-initiated invitations (as opposed to guild_applications,
-- which is the player-initiated "I want to join" direction). Mirrors the
-- client's Invitation(Hashtable) constructor: {0:guildId, 1:masterNick,
-- 2:guildName, 3:inviterCharacterId}, inviter_char_id is stored so
-- ProcessGuildInvitations can echo it back per InvitationReply.
CREATE TABLE IF NOT EXISTS guild_invitations (
    guild_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL,
    char_id INTEGER NOT NULL DEFAULT 0,
    inviter_char_id INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (guild_id, account_id),
    FOREIGN KEY (guild_id) REFERENCES guilds(guild_id),
    FOREIGN KEY (account_id) REFERENCES accounts(account_id)
);

-- Mirrors the client's GuildNews(Hashtable) constructor (keys 0-5: poster
-- account/char id, GuildNewsReason, optional objective int/name). No event
-- pipeline records real guild activity yet, so rows are randomly seeded
-- from actual guild members the first time a guild's news is requested
-- (see db.GetGuildNews) rather than left permanently empty.
CREATE TABLE IF NOT EXISTS guild_news (
    news_id INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id INTEGER NOT NULL,
    poster_account_id INTEGER NOT NULL,
    poster_character_id INTEGER NOT NULL,
    reason INTEGER NOT NULL DEFAULT 0,
    objective INTEGER,
    poster_name TEXT,
    objective_name TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (guild_id) REFERENCES guilds(guild_id)
);

CREATE TABLE IF NOT EXISTS mail (
    mail_id INTEGER PRIMARY KEY AUTOINCREMENT,
    sender_id INTEGER NOT NULL,
    receiver_id INTEGER NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    item_type INTEGER NOT NULL DEFAULT 0,
    item_data TEXT NOT NULL DEFAULT '',
    is_read INTEGER NOT NULL DEFAULT 0,
    is_claimed INTEGER NOT NULL DEFAULT 0,
    sent_at TEXT NOT NULL DEFAULT (datetime('now')),
    -- Client MailType enum: 0 = System, 1 = User. Defaults to User since
    -- CreateMail (player-sent) is the common path; CreateMailWithItem
    -- (GM/system reward mail) sets it to 0 explicitly.
    mail_type INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY (sender_id) REFERENCES characters(char_id),
    FOREIGN KEY (receiver_id) REFERENCES characters(char_id)
);

-- MessagingService (client): live typed messages (friend_request,
-- lobby_invite, group_invitation, ...) pushed as event 223 and/or fetched via
-- op 210 RetrieveMessages. Distinct from the mail table, which the client never reads
-- for invite flows (confirmed by decompile: LobbyService/GroupService/
-- FriendsListsService all register listeners on MessagingService, not mail).
CREATE TABLE IF NOT EXISTS messages (
    message_id INTEGER PRIMARY KEY AUTOINCREMENT,
    sender_id INTEGER NOT NULL,
    receiver_id INTEGER NOT NULL,
    type TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '',
    is_read INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (sender_id) REFERENCES characters(char_id),
    FOREIGN KEY (receiver_id) REFERENCES characters(char_id)
);

CREATE TABLE IF NOT EXISTS quest_progress (
    progress_id INTEGER PRIMARY KEY AUTOINCREMENT,
    char_id INTEGER NOT NULL,
    quest_id INTEGER NOT NULL,
    status INTEGER NOT NULL DEFAULT 0,
    current_task INTEGER NOT NULL DEFAULT 0,
    task_progress INTEGER NOT NULL DEFAULT 0,
    objectives TEXT NOT NULL DEFAULT '{}',
    started_at TEXT,
    completed_at TEXT,
    FOREIGN KEY (char_id) REFERENCES characters(char_id)
);

CREATE TABLE IF NOT EXISTS achievement_progress (
    char_id INTEGER NOT NULL,
    achievement_id INTEGER NOT NULL,
    stage INTEGER NOT NULL DEFAULT 0,
    progress INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (char_id, achievement_id),
    FOREIGN KEY (char_id) REFERENCES characters(char_id)
);

CREATE TABLE IF NOT EXISTS pvp_stats (
    char_id INTEGER PRIMARY KEY,
    wins INTEGER NOT NULL DEFAULT 0,
    losses INTEGER NOT NULL DEFAULT 0,
    kills INTEGER NOT NULL DEFAULT 0,
    deaths INTEGER NOT NULL DEFAULT 0,
    rating INTEGER NOT NULL DEFAULT 1000,
    FOREIGN KEY (char_id) REFERENCES characters(char_id)
);
`


func RunMigrations() error {
	_, err := DB().Exec(schema)
	return err
}


func MigrateAccountSettings() error {
	existing, err := tableColumns("accounts")
	if err != nil {
		return err
	}
	if existing["settings"] {
		return nil
	}

	_, err = DB().Exec("ALTER TABLE accounts ADD COLUMN settings TEXT NOT NULL DEFAULT ''")
	return err
}


func MigrateAccountTesterFlags() error {
	
	if _, err := DB().Exec("CREATE TABLE IF NOT EXISTS banned_ips (ip TEXT PRIMARY KEY, reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)"); err != nil {
		return err
	}
	existing, err := tableColumns("accounts")
	if err != nil {
		return err
	}
	if !existing["is_tester"] {
		if _, err := DB().Exec("ALTER TABLE accounts ADD COLUMN is_tester INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	if !existing["can_play"] {
		if _, err := DB().Exec("ALTER TABLE accounts ADD COLUMN can_play INTEGER NOT NULL DEFAULT 1"); err != nil {
			return err
		}
	}
	return nil
}


func MigratePvPStats() error {
	existing, err := tableColumns("pvp_stats")
	if err != nil {
		return err
	}
	if !existing["matches"] {
		if _, err := DB().Exec("ALTER TABLE pvp_stats ADD COLUMN matches INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	if !existing["honor"] {
		if _, err := DB().Exec("ALTER TABLE pvp_stats ADD COLUMN honor INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	return nil
}


func MigrateCharacterAvatar() error {
	existing, err := tableColumns("characters")
	if err != nil {
		return err
	}

	migrations := []struct{ col, def string }{
		{"avatar_parts", "TEXT NOT NULL DEFAULT '0,0,0,0,0,0,0,0'"},
		{"avatar_materials", "TEXT NOT NULL DEFAULT '0,0,0,0,0,0,0,0'"},
		{"avatar_id", "INTEGER NOT NULL DEFAULT 0"},
		{"skills", "TEXT NOT NULL DEFAULT '{}'"},
		{"backpack_size", "INTEGER NOT NULL DEFAULT 60"},
		{"ammo", "TEXT NOT NULL DEFAULT '{}'"},
		{"active_weapon_slot", "INTEGER NOT NULL DEFAULT 0"},
		{"settings", "TEXT NOT NULL DEFAULT '{}'"},
		{"last_seen", "TEXT NOT NULL DEFAULT ''"},
		{"energy", "INTEGER NOT NULL DEFAULT 20"},
		{"energy_updated_at", "TEXT NOT NULL DEFAULT ''"},
		{"last_town_playlist_id", "INTEGER NOT NULL DEFAULT 10000"},
	}
	lastSeenAdded := !existing["last_seen"]
	for _, m := range migrations {
		if !existing[m.col] {
			if _, err := DB().Exec("ALTER TABLE characters ADD COLUMN " + m.col + " " + m.def); err != nil {
				return err
			}
		}
	}
	if lastSeenAdded {
		if _, err := DB().Exec("UPDATE characters SET last_seen = updated_at WHERE last_seen = ''"); err != nil {
			return err
		}
	}

	if _, err := DB().Exec("UPDATE characters SET backpack_size = 60 WHERE backpack_size < 60"); err != nil {
		return err
	}
	return migrateGuilds()
}


func migrateGuilds() error {
	existing, err := tableColumns("guilds")
	if err != nil {
		return err
	}

	migrations := []struct{ col, def string }{
		{"banner", "TEXT NOT NULL DEFAULT ''"},
		{"level", "INTEGER NOT NULL DEFAULT 1"},
		{"experience", "INTEGER NOT NULL DEFAULT 0"},
		{"member_count", "INTEGER NOT NULL DEFAULT 1"},
		{"has_vacancy", "INTEGER NOT NULL DEFAULT 1"},
		{"free_skill_counts", "INTEGER NOT NULL DEFAULT 0"},
		{"max_member_count", "INTEGER NOT NULL DEFAULT 50"},
		{"master_account_id", "INTEGER NOT NULL DEFAULT 0"},
		{"master_character_id", "INTEGER NOT NULL DEFAULT 0"},
		{"preference", "INTEGER NOT NULL DEFAULT 15"},
	}
	for _, m := range migrations {
		if !existing[m.col] {
			if _, err := DB().Exec("ALTER TABLE guilds ADD COLUMN " + m.col + " " + m.def); err != nil {
				return err
			}
		}
	}
	return migrateMail()
}


func migrateMail() error {
	existing, err := tableColumns("mail")
	if err != nil {
		return err
	}

	migrations := []struct{ col, def string }{
		{"item_type", "INTEGER NOT NULL DEFAULT 0"},
		{"item_data", "TEXT NOT NULL DEFAULT ''"},
		{"mail_type", "INTEGER NOT NULL DEFAULT 1"},
	}
	for _, m := range migrations {
		if !existing[m.col] {
			if _, err := DB().Exec("ALTER TABLE mail ADD COLUMN " + m.col + " " + m.def); err != nil {
				return err
			}
		}
	}
	if err := migrateMailForeignKeys(); err != nil {
		return err
	}
	return migrateQuestProgress()
}


func migrateMailForeignKeys() error {
	rows, err := DB().Query(`PRAGMA foreign_key_list(mail)`)
	if err != nil {
		return err
	}
	stale := false
	for rows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			rows.Close()
			return err
		}
		if table == "accounts" {
			stale = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !stale {
		return nil
	}
	log.Printf("[DB] migrating mail table: FKs point at accounts, rebuilding to reference characters")
	_, err = DB().Exec(`
		CREATE TABLE mail_new (
			mail_id INTEGER PRIMARY KEY AUTOINCREMENT,
			sender_id INTEGER NOT NULL,
			receiver_id INTEGER NOT NULL,
			subject TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL DEFAULT '',
			item_type INTEGER NOT NULL DEFAULT 0,
			item_data TEXT NOT NULL DEFAULT '',
			is_read INTEGER NOT NULL DEFAULT 0,
			is_claimed INTEGER NOT NULL DEFAULT 0,
			sent_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (sender_id) REFERENCES characters(char_id),
			FOREIGN KEY (receiver_id) REFERENCES characters(char_id)
		);
		INSERT INTO mail_new (mail_id, sender_id, receiver_id, subject, body, item_type, item_data, is_read, is_claimed, sent_at)
			SELECT mail_id, sender_id, receiver_id, subject, body, item_type, item_data, is_read, is_claimed, sent_at
			FROM mail
			WHERE sender_id IN (SELECT char_id FROM characters) AND receiver_id IN (SELECT char_id FROM characters);
		DROP TABLE mail;
		ALTER TABLE mail_new RENAME TO mail;
	`)
	return err
}


func migrateQuestProgress() error {
	existing, err := tableColumns("quest_progress")
	if err != nil {
		return err
	}

	migrations := []struct{ col, def string }{
		{"current_task", "INTEGER NOT NULL DEFAULT 0"},
		{"task_progress", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, m := range migrations {
		if !existing[m.col] {
			if _, err := DB().Exec("ALTER TABLE quest_progress ADD COLUMN " + m.col + " " + m.def); err != nil {
				return err
			}
		}
	}
	return migrateInventory()
}

func migrateInventory() error {
	existing, err := tableColumns("inventory")
	if err != nil {
		return err
	}

	migrations := []struct{ col, def string }{
		{"item_kind", "INTEGER NOT NULL DEFAULT 2"},
		{"level", "INTEGER NOT NULL DEFAULT 1"},
		{"uid", "TEXT NOT NULL DEFAULT ''"},
		{"item_name", "TEXT NOT NULL DEFAULT ''"},
		{"generation_source", "INTEGER NOT NULL DEFAULT 0"},
		{"price", "INTEGER NOT NULL DEFAULT 0"},
		{"expiration", "INTEGER NOT NULL DEFAULT 0"},
		{"owner_id", "INTEGER NOT NULL DEFAULT 0"},
		{"part_a", "INTEGER NOT NULL DEFAULT 0"},
		{"part_b", "INTEGER NOT NULL DEFAULT 0"},
		{"part_c", "INTEGER NOT NULL DEFAULT 0"},
		{"part_d", "INTEGER NOT NULL DEFAULT 0"},
		{"part_e", "INTEGER NOT NULL DEFAULT 0"},
		{"part_f", "INTEGER NOT NULL DEFAULT 0"},
		{"description", "TEXT NOT NULL DEFAULT ''"},
		{"star_level", "INTEGER NOT NULL DEFAULT 0"},
		{"star_experience", "INTEGER NOT NULL DEFAULT 0"},
		{"subparts", "TEXT NOT NULL DEFAULT ''"},
		{"extra_effects", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, m := range migrations {
		if !existing[m.col] {
			if _, err := DB().Exec("ALTER TABLE inventory ADD COLUMN " + m.col + " " + m.def); err != nil {
				return err
			}
		}
	}
	return migrateWarehouse()
}


func migrateWarehouse() error {
	existing, err := tableColumns("warehouse")
	if err != nil {
		return err
	}

	migrations := []struct{ col, def string }{
		{"flags", "INTEGER NOT NULL DEFAULT 0"},
		{"rarity", "INTEGER NOT NULL DEFAULT 0"},
		{"damage", "REAL NOT NULL DEFAULT 0.0"},
		{"accuracy", "REAL NOT NULL DEFAULT 0.0"},
		{"fire_rate", "REAL NOT NULL DEFAULT 0.0"},
		{"reload_speed", "REAL NOT NULL DEFAULT 0.0"},
		{"magazine", "INTEGER NOT NULL DEFAULT 0"},
		{"element_type", "INTEGER NOT NULL DEFAULT 0"},
		{"element_damage", "REAL NOT NULL DEFAULT 0.0"},
		{"element_chance", "REAL NOT NULL DEFAULT 0.0"},
		{"parts", "INTEGER NOT NULL DEFAULT 0"},
		{"seed", "INTEGER NOT NULL DEFAULT 0"},
		{"is_weapon", "INTEGER NOT NULL DEFAULT 1"},
		{"item_kind", "INTEGER NOT NULL DEFAULT 2"},
		{"level", "INTEGER NOT NULL DEFAULT 1"},
		{"uid", "TEXT NOT NULL DEFAULT ''"},
		{"item_name", "TEXT NOT NULL DEFAULT ''"},
		{"generation_source", "INTEGER NOT NULL DEFAULT 0"},
		{"price", "INTEGER NOT NULL DEFAULT 0"},
		{"expiration", "INTEGER NOT NULL DEFAULT 0"},
		{"owner_id", "INTEGER NOT NULL DEFAULT 0"},
		{"part_a", "INTEGER NOT NULL DEFAULT 0"},
		{"part_b", "INTEGER NOT NULL DEFAULT 0"},
		{"part_c", "INTEGER NOT NULL DEFAULT 0"},
		{"part_d", "INTEGER NOT NULL DEFAULT 0"},
		{"part_e", "INTEGER NOT NULL DEFAULT 0"},
		{"part_f", "INTEGER NOT NULL DEFAULT 0"},
		{"description", "TEXT NOT NULL DEFAULT ''"},
		{"star_level", "INTEGER NOT NULL DEFAULT 0"},
		{"star_experience", "INTEGER NOT NULL DEFAULT 0"},
		{"subparts", "TEXT NOT NULL DEFAULT ''"},
		{"extra_effects", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, m := range migrations {
		if !existing[m.col] {
			if _, err := DB().Exec("ALTER TABLE warehouse ADD COLUMN " + m.col + " " + m.def); err != nil {
				return err
			}
		}
	}
	return nil
}
