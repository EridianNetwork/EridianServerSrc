package db

import (
	"database/sql"
	"time"

	"eridian/internal/domain"
)

const questProgressCols = `
	progress_id, char_id, quest_id, status, current_task, task_progress,
	objectives, COALESCE(started_at, ''), COALESCE(completed_at, '')`

func scanQuestProgress(row interface{ Scan(...any) error }, p *domain.QuestProgress) error {
	return row.Scan(
		&p.ProgressID, &p.CharID, &p.QuestID, &p.Status, &p.CurrentTask, &p.TaskProgress,
		&p.Objectives, &p.StartedAt, &p.CompletedAt,
	)
}

func GetQuestProgressByChar(charID int64) (map[int]domain.QuestProgress, error) {
	rows, err := DB().Query(`SELECT `+questProgressCols+` FROM quest_progress WHERE char_id = ?`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]domain.QuestProgress{}
	for rows.Next() {
		var p domain.QuestProgress
		if err := scanQuestProgress(rows, &p); err != nil {
			return nil, err
		}
		out[p.QuestID] = p
	}
	return out, rows.Err()
}


func StartQuest(charID int64, questID int) error {
	existing, err := GetQuestProgressOne(charID, questID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	_, err = DB().Exec(`
		INSERT INTO quest_progress (char_id, quest_id, status, current_task, task_progress, started_at)
		VALUES (?, ?, ?, 0, 0, ?)`,
		charID, questID, domain.QuestStateInProgress, time.Now().UTC().Format(time.RFC3339))
	return err
}

func SetQuestStatus(charID int64, questID int, status int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := DB().Exec(`
		UPDATE quest_progress SET status = ?, completed_at = CASE WHEN ? IN (?, ?) THEN ? ELSE completed_at END
		WHERE char_id = ? AND quest_id = ?`,
		status, status, domain.QuestStateCompleted, domain.QuestStateRewardClaimed, now, charID, questID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = DB().Exec(`
		INSERT INTO quest_progress (char_id, quest_id, status, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?)`,
		charID, questID, status, now, now)
	return err
}

func AdvanceQuestTask(charID int64, questID int, newTask int) error {
	_, err := DB().Exec(`
		UPDATE quest_progress SET current_task = ?, task_progress = 0
		WHERE char_id = ? AND quest_id = ?`,
		newTask, charID, questID)
	return err
}


func SetQuestTaskProgress(charID int64, questID int, taskID int, progress int) error {
	_, err := DB().Exec(`
		UPDATE quest_progress SET current_task = ?, task_progress = ?
		WHERE char_id = ? AND quest_id = ?`,
		taskID, progress, charID, questID)
	return err
}

// removes a quest row entirely (op 215 RejectQuest) the
// quest reverts to catalog-computed on the next op 185
func DeleteQuestProgress(charID int64, questID int) error {
	_, err := DB().Exec(`DELETE FROM quest_progress WHERE char_id = ? AND quest_id = ?`, charID, questID)
	return err
}


func parseQuestTimestamp(ts string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}


func ResetStaleDailyQuests(charID int64, dailyQuestIDs map[int]bool) error {
	if len(dailyQuestIDs) == 0 {
		return nil
	}
	rows, err := DB().Query(`
		SELECT quest_id, COALESCE(completed_at, ''), COALESCE(started_at, '')
		FROM quest_progress WHERE char_id = ?`, charID)
	if err != nil {
		return err
	}
	today := time.Now().UTC().Format("2006-01-02")
	var stale []int
	for rows.Next() {
		var questID int
		var completedAt, startedAt string
		if err := rows.Scan(&questID, &completedAt, &startedAt); err != nil {
			rows.Close()
			return err
		}
		if !dailyQuestIDs[questID] {
			continue
		}
		ts := completedAt
		if ts == "" {
			ts = startedAt
		}
		if ts == "" {
			continue
		}
		t, ok := parseQuestTimestamp(ts)
		if !ok || t.UTC().Format("2006-01-02") == today {
			continue
		}
		stale = append(stale, questID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, questID := range stale {
		if _, err := DB().Exec(`DELETE FROM quest_progress WHERE char_id = ? AND quest_id = ?`, charID, questID); err != nil {
			return err
		}
	}
	return nil
}

func GetQuestProgressOne(charID int64, questID int) (*domain.QuestProgress, error) {
	row := DB().QueryRow(`SELECT `+questProgressCols+` FROM quest_progress WHERE char_id = ? AND quest_id = ?`, charID, questID)
	var p domain.QuestProgress
	if err := scanQuestProgress(row, &p); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}
