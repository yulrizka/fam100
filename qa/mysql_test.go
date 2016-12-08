package qa

import (
	"fmt"
	"reflect"
	"testing"
)

const dbName = "fam100_test"

func TestMysql(t *testing.T) {
	var err error
	mysql, err := NewMysql("root:root@/" + dbName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mysql.db.Exec("TRUNCATE questions")
	if err != nil {
		t.Fatal(err)
	}

	var db Provider = mysql

	ans := []Answer{
		{Text: []string{"Answer 1", "alias Answer 1"}, Score: 10},
		{Text: []string{"answer 2"}, Score: 20},
	}
	qTpl := NewQuestion("Question text", ans, Pending, true)

	t.Run("add_get", func(t *testing.T) {
		q := qTpl
		if err := db.Add(&q); err != nil {
			t.Fatal(err)
		}
		if q.ID == 0 {
			t.Fatal("got id 0, want > 0")
		}

		// letter case should be normalize
		got, err := db.Get(q.ID)
		if err != nil {
			t.Error(err)
		}
		fmt.Printf("got = %+v\n", q.Format())
		if !reflect.DeepEqual(*got, q) {
			t.Errorf("got %+v want %+v", *got, q)
		}

		// delete
		if err := db.Delete(q.ID); err != nil {
			if err != nil {
				t.Fatalf("delete failed, %s", err)
			}
		}

		got, err = db.Get(q.ID)
		if err != nil {
			t.Fatal("add still exists after deletion")
		}
		if got != nil {
			t.Error("got %+v want nil", got)
		}
	})
}
