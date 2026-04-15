package main

import "fmt"

// ── Quest definitions (static) ────────────────────────────────────────────────

type QuestDef struct {
	ID          string
	Name        string
	Description string
	Objective   string
	// ObjectiveType: "kill_npc" | "collect_gralats" | "talk_npc"
	ObjectiveType string
	ObjectiveTarget string // NPC type or item name
	ObjectiveCount  int
	// Reward
	RewardGralats int
}

var questDefs = []QuestDef{
	{
		ID:              "q_first_steps",
		Name:            "First Steps",
		Description:     "Collect your first gralats from the world.",
		Objective:       "Collect 10 gralats",
		ObjectiveType:   "collect_gralats",
		ObjectiveCount:  10,
		RewardGralats:   25,
	},
	{
		ID:              "q_monster_hunter",
		Name:            "Monster Hunter",
		Description:     "The lands are plagued by slimes. Defeat 3 aggressive monsters.",
		Objective:       "Defeat 3 monsters",
		ObjectiveType:   "kill_npc",
		ObjectiveTarget: "aggressive",
		ObjectiveCount:  3,
		RewardGralats:   50,
	},
	{
		ID:              "q_traveller",
		Name:            "World Traveller",
		Description:     "Visit the Merchant in town and introduce yourself.",
		Objective:       "Talk to a Merchant",
		ObjectiveType:   "talk_npc",
		ObjectiveTarget: "merchant",
		ObjectiveCount:  1,
		RewardGralats:   15,
	},
	{
		ID:              "q_rich",
		Name:            "Nouveau Riche",
		Description:     "Accumulate a fortune of 100 gralats.",
		Objective:       "Have 100 gralats",
		ObjectiveType:   "collect_gralats",
		ObjectiveCount:  100,
		RewardGralats:   30,
	},
	{
		ID:              "q_exterminator",
		Name:            "Exterminator",
		Description:     "The village needs peace. Defeat 10 aggressive monsters.",
		Objective:       "Defeat 10 monsters",
		ObjectiveType:   "kill_npc",
		ObjectiveTarget: "aggressive",
		ObjectiveCount:  10,
		RewardGralats:   150,
	},
}

func questByID(id string) *QuestDef {
	for i := range questDefs {
		if questDefs[i].ID == id {
			return &questDefs[i]
		}
	}
	return nil
}

// ── DB helpers ────────────────────────────────────────────────────────────────

type PlayerQuestRow struct {
	QuestID   string
	Progress  int
	Completed bool
}

func dbGetPlayerQuests(userID int64) []PlayerQuestRow {
	rows, err := database.Query(
		`SELECT quest_id, progress, completed FROM player_quests WHERE user_id=?`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []PlayerQuestRow
	for rows.Next() {
		var r PlayerQuestRow
		var comp int
		if rows.Scan(&r.QuestID, &r.Progress, &comp) == nil {
			r.Completed = comp == 1
			result = append(result, r)
		}
	}
	return result
}

// dbStartQuest initializes a quest for a player (if not already started).
func dbStartQuest(userID int64, questID string) error {
	if questByID(questID) == nil {
		return fmt.Errorf("unknown quest")
	}
	_, err := database.Exec(
		`INSERT OR IGNORE INTO player_quests (user_id, quest_id, progress, completed) VALUES (?,?,0,0)`,
		userID, questID,
	)
	return err
}

// dbUpdateQuestProgress increments quest progress; completes it if target reached.
// Returns (newProgress, justCompleted, rewardGralats).
func dbUpdateQuestProgress(userID int64, questID string, delta int) (int, bool, int) {
	def := questByID(questID)
	if def == nil {
		return 0, false, 0
	}

	var progress int
	var completed int
	database.QueryRow(
		`SELECT progress, completed FROM player_quests WHERE user_id=? AND quest_id=?`,
		userID, questID,
	).Scan(&progress, &completed)

	if completed == 1 {
		return progress, false, 0 // already done
	}

	// Ensure row exists
	database.Exec(
		`INSERT OR IGNORE INTO player_quests (user_id, quest_id, progress, completed) VALUES (?,?,0,0)`,
		userID, questID,
	)

	progress += delta
	if progress > def.ObjectiveCount {
		progress = def.ObjectiveCount
	}

	justCompleted := progress >= def.ObjectiveCount
	comp := 0
	reward := 0
	if justCompleted {
		comp = 1
		reward = def.RewardGralats
		dbAddGralats(userID, reward)
	}
	database.Exec(
		`UPDATE player_quests SET progress=?, completed=? WHERE user_id=? AND quest_id=?`,
		progress, comp, userID, questID,
	)
	return progress, justCompleted, reward
}
