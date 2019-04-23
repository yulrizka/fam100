package model

// PlayerID is the player ID type
type PlayerID string

// Player of the game
type Player struct {
	ID   PlayerID
	Name string
}
