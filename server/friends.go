package main

// ── DB helpers ────────────────────────────────────────────────────────────────

type FriendRow struct {
	UserID   int64
	Username string
	Status   string // "pending" | "accepted"
}

// dbSendFriendRequest creates a pending friend request from sender → target.
// Returns false if already exists (any status).
func dbSendFriendRequest(senderID, targetID int64) bool {
	_, err := database.Exec(
		`INSERT OR IGNORE INTO friends (user_id, friend_id, status) VALUES (?,?,'pending')`,
		senderID, targetID,
	)
	return err == nil
}

// dbAcceptFriend marks the friend request as accepted (both directions).
func dbAcceptFriend(userID, friendID int64) {
	database.Exec(
		`UPDATE friends SET status='accepted' WHERE user_id=? AND friend_id=?`,
		friendID, userID,
	)
	// Also create the reverse direction so both sides appear in lists.
	database.Exec(
		`INSERT OR IGNORE INTO friends (user_id, friend_id, status) VALUES (?,?,'accepted')`,
		userID, friendID,
	)
}

// dbRemoveFriend removes a friendship in both directions.
func dbRemoveFriend(userID, friendID int64) {
	database.Exec(`DELETE FROM friends WHERE (user_id=? AND friend_id=?) OR (user_id=? AND friend_id=?)`,
		userID, friendID, friendID, userID)
}

// dbGetFriends returns all accepted friends for userID.
func dbGetFriends(userID int64) []FriendRow {
	rows, err := database.Query(
		`SELECT u.id, u.username, f.status
		 FROM friends f JOIN users u ON f.friend_id = u.id
		 WHERE f.user_id = ?`, userID)
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

// dbGetPendingRequests returns friend requests received by userID (status=pending).
func dbGetPendingRequests(userID int64) []FriendRow {
	rows, err := database.Query(
		`SELECT u.id, u.username, 'pending'
		 FROM friends f JOIN users u ON f.user_id = u.id
		 WHERE f.friend_id = ? AND f.status = 'pending'`, userID)
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

// dbAreFriends returns true if userID and friendID are accepted friends.
func dbAreFriends(userID, friendID int64) bool {
	var n int
	database.QueryRow(
		`SELECT COUNT(*) FROM friends WHERE user_id=? AND friend_id=? AND status='accepted'`,
		userID, friendID,
	).Scan(&n)
	return n > 0
}
