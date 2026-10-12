package db

import (
	"time"

	"eridian/internal/domain"
)

// returns every achievement_progress row for a character, keyed by achievement_id
func GetAchievementProgress(charID int64) (map[int]domain.AchievementProgress, error) {
	rows, err := DB().Query(`SELECT char_id, achievement_id, stage, progress, updated_at FROM achievement_progress WHERE char_id = ?`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]domain.AchievementProgress{}
	for rows.Next() {
		var p domain.AchievementProgress
		if err := rows.Scan(&p.CharID, &p.AchievementID, &p.Stage, &p.Progress, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out[p.AchievementID] = p
	}
	return out, rows.Err()
}

// adds delta to achievementID counter (creating the row at 0 first if his is the first progress)
func IncrementAchievementProgress(charID int64, achievementID, delta int) (int, error) {
	now := time.Now().Unix()
	_, err := DB().Exec(`
		INSERT INTO achievement_progress (char_id, achievement_id, progress, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(char_id, achievement_id) DO UPDATE SET
			progress = progress + excluded.progress,
			updated_at = excluded.updated_at`,
		charID, achievementID, delta, now)
	if err != nil {
		return 0, err
	}
	row := DB().QueryRow(`SELECT progress FROM achievement_progress WHERE char_id = ? AND achievement_id = ?`, charID, achievementID)
	var progress int
	if err := row.Scan(&progress); err != nil {
		return 0, err
	}
	return progress, nil
}

func UpdateAchievementStage(charID int64, achievementID, newStage int) error {
	_, err := DB().Exec(`UPDATE achievement_progress SET stage = MAX(stage, ?) WHERE char_id = ? AND achievement_id = ?`,
		newStage, charID, achievementID)
	return err
}

// overwrites achievementID progress with an explicit value instead of adding a delta
func SetAchievementBitmask(charID int64, achievementID, bitmask, newStage int) error {
	now := time.Now().Unix()
	_, err := DB().Exec(`
		INSERT INTO achievement_progress (char_id, achievement_id, stage, progress, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(char_id, achievement_id) DO UPDATE SET
			progress = excluded.progress,
			stage = MAX(stage, excluded.stage),
			updated_at = excluded.updated_at`,
		charID, achievementID, newStage, bitmask, now)
	return err
}
