package main

// PlayerState represents a player's synchronized state.
type PlayerState struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Dir       int     `json:"dir"`
	Moving    bool    `json:"moving"`
	Body      string  `json:"body,omitempty"`
	Head      string  `json:"head,omitempty"`
	Hat       string  `json:"hat,omitempty"`
	Shield    string  `json:"shield,omitempty"`
	Sword     string  `json:"sword,omitempty"`
	Gralats   int     `json:"gralats,omitempty"`
	Playtime  int     `json:"playtime,omitempty"`
	AnimState string  `json:"anim,omitempty"`
	Mounted   bool    `json:"mounted,omitempty"`
	HP        int     `json:"hp,omitempty"`
	MaxHP     int     `json:"maxHp,omitempty"`
}

// NPCState represents an NPC's synchronized state.
type NPCState struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Dir       int     `json:"dir"`
	Moving    bool    `json:"moving"`
	NPCType   int     `json:"npcType"`
	HP        int     `json:"hp"`
	MaxHP     int     `json:"maxHp"`
	MountedBy string  `json:"mountedBy,omitempty"`
	AnimState string  `json:"anim,omitempty"`
}

// GralatPickup is a collectable gralat coin in the world.
type GralatPickup struct {
	ID    string  `json:"id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Value int     `json:"value"` // 1, 5, 30, or 100
}

// InventoryItem represents a usable item in the player's inventory.
type InventoryItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Gani     string `json:"gani"`
	Quantity int    `json:"quantity"`
}

// WorldSpawnItem is an admin-spawned decorative / shop item visible to all players.
type WorldSpawnItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	SpritePath string  `json:"sprite"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Price      int     `json:"price"`
	ItemID     string  `json:"item_id,omitempty"`
}

// FriendEntry represents a friend or pending request.
type FriendEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "accepted" | "pending"
	Online bool   `json:"online"`
}

// GuildMemberEntry is one member in a guild roster.
type GuildMemberEntry struct {
	Name   string `json:"name"`
	Rank   string `json:"rank"`
	Online bool   `json:"online"`
}

// GuildInfo is the full guild data sent from the server.
type GuildInfo struct {
	ID      int64              `json:"id"`
	Name    string             `json:"name"`
	Tag     string             `json:"tag"`
	Leader  string             `json:"leader"`
	Desc    string             `json:"desc"`
	Members []GuildMemberEntry `json:"members"`
}

// GuildListEntry is a compact guild entry for the browse list.
type GuildListEntry struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Tag     string `json:"tag"`
	Leader  string `json:"leader"`
	Members int    `json:"members"`
}

// QuestEntry is a single quest with current progress.
type QuestEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Desc        string `json:"desc"`
	Objective   string `json:"objective"`
	Progress    int    `json:"progress"`
	Required    int    `json:"required"`
	Completed   bool   `json:"completed"`
	Reward      int    `json:"reward"`
}

// ServerMessage is a general-purpose struct for unmarshaling server messages.
type ServerMessage struct {
	Type       string           `json:"type"`
	Players    []PlayerState    `json:"players,omitempty"`
	NPCs       []NPCState       `json:"npcs,omitempty"`
	Gralats    []GralatPickup   `json:"gralats,omitempty"`
	WorldItems []WorldSpawnItem `json:"world_items,omitempty"`
	Inventory  []InventoryItem  `json:"inventory,omitempty"`
	From       string           `json:"from,omitempty"`
	Msg        string           `json:"msg,omitempty"`
	ID         string           `json:"id,omitempty"`
	Name       string           `json:"name,omitempty"`
	X          float64          `json:"x,omitempty"`
	Y          float64          `json:"y,omitempty"`
	GralatN    int              `json:"gralat_n,omitempty"`
	Playtime   int              `json:"playtime,omitempty"`
	NPCID      string           `json:"npc_id,omitempty"`
	HP         int              `json:"hp"`
	Killed     bool             `json:"killed,omitempty"`
	Body       string           `json:"body,omitempty"`
	Head       string           `json:"head,omitempty"`
	Hat        string            `json:"hat,omitempty"`
	Shield     string           `json:"shield,omitempty"`
	Sword      string           `json:"sword,omitempty"`
	Damage     int              `json:"damage,omitempty"`
	AtkX       float64          `json:"atk_x,omitempty"`
	AtkY       float64          `json:"atk_y,omitempty"`
	MaxHP      int              `json:"maxHp,omitempty"`
	// New fields
	IsAdmin  bool   `json:"is_admin,omitempty"`
	PlayerID string `json:"player_id,omitempty"`
	AnimSt   string `json:"anim,omitempty"`
	ItemID   string `json:"item_id,omitempty"`
	Success  bool   `json:"success,omitempty"`
	Map      string `json:"map,omitempty"`

	// Friends
	Friends  []FriendEntry `json:"friends,omitempty"`
	Requests []FriendEntry `json:"requests,omitempty"`

	// Guilds
	Guild      *GuildInfo       `json:"guild,omitempty"`
	Guilds     []GuildListEntry `json:"guilds,omitempty"`
	GuildID    int64            `json:"guild_id,omitempty"`

	// Quests
	Quests   []QuestEntry `json:"quests,omitempty"`
	QuestID  string       `json:"quest_id,omitempty"`
	Progress int          `json:"progress,omitempty"`
	Required int          `json:"required,omitempty"`
}
