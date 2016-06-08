package fam100

import "testing"

func TestAdd(t *testing.T) {
	r1 := make(rank, 1)
	r1[0] = playerScore{PlayerID: "a", Score: 1}
	r2 := make(rank, 2)
	r2[0] = playerScore{PlayerID: "a", Score: 2}
	r2[1] = playerScore{PlayerID: "b", Score: 4}

	r3 := r1.add(r2)
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
