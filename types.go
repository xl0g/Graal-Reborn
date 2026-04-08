package main

// PlayerState represents a player's synchronized state.
type PlayerState struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Dir    int     `json:"dir"`
	Moving bool    `json:"moving"`
}

// NPCState represents an NPC's synchronized state.
type NPCState struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Dir     int     `json:"dir"`
	Moving  bool    `json:"moving"`
	NPCType int     `json:"npcType"`
}

// ServerMessage is a general-purpose struct for unmarshaling server messages.
type ServerMessage struct {
	Type    string        `json:"type"`
	Players []PlayerState `json:"players,omitempty"`
	NPCs    []NPCState    `json:"npcs,omitempty"`
	From    string        `json:"from,omitempty"`
	Msg     string        `json:"msg,omitempty"`
	ID      string        `json:"id,omitempty"`
	Name    string        `json:"name,omitempty"`
	X       float64       `json:"x,omitempty"`
	Y       float64       `json:"y,omitempty"`
}
