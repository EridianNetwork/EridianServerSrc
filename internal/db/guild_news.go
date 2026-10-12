package db

import (
	"database/sql"
	"math/rand"
)

type GuildNews struct {
	PosterAccountID   int64
	PosterCharacterID int64
	Reason            int
	Objective         sql.NullInt64
	PosterName        sql.NullString
	ObjectiveName     sql.NullString
}

var guildNewsTemplates = []struct {
	reason        int
	objective     int64
	objectiveName string
}{
	{1, 0, ""},                 // Join
	{3, 25, ""},                // LevelUp (objective = new level)
	{4, 0, "Legendary weapon"}, // Loot
	{5, 12, "First Blood"},     // Achieve (objective = achievement id)
	{8, 6, ""},                 // GuildLevelUp (objective = new guild level)
}

// returns a guilds news feed for op 78 (GetMyGuildNews),
// seeding a handful of random entries from real guild members the first
// time a guild has none
func GetGuildNews(guildID int64) ([]GuildNews, error) {
	if err := seedGuildNews(guildID); err != nil {
		return nil, err
	}

	rows, err := DB().Query(`
		SELECT poster_account_id, poster_character_id, reason, objective, poster_name, objective_name
		FROM guild_news WHERE guild_id = ? ORDER BY created_at DESC LIMIT 20`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var news []GuildNews
	for rows.Next() {
		var n GuildNews
		if err := rows.Scan(&n.PosterAccountID, &n.PosterCharacterID, &n.Reason, &n.Objective, &n.PosterName, &n.ObjectiveName); err != nil {
			return nil, err
		}
		news = append(news, n)
	}
	return news, rows.Err()
}

func AddGuildLevelUpNews(guildID, posterAccountID, posterCharacterID int64, posterName string, newLevel int) error {
	_, err := DB().Exec(`
		INSERT INTO guild_news (guild_id, poster_account_id, poster_character_id, reason, objective, poster_name)
		VALUES (?, ?, ?, 8, ?, ?)`,
		guildID, posterAccountID, posterCharacterID, newLevel, posterName)
	return err
}


func seedGuildNews(guildID int64) error {
	var count int
	if err := DB().QueryRow(`SELECT COUNT(*) FROM guild_news WHERE guild_id = ?`, guildID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	rows, err := DB().Query(`SELECT account_id FROM guild_members WHERE guild_id = ?`, guildID)
	if err != nil {
		return err
	}
	var memberIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		memberIDs = append(memberIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(memberIDs) == 0 {
		return nil
	}

	n := 3 + rand.Intn(3)
	for i := 0; i < n; i++ {
		accountID := memberIDs[rand.Intn(len(memberIDs))]
		chars, err := GetCharactersByAccount(accountID)
		if err != nil || len(chars) == 0 {
			continue
		}
		ch := chars[rand.Intn(len(chars))]
		t := guildNewsTemplates[rand.Intn(len(guildNewsTemplates))]

		var objective any
		if t.objective != 0 {
			objective = t.objective
		}
		var objectiveName any
		if t.objectiveName != "" {
			objectiveName = t.objectiveName
		}
		if _, err := DB().Exec(`
			INSERT INTO guild_news (guild_id, poster_account_id, poster_character_id, reason, objective, poster_name, objective_name)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			guildID, accountID, ch.CharID, t.reason, objective, ch.Name, objectiveName); err != nil {
			return err
		}
	}
	return nil
}
