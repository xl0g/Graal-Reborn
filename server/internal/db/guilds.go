package db

import "fmt"

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

func CreateGuild(name, tag, description string, leaderID int64) (int64, error) {
	if len(name) < 3 || len(name) > 24 {
		return 0, fmt.Errorf("guild name must be 3-24 characters")
	}
	if len(tag) < 2 || len(tag) > 5 {
		return 0, fmt.Errorf("guild tag must be 2-5 characters")
	}

	var existing int64
	conn.QueryRow(`SELECT COALESCE(guild_id,0) FROM users WHERE id=$1`, leaderID).Scan(&existing)
	if existing != 0 {
		return 0, fmt.Errorf("you are already in a guild")
	}

	var guildID int64
	err := conn.QueryRow(
		`INSERT INTO guilds (name, tag, leader_id, description) VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		name, tag, leaderID, description,
	).Scan(&guildID)
	if err != nil {
		return 0, fmt.Errorf("guild name already taken")
	}

	conn.Exec(`INSERT INTO guild_members (guild_id, user_id, rank) VALUES ($1, $2, 'leader')`, guildID, leaderID)
	conn.Exec(`UPDATE users SET guild_id=$1 WHERE id=$2`, guildID, leaderID)
	return guildID, nil
}

func JoinGuild(guildID, userID int64) error {
	var existing int64
	conn.QueryRow(`SELECT COALESCE(guild_id,0) FROM users WHERE id=$1`, userID).Scan(&existing)
	if existing != 0 {
		return fmt.Errorf("you are already in a guild")
	}
	_, err := conn.Exec(
		`INSERT INTO guild_members (guild_id, user_id, rank) VALUES ($1, $2, 'member')
		 ON CONFLICT (guild_id, user_id) DO NOTHING`,
		guildID, userID,
	)
	if err != nil {
		return fmt.Errorf("could not join guild")
	}
	conn.Exec(`UPDATE users SET guild_id=$1 WHERE id=$2`, guildID, userID)
	return nil
}

func LeaveGuild(userID int64) error {
	var guildID int64
	conn.QueryRow(`SELECT COALESCE(guild_id,0) FROM users WHERE id=$1`, userID).Scan(&guildID)
	if guildID == 0 {
		return fmt.Errorf("you are not in a guild")
	}

	conn.Exec(`DELETE FROM guild_members WHERE guild_id=$1 AND user_id=$2`, guildID, userID)
	conn.Exec(`UPDATE users SET guild_id=0 WHERE id=$1`, userID)

	var count int
	conn.QueryRow(`SELECT COUNT(*) FROM guild_members WHERE guild_id=$1`, guildID).Scan(&count)
	if count == 0 {
		conn.Exec(`DELETE FROM guilds WHERE id=$1`, guildID)
		return nil
	}

	var leaderID int64
	conn.QueryRow(`SELECT leader_id FROM guilds WHERE id=$1`, guildID).Scan(&leaderID)
	if leaderID == userID {
		var newLeaderID int64
		conn.QueryRow(
			`SELECT user_id FROM guild_members WHERE guild_id=$1
			 ORDER BY joined_at ASC LIMIT 1`, guildID,
		).Scan(&newLeaderID)
		if newLeaderID != 0 {
			conn.Exec(`UPDATE guilds SET leader_id=$1 WHERE id=$2`, newLeaderID, guildID)
			conn.Exec(`UPDATE guild_members SET rank='leader' WHERE guild_id=$1 AND user_id=$2`, guildID, newLeaderID)
		}
	}
	return nil
}

func GetGuild(guildID int64) (*GuildRow, error) {
	var g GuildRow
	err := conn.QueryRow(
		`SELECT g.id, g.name, g.tag, g.leader_id, u.username, g.description,
		        (SELECT COUNT(*) FROM guild_members WHERE guild_id=g.id)
		 FROM guilds g JOIN users u ON g.leader_id=u.id
		 WHERE g.id=$1`, guildID,
	).Scan(&g.ID, &g.Name, &g.Tag, &g.LeaderID, &g.LeaderName, &g.Description, &g.MemberCount)
	if err != nil {
		return nil, fmt.Errorf("guild not found")
	}
	return &g, nil
}

func GetGuildByName(name string) (*GuildRow, error) {
	var g GuildRow
	err := conn.QueryRow(
		`SELECT g.id, g.name, g.tag, g.leader_id, u.username, g.description,
		        (SELECT COUNT(*) FROM guild_members WHERE guild_id=g.id)
		 FROM guilds g JOIN users u ON g.leader_id=u.id
		 WHERE g.name=$1`, name,
	).Scan(&g.ID, &g.Name, &g.Tag, &g.LeaderID, &g.LeaderName, &g.Description, &g.MemberCount)
	if err != nil {
		return nil, fmt.Errorf("guild not found")
	}
	return &g, nil
}

func GetGuildMembers(guildID int64) []GuildMemberRow {
	rows, err := conn.Query(
		`SELECT u.id, u.username, gm.rank
		 FROM guild_members gm JOIN users u ON gm.user_id=u.id
		 WHERE gm.guild_id=$1
		 ORDER BY CASE gm.rank WHEN 'leader' THEN 0 WHEN 'officer' THEN 1 ELSE 2 END,
		          gm.joined_at`,
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

func ListGuilds() []GuildRow {
	rows, err := conn.Query(
		`SELECT g.id, g.name, g.tag, g.leader_id, u.username, g.description,
		        (SELECT COUNT(*) FROM guild_members WHERE guild_id=g.id) AS mc
		 FROM guilds g JOIN users u ON g.leader_id=u.id
		 ORDER BY mc DESC LIMIT 50`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []GuildRow
	for rows.Next() {
		var g GuildRow
		if rows.Scan(&g.ID, &g.Name, &g.Tag, &g.LeaderID, &g.LeaderName,
			&g.Description, &g.MemberCount) == nil {
			result = append(result, g)
		}
	}
	return result
}

func GetUserGuildID(userID int64) int64 {
	var id int64
	conn.QueryRow(`SELECT COALESCE(guild_id,0) FROM users WHERE id=$1`, userID).Scan(&id)
	return id
}
