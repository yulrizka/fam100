package model

import "sort"

type PlayerScore struct {
	PlayerID PlayerID `json:"playerID"`
	Name     string   `json:"name"`
	Score    int      `json:"score"`
	Position int      `json:"position"`
}

type Rank []PlayerScore

func (r Rank) Len() int           { return len(r) }
func (r Rank) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r Rank) Less(i, j int) bool { return r[i].Score > r[j].Score }

func (r Rank) Add(source Rank) Rank {
	lookup := make(map[PlayerID]PlayerScore)
	for _, v := range r {
		lookup[v.PlayerID] = v
	}

	for _, s := range source {
		if ps, ok := lookup[s.PlayerID]; !ok {
			lookup[s.PlayerID] = s
		} else {
			ps.Name = s.Name
			ps.Score += s.Score
			lookup[s.PlayerID] = ps
		}
	}

	result := make(Rank, 0, len(lookup))
	for _, v := range lookup {
		result = append(result, v)
	}
	sort.Sort(result)
	for i := 0; i < len(result); i++ {
		result[i].Position = i + 1
	}
	return result
}

func (r Rank) Subtract(source Rank) Rank {
	result := make(Rank, len(r))
	copy(result, r)
	lookup := make(map[PlayerID]int)
	for i, v := range r {
		lookup[v.PlayerID] = i
	}

	for _, v := range source {
		if ix, ok := lookup[v.PlayerID]; ok {
			if newVal := result[ix].Score - v.Score; newVal >= 0 {
				result[ix].Score = newVal
			} else {
				result[ix].Score = 0
			}
		}
	}

	return result
}
