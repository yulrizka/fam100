package model

import "testing"

func TestAdd(t *testing.T) {
	r1 := make(Rank, 1)
	r1[0] = PlayerScore{PlayerID: "a", Score: 1}
	r2 := make(Rank, 2)
	r2[0] = PlayerScore{PlayerID: "a", Score: 2}
	r2[1] = PlayerScore{PlayerID: "b", Score: 4}

	r3 := r1.Add(r2)
	if want, got := 1, r1[0].Score; want != got {
		t.Errorf("r1[0] want %d got %d", want, got)
	}
	if want, got := 2, r2[0].Score; want != got {
		t.Errorf("r2[0] want %d got %d", want, got)
	}
	if want, got := 4, r2[1].Score; want != got {
		t.Errorf("r2[1] want %d got %d", want, got)
	}

	if want, got := 2, len(r3); want != got {
		t.Errorf("len(r3) want %d got %d", want, got)
	}
	if want, got := 4, r3[0].Score; want != got {
		t.Errorf("r3[0] want %d got %d", want, got)
	}
	if want, got := 3, r3[1].Score; want != got {
		t.Errorf("r3[1] want %d got %d", want, got)
	}
}

func TestSubtract(t *testing.T) {
	r1 := make(Rank, 3)
	r1[0] = PlayerScore{PlayerID: "a", Score: 2}
	r1[1] = PlayerScore{PlayerID: "b", Score: 1}
	r1[2] = PlayerScore{PlayerID: "c", Score: 1}
	r2 := make(Rank, 4)
	r2[0] = PlayerScore{PlayerID: "a", Score: 1}
	r2[1] = PlayerScore{PlayerID: "b", Score: 4}
	r2[2] = PlayerScore{PlayerID: "c", Score: 1}
	r2[3] = PlayerScore{PlayerID: "d", Score: 4}

	r3 := r1.Subtract(r2)
	if want, got := 3, len(r3); want != got {
		t.Errorf("len(r3) want %d got %d", want, got)
	}

	if want, got := 2, r1[0].Score; want != got {
		t.Errorf("r1[0] want %d got %d", want, got)
	}

	if want, got := 1, r3[0].Score; want != got {
		t.Errorf("r3[0] want %d got %d", want, got)
	}
	if want, got := 0, r3[1].Score; want != got {
		t.Errorf("r3[1] want %d got %d", want, got)
	}
	if want, got := 0, r3[2].Score; want != got {
		t.Errorf("r3[2] want %d got %d", want, got)
	}
}
