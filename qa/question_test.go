package qa

import (
	"reflect"
	"testing"
)

var ()

func TestQuestionString(t *testing.T) {
	q := Question{
		ID:   5,
		Text: "Question text",
		Answers: []Answer{
			{Text: []string{"Answer 1", "alias Answer 1"}, Score: 10},
			{Text: []string{"answer 2"}, Score: 20},
		},
		Status: Pending,
		Active: true,
	}
	got := q.Format()
	want := "5;question text;10:answer 1 / alias answer 1*20:answer 2;pending;true"

	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestParse(t *testing.T) {
	got, err := Parse("5;question text;10:answer 1 / alias answer 1*20:answer 2;pending;true")
	if err != nil {
		t.Fatal(err)
	}

	want := Question{
		ID:   5,
		Text: "question text",
		Answers: []Answer{
			{Text: []string{"answer 1", "alias answer 1"}, Score: 10},
			{Text: []string{"answer 2"}, Score: 20},
		},
		Status: Pending,
		Active: true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}

}
