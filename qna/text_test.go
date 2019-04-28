package qna

import (
	"testing"
)

func TestNewText(t *testing.T) {
	text, err := NewText("famili100.txt")
	if err != nil {
		t.Fatal(err)
	}

	count, err := text.Count()
	if err != nil {
		t.Fatal(err)
	}

	if count == 0 {
		t.Error("got empty questions")
	}

	_, err = text.NextQuestion(1, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
}
