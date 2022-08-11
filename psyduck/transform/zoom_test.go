package transform

import (
	"testing"

	"github.com/gastrodon/psyduck/sdk"
)

var cases = []struct {
	Field  string
	Source []byte
	Want   []byte
}{
	{
		Field:  "who",
		Source: []byte(`{"who": {"cat": "huge"}}`),
		Want:   []byte(`{"cat": "huge"}`),
	},
	{
		Field:  "cats",
		Source: []byte(`{"cats": ["huge", "alice", "edward", "pixie"]}`),
		Want:   []byte(`["huge", "alice", "edward", "pixie"]`),
	},
}

func TestZoom(test *testing.T) {
	for index, testcase := range cases {
		parse := func(target interface{}) error {
			target.(*ZoomConfig).Field = testcase.Field

			return nil
		}

		transformer, err := Zoom(parse, nil) // TODO
		if err != nil {
			test.Fatal(err)
		}

		zoomed, err := transformer(testcase.Source)
		if err != nil {
			test.Fatal(err)
		}

		if !sdk.SameBytes(zoomed, testcase.Want) {
			test.Fatalf("zoomed does not match #%d! \nzoomed: %s\nwant:%s", index, zoomed, testcase.Want)
		}
	}
}
