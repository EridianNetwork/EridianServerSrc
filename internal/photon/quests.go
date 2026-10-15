package photon


import (
	"slices"
	"encoding/json"
	"log"
	"maps"
	"os"
	"sync"

	"eridian/internal/db"
	"eridian/internal/domain"
)

type QuestReward struct {
	Type     string `json:"type"`
	Count    int    `json:"count,omitempty"`
	RewardID int    `json:"rewardId,omitempty"`
	MaterialType string `json:"materialType,omitempty"`
}

type QuestTask struct {
	TaskID    int    `json:"taskId"`
	Type      string `json:"type"`
	Target    int    `json:"target"`
	TimeLimit int    `json:"timeLimit"`
}

type QuestDef struct {
	ID               int           `json:"id"`
	GiverIDs         []int         `json:"giverIds"`
	ReclaimerIDs     []int         `json:"reclaimerIds"`
	BoundInstanceIDs []int         `json:"boundInstanceIds"`
	Category         string        `json:"category"`
	Title            string        `json:"title"`
	Description      string        `json:"description"`
	RequiredLevel    int           `json:"requiredLevel"`
	UnlockLevel      int           `json:"unlockLevel"`
	// RequiredDifficulty is Quest.mRequiredDifficulty: -1 any, 1 hard (hero), 2 hell; matches the room "Difficulty" property.
	RequiredDifficulty int  `json:"requiredDifficulty"`
	IsBoundInstance    bool `json:"isBoundInstance"`
	Rewards          []QuestReward `json:"rewards"`
	Tasks            []QuestTask   `json:"tasks"`
	OptionalRewardsCount int           `json:"optionalRewardsCount,omitempty"`
	OptionalRewards      []QuestReward `json:"optionalRewards,omitempty"`
}

var (
	questsOnce    sync.Once
	questByID     map[int]*QuestDef
	questPrereq   map[int]int
	dailyQuestIDs map[int]bool
	questsAll     []QuestDef
)

func loadQuestCatalog() {
	questsOnce.Do(func() {
		questByID = map[int]*QuestDef{}
		questPrereq = map[int]int{}
		dailyQuestIDs = map[int]bool{}
		raw, err := os.ReadFile("data/quests.json")
		if err != nil {
			log.Printf("[PHOTON] quests.json not loaded: %v", err)
			return
		}
		var cat struct {
			Quests      []QuestDef `json:"quests"`
			DailyQuests []QuestDef `json:"dailyQuests"`
		}
		if err := json.Unmarshal(raw, &cat); err != nil {
			log.Printf("[PHOTON] quests.json parse error: %v", err)
			return
		}
		questsAll = append(cat.Quests, cat.DailyQuests...)
		for i := range questsAll {
			questByID[questsAll[i].ID] = &questsAll[i]
		}
		for _, q := range cat.DailyQuests {
			dailyQuestIDs[q.ID] = true
		}
		for i := range questsAll {
			for _, r := range questsAll[i].Rewards {
				if r.Type == "Quest" && r.RewardID != 0 {
					questPrereq[r.RewardID] = questsAll[i].ID
				}
			}
		}
		log.Printf("[PHOTON] loaded %d quests from data/quests.json (%d daily)", len(questsAll), len(dailyQuestIDs))
	})
}

func questCountsInRoom(q *QuestDef, room *Room) bool {
	if room == nil {
		return true
	}
	if q.RequiredDifficulty != -1 && int32(q.RequiredDifficulty) != asInt32(room.Properties["Difficulty"]) {
		return false
	}
	if q.IsBoundInstance && len(q.BoundInstanceIDs) > 0 {
		playlist := int(asInt32(room.Properties["PlaylistId"]))
		for _, id := range q.BoundInstanceIDs {
			if id == playlist {
				return true
			}
		}
		return false
	}
	return true
}

func questIsUnlocked(questID int, progress map[int]domain.QuestProgress) bool {
	prereq, has := questPrereq[questID]
	if !has {
		return true
	}
	p, ok := progress[prereq]
	return ok && (p.Status == domain.QuestStateCompleted || p.Status == domain.QuestStateRewardClaimed)
}

func playerQuestProgress(charID int64, charLevel int) map[int]domain.QuestProgress {
	loadQuestCatalog()
	if err := db.ResetStaleDailyQuests(charID, dailyQuestIDs); err != nil {
		log.Printf("[PHOTON] ResetStaleDailyQuests char=%d: %v", charID, err)
	}
	progress, err := db.GetQuestProgressByChar(charID)
	if err != nil {
		log.Printf("[PHOTON] GetQuestProgressByChar error: %v", err)
		progress = map[int]domain.QuestProgress{}
	}
	out := make(map[int]domain.QuestProgress, len(progress))
	maps.Copy(out, progress)
	for i := range questsAll {
		q := &questsAll[i]
		if _, started := progress[q.ID]; started {
			continue
		}
		if charLevel < q.RequiredLevel {
			continue
		}
		if !questIsUnlocked(q.ID, progress) {
			continue
		}
		out[q.ID] = domain.QuestProgress{CharID: charID, QuestID: q.ID, Status: domain.QuestStateAvailable}
	}
	return out
}

func questHashtable(p domain.QuestProgress) map[int]any {
	return map[int]any{
		0: int32(p.QuestID),
		1: int32(p.Status),
		2: int32(p.CurrentTask),
		3: int32(p.TaskProgress),
	}
}

func questProgressHashtable(charID int64) map[int]any {
	level := 1
	if ch, err := db.GetCharacterByID(charID); err == nil && ch != nil {
		level = int(ch.Level)
	}
	progress := playerQuestProgress(charID, level)
	out := make(map[int]any, len(progress))
	for id, p := range progress {
		out[id] = questHashtable(p)
	}
	return out
}

func charLevel(charID int64) int {
	if ch, err := db.GetCharacterByID(charID); err == nil && ch != nil {
		return int(ch.Level)
	}
	return 1
}

func unlockedInstancesForChar(charID int64, level int) map[int]int32 {
	loadQuestCatalog()
	progress := playerQuestProgress(charID, level)
	out := map[int]int32{}
	for qid := range progress {
		q := questByID[qid]
		if q == nil {
			continue
		}
		for _, id := range q.BoundInstanceIDs {
			out[id] = 2
		}
	}
	return out
}

func (s *EnetServer) notifyNewlyUnlockedInstances(sess *ClientSession, charID int64, level int, before map[int]int32) {
	for id := range unlockedInstancesForChar(charID, level) {
		if _, had := before[id]; !had {
			s.sendEventTo(sess, EvInstanceUnlocked, map[byte]any{0: int32(id)})
		}
	}
}

func (s *EnetServer) hostSyncGroupUnlocks(sess *ClientSession) {
	if sess.selectedCharID == 0 {
		return
	}
	for id := range unlockedInstancesForChar(sess.selectedCharID, charLevel(sess.selectedCharID)) {
		s.sendEventTo(sess, EvInstanceUnlocked, map[byte]any{0: int32(id)})
	}
}

func sessCharID(sess *ClientSession) int64 {
	if sess.selectedCharID != 0 {
		return sess.selectedCharID
	}
	if sess.hostChar != nil {
		return sess.hostChar.CharID
	}
	return 0
}

func (s *EnetServer) opGetPlayerQuests(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int64(0)
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	if charID == 0 {
		charID = sessCharID(sess)
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(charID), 1: questProgressHashtable(charID)})
}

func (s *EnetServer) opUpdateQuestRequirement(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := sessCharID(sess)
	questID, taskID, taskProgress := 0, 0, 0
	if len(arr) > 0 {
		charID = int64(asInt32(arr[0]))
	}
	if len(arr) > 1 {
		questID = int(asInt32(arr[1]))
	}
	if len(arr) > 2 {
		taskID = int(asInt32(arr[2]))
	}
	if len(arr) > 3 {
		taskProgress = int(asInt32(arr[3]))
	}
	if err := db.SetQuestTaskProgress(charID, questID, taskID, taskProgress); err != nil {
		log.Printf("[PHOTON] UpdateQuestRequirement error char=%d quest=%d: %v", charID, questID, err)
	} else {
		log.Printf("[PHOTON] UpdateQuestRequirement char=%d quest=%d task=%d progress=%d", charID, questID, taskID, taskProgress)
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: int32(charID), 1: questProgressHashtable(charID)})
}

func (s *EnetServer) opRejectQuest(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int32(sessCharID(sess))
	questID := int32(0)
	if len(arr) > 0 {
		charID = asInt32(arr[0])
	}
	if len(arr) > 1 {
		questID = asInt32(arr[1])
	}
	result := int16(0)
	if err := db.DeleteQuestProgress(int64(charID), int(questID)); err != nil {
		log.Printf("[PHOTON] RejectQuest error char=%d quest=%d: %v", charID, questID, err)
		result = 1
	} else {
		log.Printf("[PHOTON] RejectQuest char=%d quest=%d", charID, questID)
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: result, 1: questID})
	if result == 0 {
		s.sendEventTo(sess, EvQuestStatusUpdated, map[byte]any{
			0: charID, 1: questID, 2: int32(domain.QuestStateAvailable),
		})
	}
}

func (s *EnetServer) opCompleteQuest(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int32(sessCharID(sess))
	questID := int32(0)
	if len(arr) > 0 {
		charID = asInt32(arr[0])
	}
	if len(arr) > 1 {
		questID = asInt32(arr[1])
	}
	level := charLevel(int64(charID))
	before := unlockedInstancesForChar(int64(charID), level)
	if err := db.SetQuestStatus(int64(charID), int(questID), domain.QuestStateCompleted); err != nil {
		log.Printf("[PHOTON] CompleteQuest error char=%d quest=%d: %v", charID, questID, err)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	log.Printf("[PHOTON] CompleteQuest char=%d quest=%d", charID, questID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	s.sendEventTo(sess, EvQuestStatusUpdated, map[byte]any{
		0: charID, 1: questID, 2: int32(domain.QuestStateCompleted),
	})
	s.notifyNewlyUnlockedInstances(sess, int64(charID), level, before)
}

func (s *EnetServer) hostValidateQuestStep(room *Room, questID, stepID int, clusterName string) {
	loadQuestCatalog()
	q := questByID[questID]
	if q == nil || len(q.Tasks) == 0 {
		log.Printf("[CLUSTER] ValidateQuestStep quest=%d step=%d: not in client catalog, skipped (cluster %q)", questID, stepID, clusterName)
		return
	}
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.hostChar == nil {
			continue
		}
		charID := sess.hostChar.CharID
		p, err := db.GetQuestProgressOne(charID, questID)
		if err != nil {
			log.Printf("[CLUSTER] ValidateQuestStep char=%d quest=%d: %v", charID, questID, err)
			continue
		}
		if p == nil || p.Status != domain.QuestStateInProgress || p.TaskProgress >= stepID {
			continue
		}
		taskIdx := p.CurrentTask
		if taskIdx >= len(q.Tasks) {
			taskIdx = len(q.Tasks) - 1
		}
		task := q.Tasks[taskIdx]
		if taskIdx == len(q.Tasks)-1 && stepID >= task.Target {
			level := charLevel(charID)
			before := unlockedInstancesForChar(charID, level)
			if err := db.SetQuestStatus(charID, questID, domain.QuestStateCompleted); err != nil {
				log.Printf("[CLUSTER] ValidateQuestStep complete char=%d quest=%d: %v", charID, questID, err)
				continue
			}
			if online := s.sessionOnline(sess.userID); online != nil {
				s.sendEventTo(online, EvQuestStatusUpdated, map[byte]any{
					0: int32(charID), 1: int32(questID), 2: int32(domain.QuestStateCompleted),
				})
				s.notifyNewlyUnlockedInstances(online, charID, level, before)
			}
			log.Printf("[CLUSTER] quest %d COMPLETED via step %d char=%d (cluster %q)", questID, stepID, charID, clusterName)
			continue
		}
		if err := db.SetQuestTaskProgress(charID, questID, p.CurrentTask, stepID); err != nil {
			log.Printf("[CLUSTER] ValidateQuestStep progress char=%d quest=%d: %v", charID, questID, err)
			continue
		}
		log.Printf("[CLUSTER] quest %d step %d recorded char=%d (cluster %q)", questID, stepID, charID, clusterName)
	}
}

func (s *EnetServer) opGetDailyQuests(sess *ClientSession, opCode byte, params map[byte]any) {
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: dailyQuestsPanelHashtable(sessCharID(sess))})
}

func dailyQuestsPanelHashtable(charID int64) map[int]any {
	loadQuestCatalog()
	progress := playerQuestProgress(charID, charLevel(charID))
	out := map[int]any{}
	for questID := range dailyQuestIDs {
		if _, unlocked := progress[questID]; unlocked {
			out[questID] = map[int]any{0: int32(questID), 1: int32(questID)}
		}
	}
	return out
}

func (s *EnetServer) opTalkToNPC(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	questID, taskID := 0, 0
	if len(arr) > 0 {
		questID = int(asInt32(arr[0]))
	}
	if len(arr) > 1 {
		taskID = int(asInt32(arr[1]))
	}
	charID := sessCharID(sess)
	level := 1
	if ch, err := db.GetCharacterByID(charID); err == nil && ch != nil {
		level = int(ch.Level)
	}
	wantTaskType := "TalkTo"
	if opCode == OpBringToNPC {
		wantTaskType = "BringTo"
	}
	loadQuestCatalog()
	progress := playerQuestProgress(charID, level)
	out := map[int]any{}
	q := questByID[questID]
	if q != nil {
		p := progress[q.ID]
		if p.Status == domain.QuestStateInProgress && p.CurrentTask < len(q.Tasks) {
			task := q.Tasks[p.CurrentTask]
			if task.TaskID == taskID && task.Type == wantTaskType {
				if p.CurrentTask == len(q.Tasks)-1 {
					before := unlockedInstancesForChar(charID, level)
					if err := db.SetQuestStatus(charID, q.ID, domain.QuestStateCompleted); err != nil {
						log.Printf("[PHOTON] TalkToNPC complete quest error quest=%d: %v", q.ID, err)
					} else {
						p.Status = domain.QuestStateCompleted
						s.sendEventTo(sess, EvQuestStatusUpdated, map[byte]any{
							0: int32(charID), 1: int32(q.ID), 2: int32(domain.QuestStateCompleted),
						})
						s.notifyNewlyUnlockedInstances(sess, charID, level, before)
					}
				} else if err := db.AdvanceQuestTask(charID, q.ID, p.CurrentTask+1); err != nil {
					log.Printf("[PHOTON] TalkToNPC advance task error quest=%d: %v", q.ID, err)
				} else {
					p.CurrentTask++
					p.TaskProgress = 0
					s.sendEventTo(sess, EvQuestTaskCompleted, map[byte]any{
						0: int32(charID), 1: int32(q.ID),
					})
				}
			}
		}
		out[q.ID] = questHashtable(p)
	}
	log.Printf("[PHOTON] TalkToNPC char=%d quest=%d task=%d → advanced=%v", charID, questID, taskID, len(out) > 0)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: int32(charID),
		1: out,
	})
}

func (s *EnetServer) hostAdvanceInstanceQuestTasks(sess *ClientSession, room *Room, charID int64, playlistID int32) {
	if charID == 0 {
		return
	}
	online := s.sessionOnline(sess.userID)
	if online == nil {
		online = sess
	}
	level := charLevel(charID)
	loadQuestCatalog()
	progress := playerQuestProgress(charID, level)
	for _, q := range questsAll {
		p := progress[q.ID]
		if p.Status != domain.QuestStateInProgress || p.CurrentTask >= len(q.Tasks) {
			continue
		}
		task := q.Tasks[p.CurrentTask]
		if task.Type != "Instance" || int32(task.Target) != playlistID || !questCountsInRoom(&q, room) {
			continue
		}
		if p.CurrentTask == len(q.Tasks)-1 {
			before := unlockedInstancesForChar(charID, level)
			if err := db.SetQuestStatus(charID, q.ID, domain.QuestStateCompleted); err != nil {
				log.Printf("[PHOTON] CompleteInstance quest complete error quest=%d: %v", q.ID, err)
				continue
			}
			s.sendEventTo(online, EvQuestStatusUpdated, map[byte]any{
				0: int32(charID), 1: int32(q.ID), 2: int32(domain.QuestStateCompleted),
			})
			s.notifyNewlyUnlockedInstances(online, charID, level, before)
			log.Printf("[PHOTON] CompleteInstance char=%d quest=%d completed via Instance task target=%d", charID, q.ID, playlistID)
		} else if err := db.AdvanceQuestTask(charID, q.ID, p.CurrentTask+1); err != nil {
			log.Printf("[PHOTON] CompleteInstance quest advance error quest=%d: %v", q.ID, err)
		} else {
			s.sendEventTo(online, EvQuestTaskCompleted, map[byte]any{
				0: int32(charID), 1: int32(q.ID),
			})
			log.Printf("[PHOTON] CompleteInstance char=%d quest=%d advanced task via Instance target=%d", charID, q.ID, playlistID)
		}
	}
}

func (s *EnetServer) hostAdvanceCraftQuestTasks(sess *ClientSession, charID int64) {
	if charID == 0 {
		return
	}
	level := charLevel(charID)
	loadQuestCatalog()
	progress := playerQuestProgress(charID, level)
	for _, q := range questsAll {
		p := progress[q.ID]
		if p.Status != domain.QuestStateInProgress || p.CurrentTask >= len(q.Tasks) {
			continue
		}
		task := q.Tasks[p.CurrentTask]
		if task.Type != "Craft" {
			continue
		}
		newProgress := p.TaskProgress + 1
		if newProgress < task.Target {
			if err := db.SetQuestTaskProgress(charID, q.ID, p.CurrentTask, newProgress); err != nil {
				log.Printf("[PHOTON] CraftWeapon quest progress error quest=%d: %v", q.ID, err)
			}
			continue
		}
		if p.CurrentTask == len(q.Tasks)-1 {
			before := unlockedInstancesForChar(charID, level)
			if err := db.SetQuestStatus(charID, q.ID, domain.QuestStateCompleted); err != nil {
				log.Printf("[PHOTON] CraftWeapon quest complete error quest=%d: %v", q.ID, err)
				continue
			}
			s.sendEventTo(sess, EvQuestStatusUpdated, map[byte]any{
				0: int32(charID), 1: int32(q.ID), 2: int32(domain.QuestStateCompleted),
			})
			s.notifyNewlyUnlockedInstances(sess, charID, level, before)
			log.Printf("[PHOTON] CraftWeapon char=%d quest=%d completed via Craft task target=%d", charID, q.ID, task.Target)
		} else if err := db.AdvanceQuestTask(charID, q.ID, p.CurrentTask+1); err != nil {
			log.Printf("[PHOTON] CraftWeapon quest advance error quest=%d: %v", q.ID, err)
		} else {
			s.sendEventTo(sess, EvQuestTaskCompleted, map[byte]any{
				0: int32(charID), 1: int32(q.ID),
			})
			log.Printf("[PHOTON] CraftWeapon char=%d quest=%d advanced task via Craft target=%d", charID, q.ID, task.Target)
		}
	}
}

func (s *EnetServer) hostAdvanceKillQuestTasks(sess *ClientSession, room *Room, charID int64) {
	if charID == 0 {
		return
	}
	level := charLevel(charID)
	loadQuestCatalog()
	progress := playerQuestProgress(charID, level)
	for _, q := range questsAll {
		p := progress[q.ID]
		if p.Status != domain.QuestStateInProgress || p.CurrentTask >= len(q.Tasks) {
			continue
		}
		task := q.Tasks[p.CurrentTask]
		if task.Type != "Kills" || !questCountsInRoom(&q, room) {
			continue
		}
		newProgress := p.TaskProgress + 1
		if newProgress < task.Target {
			if err := db.SetQuestTaskProgress(charID, q.ID, p.CurrentTask, newProgress); err != nil {
				log.Printf("[PHOTON] KillNpc quest progress error quest=%d: %v", q.ID, err)
			}
			continue
		}
		if p.CurrentTask == len(q.Tasks)-1 {
			before := unlockedInstancesForChar(charID, level)
			if err := db.SetQuestStatus(charID, q.ID, domain.QuestStateCompleted); err != nil {
				log.Printf("[PHOTON] KillNpc quest complete error quest=%d: %v", q.ID, err)
				continue
			}
			s.sendEventTo(sess, EvQuestStatusUpdated, map[byte]any{
				0: int32(charID), 1: int32(q.ID), 2: int32(domain.QuestStateCompleted),
			})
			s.notifyNewlyUnlockedInstances(sess, charID, level, before)
			log.Printf("[PHOTON] KillNpc char=%d quest=%d completed via Kills task target=%d", charID, q.ID, task.Target)
		} else if err := db.AdvanceQuestTask(charID, q.ID, p.CurrentTask+1); err != nil {
			log.Printf("[PHOTON] KillNpc quest advance error quest=%d: %v", q.ID, err)
		} else {
			s.sendEventTo(sess, EvQuestTaskCompleted, map[byte]any{
				0: int32(charID), 1: int32(q.ID),
			})
			log.Printf("[PHOTON] KillNpc char=%d quest=%d advanced task via Kills target=%d", charID, q.ID, task.Target)
		}
	}
}

func (s *EnetServer) hostAdvancePvPQuestTasks(sess *ClientSession, charID int64) {
	if charID == 0 {
		return
	}
	online := s.sessionOnline(sess.userID)
	if online == nil {
		online = sess
	}
	level := charLevel(charID)
	loadQuestCatalog()
	progress := playerQuestProgress(charID, level)
	for _, q := range questsAll {
		p := progress[q.ID]
		if p.Status != domain.QuestStateInProgress || p.CurrentTask >= len(q.Tasks) {
			continue
		}
		task := q.Tasks[p.CurrentTask]
		if task.Type != "PVPMode" {
			continue
		}
		newProgress := p.TaskProgress + 1
		if newProgress < task.Target {
			if err := db.SetQuestTaskProgress(charID, q.ID, p.CurrentTask, newProgress); err != nil {
				log.Printf("[PHOTON] PvPMatchEnd quest progress error quest=%d: %v", q.ID, err)
			}
			continue
		}
		if p.CurrentTask == len(q.Tasks)-1 {
			before := unlockedInstancesForChar(charID, level)
			if err := db.SetQuestStatus(charID, q.ID, domain.QuestStateCompleted); err != nil {
				log.Printf("[PHOTON] PvPMatchEnd quest complete error quest=%d: %v", q.ID, err)
				continue
			}
			s.sendEventTo(online, EvQuestStatusUpdated, map[byte]any{
				0: int32(charID), 1: int32(q.ID), 2: int32(domain.QuestStateCompleted),
			})
			s.notifyNewlyUnlockedInstances(online, charID, level, before)
			log.Printf("[PHOTON] PvPMatchEnd char=%d quest=%d completed via PVPMode task target=%d", charID, q.ID, task.Target)
		} else if err := db.AdvanceQuestTask(charID, q.ID, p.CurrentTask+1); err != nil {
			log.Printf("[PHOTON] PvPMatchEnd quest advance error quest=%d: %v", q.ID, err)
		} else {
			s.sendEventTo(online, EvQuestTaskCompleted, map[byte]any{
				0: int32(charID), 1: int32(q.ID),
			})
			log.Printf("[PHOTON] PvPMatchEnd char=%d quest=%d advanced task via PVPMode target=%d", charID, q.ID, task.Target)
		}
	}
}

func (s *EnetServer) opAcceptQuest(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int32(sessCharID(sess))
	questID := int32(0)
	if len(arr) > 0 {
		charID = asInt32(arr[0])
	}
	if len(arr) > 1 {
		questID = asInt32(arr[1])
	}
	loadQuestCatalog()
	if err := db.ResetStaleDailyQuests(int64(charID), dailyQuestIDs); err != nil {
		log.Printf("[PHOTON] AcceptQuest ResetStaleDailyQuests char=%d: %v", charID, err)
	}
	if err := db.StartQuest(int64(charID), int(questID)); err != nil {
		log.Printf("[PHOTON] AcceptQuest error char=%d quest=%d: %v", charID, questID, err)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: charID, 1: questID, 2: false})
		return
	}
	log.Printf("[PHOTON] AcceptQuest char=%d quest=%d", charID, questID)
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: charID,
		1: questID,
		2: true,
	})
	s.sendEventTo(sess, EvQuestStatusUpdated, map[byte]any{
		0: charID,
		1: questID,
		2: int32(domain.QuestStateInProgress),
	})
}

func (s *EnetServer) grantQuestRewards(sess *ClientSession, charID int64, rewards []QuestReward) {
	ch, err := db.GetCharacterByID(charID)
	if err != nil || ch == nil {
		log.Printf("[PHOTON] grantQuestRewards char=%d: character not found: %v", charID, err)
		return
	}
	backpackSize := clampBackpackSize(ch.BackpackSize)
	grantedCurrencyOrItems := false
	for _, r := range rewards {
		switch r.Type {
		case "Experience":
			if sess.hostChar != nil {
				s.hostGrantExp(sess, r.Count)
			} else {
				log.Printf("[PHOTON] grantQuestRewards char=%d: no hostChar, skipping %d xp", charID, r.Count)
			}
		case "BondCurrency":
			if err := db.AddCharacterMoney(charID, r.Count); err != nil {
				log.Printf("[PHOTON] grantQuestRewards char=%d: AddCharacterMoney: %v", charID, err)
			} else {
				grantedCurrencyOrItems = true
			}
		case "Material":
			mt, ok := materialTypeValues[r.MaterialType]
			if !ok {
				log.Printf("[PHOTON] grantQuestRewards char=%d: unknown materialType %q", charID, r.MaterialType)
				continue
			}
			if err := db.GrantMaterial(charID, mt, r.Count, backpackSize); err != nil {
				log.Printf("[PHOTON] grantQuestRewards char=%d: GrantMaterial %s: %v", charID, r.MaterialType, err)
			} else {
				grantedCurrencyOrItems = true
			}
		case "Shield":
			sp := shieldPresetByID(r.RewardID)
			if sp == nil {
				log.Printf("[PHOTON] grantQuestRewards char=%d: no shield preset id=%d, falling back to random", charID, r.RewardID)
				sp = randomShieldPresetAnyRarity(ch.Level)
			}
			if sp == nil {
				log.Printf("[PHOTON] grantQuestRewards char=%d: no shield preset available at all (rewardId=%d)", charID, r.RewardID)
				continue
			}
			item := &domain.InventoryItem{
				CharID:           charID,
				ItemKind:         domain.ItemTypeShield,
				ResourceID:       sp.ID,
				ItemName:         sp.Name,
				Rarity:           itemRarityValues[sp.Rarity],
				PartA:            shieldManufacturerValues[sp.Manufacturer],
				PartB:            sp.Body,
				PartC:            sp.Battery,
				PartD:            sp.Capacitor,
				PartE:            sp.Accessory,
				PartF:            sp.Material,
				Level:            1,
				UID:              newRandomUID("quest"),
				GenerationSource: domain.GenerationSourceLoot,
			}
			if _, _, err := db.AddItemToBackpack(item, backpackSize); err != nil {
				log.Printf("[PHOTON] grantQuestRewards char=%d: AddItemToBackpack shield=%q: %v", charID, sp.Name, err)
			} else {
				grantedCurrencyOrItems = true
			}
		case "Outfit":
			if s.grantQuestOutfit(charID, ch, r, backpackSize) {
				grantedCurrencyOrItems = true
			}
		case "ClassMode":
			if s.grantQuestClassMode(charID, ch, r, backpackSize) {
				grantedCurrencyOrItems = true
			}
		case "Item":
			if s.grantQuestWeapon(charID, ch, r, backpackSize) {
				grantedCurrencyOrItems = true
			}
		case "Quest", "Instance":
		default:
			log.Printf("[PHOTON] grantQuestRewards char=%d: reward type %q not implemented yet (rewardId=%d count=%d)", charID, r.Type, r.RewardID, r.Count)
		}
	}
	if grantedCurrencyOrItems {
		s.sendCurrencyUpdate(sess, charID)
		s.sendInventorySync(sess)
	}
}

func (s *EnetServer) grantQuestOutfit(charID int64, ch *domain.Character, r QuestReward, backpackSize int) bool {
	preset := outfitPresetByID(r.RewardID)
	if preset == nil {
		log.Printf("[PHOTON] grantQuestRewards char=%d: no outfit preset id=%d", charID, r.RewardID)
		return false
	}
	if avatarTypeValues[preset.AvatarType] != ch.ClassType {
		log.Printf("[PHOTON] grantQuestRewards char=%d: outfit %q is for %s, not class=%d", charID, preset.Name, preset.AvatarType, ch.ClassType)
		return false
	}
	it := &domain.InventoryItem{
		CharID:     charID,
		ItemKind:   domain.ItemTypeOutfit,
		ResourceID: preset.ID,
		ItemName:   preset.Name,
		Rarity:     itemRarityValues[preset.Rarity],
		Level:      1,
		UID:        newRandomUID("quest"),
		PartA:      avatarTypeValues[preset.AvatarType],
		PartB:      avatarSubPartValues[preset.AvatarSubpart],
		PartC:      preset.Index,
	}
	if _, _, err := db.AddItemToBackpack(it, backpackSize); err != nil {
		log.Printf("[PHOTON] grantQuestRewards char=%d: AddItemToBackpack outfit=%q: %v", charID, preset.Name, err)
		return false
	}
	log.Printf("[PHOTON] grantQuestRewards char=%d: granted outfit %q (id=%d)", charID, preset.Name, preset.ID)
	return true
}

func (s *EnetServer) grantQuestClassMode(charID int64, ch *domain.Character, r QuestReward, backpackSize int) bool {
	loadClassModePresets()
	var cp *classModePreset
	for i := range classModePresets {
		if classModePresets[i].ID == r.RewardID {
			cp = &classModePresets[i]
			break
		}
	}
	if cp == nil {
		log.Printf("[PHOTON] grantQuestRewards char=%d: no class mod preset id=%d", charID, r.RewardID)
		return false
	}
	if !slices.Contains(classModeTypesForAvatar[ch.ClassType], cp.CMType) {
		log.Printf("[PHOTON] grantQuestRewards char=%d: class mod %q is not for class=%d", charID, cp.Name, ch.ClassType)
		return false
	}
	it := &domain.InventoryItem{
		CharID:           charID,
		ItemKind:         domain.ItemTypeClassMode,
		ResourceID:       cp.ID,
		ItemName:         cp.Name,
		Rarity:           itemRarityValues[cp.Rarity],
		PartA:            classModeTypeValues[cp.CMType],
		PartB:            cp.Adapter,
		PartC:            cp.Body,
		PartD:            cp.Chassis,
		PartE:            cp.Drum,
		PartF:            cp.Material,
		Level:            1,
		UID:              newRandomUID("quest"),
		GenerationSource: domain.GenerationSourceLoot,
	}
	if _, _, err := db.AddItemToBackpack(it, backpackSize); err != nil {
		log.Printf("[PHOTON] grantQuestRewards char=%d: AddItemToBackpack classmod=%q: %v", charID, cp.Name, err)
		return false
	}
	log.Printf("[PHOTON] grantQuestRewards char=%d: granted class mod %q (id=%d)", charID, cp.Name, cp.ID)
	return true
}

func (s *EnetServer) grantQuestWeapon(charID int64, ch *domain.Character, r QuestReward, backpackSize int) bool {
	wp := weaponPresetByID(r.RewardID)
	if wp == nil {
		log.Printf("[PHOTON] grantQuestRewards char=%d: no weapon preset id=%d", charID, r.RewardID)
		return false
	}
	level := wp.Level
	if level < 1 {
		level = 1
	}
	it := newPresetWeapon(wp, level, domain.GenerationSourceLoot)
	it.CharID = charID
	it.UID = newRandomUID("quest")
	if _, _, err := db.AddItemToBackpack(it, backpackSize); err != nil {
		log.Printf("[PHOTON] grantQuestRewards char=%d: AddItemToBackpack weapon=%q: %v", charID, wp.Name, err)
		return false
	}
	log.Printf("[PHOTON] grantQuestRewards char=%d: granted weapon %q (id=%d)", charID, wp.Name, wp.ID)
	return true
}

func chosenOptionalRewards(def *QuestDef, picks any) []QuestReward {
	if def == nil || def.OptionalRewardsCount <= 0 || len(def.OptionalRewards) == 0 {
		return nil
	}
	var idx []int
	switch t := picks.(type) {
	case []byte:
		for _, b := range t {
			idx = append(idx, int(b))
		}
	case []any:
		for _, v := range t {
			idx = append(idx, int(asInt32(v)))
		}
	}
	seen := map[int]bool{}
	var out []QuestReward
	for _, i := range idx {
		if i < 0 || i >= len(def.OptionalRewards) || seen[i] || len(out) >= def.OptionalRewardsCount {
			continue
		}
		seen[i] = true
		out = append(out, def.OptionalRewards[i])
	}
	return out
}

func (s *EnetServer) opClaimQuestRewards(sess *ClientSession, opCode byte, params map[byte]any) {
	arr := toSlice(params[112])
	charID := int32(sessCharID(sess))
	questID := int32(0)
	var picks any
	if len(arr) > 0 {
		charID = asInt32(arr[0])
	}
	if len(arr) > 1 {
		questID = asInt32(arr[1])
	}
	if len(arr) > 2 {
		picks = arr[2]
	}
	level := charLevel(int64(charID))
	alreadyClaimed := playerQuestProgress(int64(charID), level)[int(questID)].Status == domain.QuestStateRewardClaimed
	before := unlockedInstancesForChar(int64(charID), level)
	if err := db.SetQuestStatus(int64(charID), int(questID), domain.QuestStateRewardClaimed); err != nil {
		log.Printf("[PHOTON] ClaimQuestRewards error char=%d quest=%d: %v", charID, questID, err)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{0: false, 1: charID, 2: questID})
		return
	}
	log.Printf("[PHOTON] ClaimQuestRewards char=%d quest=%d", charID, questID)
	loadQuestCatalog()
	def, hasDef := questByID[int(questID)]
	if hasDef && !alreadyClaimed {
		rewards := append([]QuestReward{}, def.Rewards...)
		rewards = append(rewards, chosenOptionalRewards(def, picks)...)
		s.grantQuestRewards(sess, int64(charID), rewards)
	} else if hasDef {
		log.Printf("[PHOTON] ClaimQuestRewards char=%d quest=%d already claimed, no rewards granted", charID, questID)
	}
	progress := playerQuestProgress(int64(charID), level)
	progressTable := make(map[int]any, len(progress))
	for id, p := range progress {
		progressTable[id] = questHashtable(p)
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{
		0: true,
		1: charID,
		2: questID,
		3: progressTable,
	})
	s.sendEventTo(sess, EvQuestStatusUpdated, map[byte]any{
		0: charID,
		1: questID,
		2: int32(domain.QuestStateRewardClaimed),
	})
	if hasDef {
		for _, r := range def.Rewards {
			if r.Type != "Quest" || r.RewardID == 0 {
				continue
			}
			p, ok := progress[r.RewardID]
			if !ok {
				continue
			}
			s.sendEventTo(sess, EvQuestStatusUpdated, map[byte]any{
				0: charID,
				1: int32(r.RewardID),
				2: int32(p.Status),
			})
		}
	}
	s.notifyNewlyUnlockedInstances(sess, int64(charID), level, before)
}

func (s *EnetServer) opGetUnlockedInstances(sess *ClientSession, opCode byte, params map[byte]any) {
	charID := sessCharID(sess)
	log.Printf("[PHOTON] GetUnlockedInstances user=%s char=%d", sess.userID, charID)
	dict := DictIntInt(unlockedInstancesForChar(charID, charLevel(charID)))
	s.sendOpResponse(sess, opCode, 0, map[byte]any{0: dict})
}

func (s *EnetServer) opCompleteInstance(sess *ClientSession, opCode byte, params map[byte]any) {
	log.Printf("[PHOTON] CompleteInstance user=%s params=%v", sess.userID, params)
	if room := s.rooms[sess.roomName]; room != nil {
		charID := sess.selectedCharID
		if sess.hostChar != nil {
			charID = sess.hostChar.CharID
		}
		if charID != 0 {
			consumeEnergyForInstance(room, charID)
			s.hostAdvanceInstanceQuestTasks(sess, room, charID, asInt32(room.Properties["PlaylistId"]))
		}
		s.grantInstanceRewards(room, sess)
	}
	s.sendOpResponse(sess, opCode, 0, map[byte]any{})
}
