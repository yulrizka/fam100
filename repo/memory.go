package repo

import (
	"github.com/yulrizka/fam100/model"
)

// MemoryDB stores data in non persistence way
type MemoryDB struct {
	Seed   int64
	played int
}

func (m *MemoryDB) Reset() error      { return nil }
func (m *MemoryDB) Init() (err error) { return nil }
func (m *MemoryDB) ChannelRanking(chanID string, limit int) (ranking model.Rank, err error) {
	return nil, nil
}
func (m *MemoryDB) ChannelCount() (total int, err error)                           { return 0, nil }
func (m *MemoryDB) Channels() (channels map[string]string, err error)              { return nil, nil }
func (m *MemoryDB) ChannelConfig(chanID, key, defaultValue string) (string, error) { return "", nil }
func (m *MemoryDB) GlobalConfig(key, defaultValue string) (string, error)          { return "", nil }
func (m *MemoryDB) PlayerCount() (total int, err error)                            { return 0, nil }
func (m *MemoryDB) IncStats(key string) error                                      { return nil }
func (m *MemoryDB) IncChannelStats(chanID, key string) error                       { return nil }
func (m *MemoryDB) IncPlayerStats(playerID model.PlayerID, key string) error       { return nil }
func (m *MemoryDB) Stats(key string) (interface{}, error)                          { return nil, nil }
func (m *MemoryDB) ChannelStats(chanID, key string) (interface{}, error)           { return nil, nil }
func (m *MemoryDB) PlayerStats(playerID, key string) (interface{}, error)          { return nil, nil }
func (m *MemoryDB) SaveScore(chanID, chanName string, scores model.Rank) error     { return nil }
func (m *MemoryDB) PlayerRanking(limit int) (model.Rank, error)                    { return nil, nil }
func (m *MemoryDB) PlayerScore(playerID model.PlayerID) (ps model.PlayerScore, err error) {
	return model.PlayerScore{}, nil
}
func (m *MemoryDB) PlayerChannelScore(chanID string, playerID model.PlayerID) (model.PlayerScore, error) {
	return model.PlayerScore{}, nil
}

func (m *MemoryDB) NextGame(chanID string) (seed int64, nextRound int, err error) {
	return m.Seed, m.played + 1, nil
}
func (m *MemoryDB) IncRoundPlayed(chanID string) error {
	m.played++
	return nil
}
