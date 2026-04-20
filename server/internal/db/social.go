package db

// FriendRow represents one row from the friends join.
type FriendRow struct {
	UserID   int64
	Username string
	Status   string // "pending" | "accepted"
}

func SendFriendRequest(senderID, targetID int64) bool {
	_, err := conn.Exec(
		`INSERT INTO friends (user_id, friend_id, status) VALUES ($1, $2, 'pending')
		 ON CONFLICT (user_id, friend_id) DO NOTHING`,
		senderID, targetID,
	)
	return err == nil
}

func AcceptFriend(userID, friendID int64) {
	conn.Exec(
		`UPDATE friends SET status='accepted' WHERE user_id=$1 AND friend_id=$2`,
		friendID, userID,
	)
	conn.Exec(
		`INSERT INTO friends (user_id, friend_id, status) VALUES ($1, $2, 'accepted')
		 ON CONFLICT (user_id, friend_id) DO NOTHING`,
		userID, friendID,
	)
}

func RemoveFriend(userID, friendID int64) {
	conn.Exec(
		`DELETE FROM friends WHERE (user_id=$1 AND friend_id=$2) OR (user_id=$2 AND friend_id=$1)`,
		userID, friendID,
	)
}

func GetFriends(userID int64) []FriendRow {
	rows, err := conn.Query(
		`SELECT u.id, u.username, f.status
		 FROM friends f JOIN users u ON f.friend_id = u.id
		 WHERE f.user_id = $1`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []FriendRow
	for rows.Next() {
		var r FriendRow
		if rows.Scan(&r.UserID, &r.Username, &r.Status) == nil {
			result = append(result, r)
		}
	}
	return result
}

func GetPendingRequests(userID int64) []FriendRow {
	rows, err := conn.Query(
		`SELECT u.id, u.username, 'pending'
		 FROM friends f JOIN users u ON f.user_id = u.id
		 WHERE f.friend_id = $1 AND f.status = 'pending'`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []FriendRow
	for rows.Next() {
		var r FriendRow
		if rows.Scan(&r.UserID, &r.Username, &r.Status) == nil {
			result = append(result, r)
		}
	}
	return result
}

func AreFriends(userID, friendID int64) bool {
	var n int
	conn.QueryRow(
		`SELECT COUNT(*) FROM friends
		 WHERE user_id=$1 AND friend_id=$2 AND status='accepted'`,
		userID, friendID,
	).Scan(&n)
	return n > 0
}
