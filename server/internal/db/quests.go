package db

import "fmt"

// QuestDef is the static definition of a quest.
type QuestDef struct {
	ID              string
	Name            string
	Description     string
	Objective       string
	ObjectiveType   string // "kill_npc" | "collect_gralats" | "talk_npc"
	ObjectiveTarget string // NPC type or item name
	ObjectiveCount  int
	RewardGralats   int
}

// QuestDefs is the authoritative list of all quests in the game.
var QuestDefs = []QuestDef{
	{
		ID:             "q_first_steps",
		Name:           "First Steps",
		Description:    "Collect your first gralats from the world.",
		Objective:      "Collect 10 gralats",
		ObjectiveType:  "collect_gralats",
		ObjectiveCount: 10,
		RewardGralats:  25,
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
		ID:             "q_rich",
		Name:           "Nouveau Riche",
		Description:    "Accumulate a fortune of 100 gralats.",
		Objective:      "Have 100 gralats",
		ObjectiveType:  "collect_gralats",
		ObjectiveCount: 100,
		RewardGralats:  30,
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

// QuestByID returns the QuestDef for the given id, or nil.
func QuestByID(id string) *QuestDef {
	for i := range QuestDefs {
		if QuestDefs[i].ID == id {
			return &QuestDefs[i]
		}
	}
	return nil
}

// PlayerQuestRow is a row from the player_quests table.
type PlayerQuestRow struct {
	QuestID   string
	Progress  int
	Completed bool
}

func GetPlayerQuests(userID int64) []PlayerQuestRow {
	rows, err := conn.Query(
		`SELECT quest_id, progress, completed FROM player_quests WHERE user_id=$1`, userID)
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

func StartQuest(userID int64, questID string) error {
	if QuestByID(questID) == nil {
		return fmt.Errorf("unknown quest")
	}
	_, err := conn.Exec(
		`INSERT INTO player_quests (user_id, quest_id, progress, completed)
		 VALUES ($1, $2, 0, 0)
		 ON CONFLICT (user_id, quest_id) DO NOTHING`,
		userID, questID,
	)
	return err
}

// UpdateQuestProgress increments quest progress.
// Returns (newProgress, justCompleted, rewardGralats).
func UpdateQuestProgress(userID int64, questID string, delta int) (int, bool, int) {
	def := QuestByID(questID)
	if def == nil {
		return 0, false, 0
	}

	var progress, completed int
	conn.QueryRow(
		`SELECT progress, completed FROM player_quests WHERE user_id=$1 AND quest_id=$2`,
		userID, questID,
	).Scan(&progress, &completed)

	if completed == 1 {
		return progress, false, 0
	}

	conn.Exec(
		`INSERT INTO player_quests (user_id, quest_id, progress, completed)
		 VALUES ($1, $2, 0, 0)
		 ON CONFLICT (user_id, quest_id) DO NOTHING`,
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
		AddGralats(userID, reward) //nolint:errcheck
	}
	conn.Exec(
		`UPDATE player_quests SET progress=$1, completed=$2
		 WHERE user_id=$3 AND quest_id=$4`,
		progress, comp, userID, questID,
	)
	return progress, justCompleted, reward
}
