package main

import "fmt"

// ── Types ─────────────────────────────────────────────────────────────────────

type GuildRow struct {
	ID          int64
	Name        string
	Tag         string
	LeaderID    int64
	LeaderName  string
	Description string
	MemberCount int
}

type GuildMemberRow struct {
	UserID   int64
	Username string
	Rank     string
}

// ── DB helpers ────────────────────────────────────────────────────────────────

// dbCreateGuild creates a new guild with leaderID as founder.
// Returns the guild ID or an error.
func dbCreateGuild(name, tag, description string, leaderID int64) (int64, error) {
	if len(name) < 3 || len(name) > 24 {
		return 0, fmt.Errorf("guild name must be 3-24 characters")
	}
	if len(tag) < 2 || len(tag) > 5 {
		return 0, fmt.Errorf("guild tag must be 2-5 characters")
	}

	// Ensure player is not already in a guild
	var existing int64
	database.QueryRow(`SELECT COALESCE(guild_id,0) FROM users WHERE id=?`, leaderID).Scan(&existing)
	if existing != 0 {
		return 0, fmt.Errorf("you are already in a guild")
	}

	res, err := database.Exec(
		`INSERT INTO guilds (name, tag, leader_id, description) VALUES (?,?,?,?)`,
		name, tag, leaderID, description,
	)
	if err != nil {
		return 0, fmt.Errorf("guild name already taken")
	}
	guildID, _ := res.LastInsertId()

	// Add leader as first member
	database.Exec(`INSERT INTO guild_members (guild_id, user_id, rank) VALUES (?,?,'leader')`, guildID, leaderID)
	database.Exec(`UPDATE users SET guild_id=? WHERE id=?`, guildID, leaderID)
	return guildID, nil
}

// dbJoinGuild adds a player to a guild (must be invited first in a real game — here open join).
func dbJoinGuild(guildID, userID int64) error {
	var existing int64
	database.QueryRow(`SELECT COALESCE(guild_id,0) FROM users WHERE id=?`, userID).Scan(&existing)
	if existing != 0 {
		return fmt.Errorf("you are already in a guild")
	}
	_, err := database.Exec(
		`INSERT OR IGNORE INTO guild_members (guild_id, user_id, rank) VALUES (?,?,'member')`,
		guildID, userID,
	)
	if err != nil {
		return fmt.Errorf("could not join guild")
	}
	database.Exec(`UPDATE users SET guild_id=? WHERE id=?`, guildID, userID)
	return nil
}

// dbLeaveGuild removes a player from their guild.
func dbLeaveGuild(userID int64) error {
	var guildID int64
	database.QueryRow(`SELECT COALESCE(guild_id,0) FROM users WHERE id=?`, userID).Scan(&guildID)
	if guildID == 0 {
		return fmt.Errorf("you are not in a guild")
	}
	database.Exec(`DELETE FROM guild_members WHERE guild_id=? AND user_id=?`, guildID, userID)
	database.Exec(`UPDATE users SET guild_id=0 WHERE id=?`, userID)

	// If no members remain, delete the guild
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM guild_members WHERE guild_id=?`, guildID).Scan(&count)
	if count == 0 {
		database.Exec(`DELETE FROM guilds WHERE id=?`, guildID)
	} else {
		// If leader left, promote oldest member
		var leaderID int64
		database.QueryRow(`SELECT leader_id FROM guilds WHERE id=?`, guildID).Scan(&leaderID)
		if leaderID == userID {
			var newLeaderID int64
			database.QueryRow(
				`SELECT user_id FROM guild_members WHERE guild_id=? ORDER BY joined_at ASC LIMIT 1`,
				guildID,
			).Scan(&newLeaderID)
			if newLeaderID != 0 {
				database.Exec(`UPDATE guilds SET leader_id=? WHERE id=?`, newLeaderID, guildID)
				database.Exec(`UPDATE guild_members SET rank='leader' WHERE guild_id=? AND user_id=?`, guildID, newLeaderID)
			}
		}
	}
	return nil
}

// dbGetGuild returns info about a guild by ID.
func dbGetGuild(guildID int64) (*GuildRow, error) {
	var g GuildRow
	err := database.QueryRow(
		`SELECT g.id, g.name, g.tag, g.leader_id, u.username, g.description,
		        (SELECT COUNT(*) FROM guild_members WHERE guild_id=g.id)
		 FROM guilds g JOIN users u ON g.leader_id=u.id
		 WHERE g.id=?`, guildID,
	).Scan(&g.ID, &g.Name, &g.Tag, &g.LeaderID, &g.LeaderName, &g.Description, &g.MemberCount)
	if err != nil {
		return nil, fmt.Errorf("guild not found")
	}
	return &g, nil
}

// dbGetGuildByName returns a guild by name.
func dbGetGuildByName(name string) (*GuildRow, error) {
	var g GuildRow
	err := database.QueryRow(
		`SELECT g.id, g.name, g.tag, g.leader_id, u.username, g.description,
		        (SELECT COUNT(*) FROM guild_members WHERE guild_id=g.id)
		 FROM guilds g JOIN users u ON g.leader_id=u.id
		 WHERE g.name=?`, name,
	).Scan(&g.ID, &g.Name, &g.Tag, &g.LeaderID, &g.LeaderName, &g.Description, &g.MemberCount)
	if err != nil {
		return nil, fmt.Errorf("guild not found")
	}
	return &g, nil
}

// dbGetGuildMembers returns all members of a guild.
func dbGetGuildMembers(guildID int64) []GuildMemberRow {
	rows, err := database.Query(
		`SELECT u.id, u.username, gm.rank
		 FROM guild_members gm JOIN users u ON gm.user_id=u.id
		 WHERE gm.guild_id=?
		 ORDER BY CASE gm.rank WHEN 'leader' THEN 0 WHEN 'officer' THEN 1 ELSE 2 END, gm.joined_at`,
		guildID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []GuildMemberRow
	for rows.Next() {
		var m GuildMemberRow
		if rows.Scan(&m.UserID, &m.Username, &m.Rank) == nil {
			result = append(result, m)
		}
	}
	return result
}

// dbListGuilds returns all guilds with member counts.
func dbListGuilds() []GuildRow {
	rows, err := database.Query(
		`SELECT g.id, g.name, g.tag, g.leader_id, u.username, g.description,
		        (SELECT COUNT(*) FROM guild_members WHERE guild_id=g.id) as mc
		 FROM guilds g JOIN users u ON g.leader_id=u.id
		 ORDER BY mc DESC LIMIT 50`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []GuildRow
	for rows.Next() {
		var g GuildRow
		if rows.Scan(&g.ID, &g.Name, &g.Tag, &g.LeaderID, &g.LeaderName, &g.Description, &g.MemberCount) == nil {
			result = append(result, g)
		}
	}
	return result
}

// dbGetUserGuildID returns the guild ID of a user (0 if none).
func dbGetUserGuildID(userID int64) int64 {
	var id int64
	database.QueryRow(`SELECT COALESCE(guild_id,0) FROM users WHERE id=?`, userID).Scan(&id)
	return id
}
