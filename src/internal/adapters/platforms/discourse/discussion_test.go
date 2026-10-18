package discourse

import (
	"tangentry/internal/content"
	"testing"
)

func TestMissingPostIDsReturnsUnloadedIDsInOrder(t *testing.T) {
	got := MissingPostIDs([]int64{10, 11, 12, 13}, []content.DiscussionPost{{ID: 10}, {ID: 12}}, 20)
	if len(got) != 2 || got[0] != 11 || got[1] != 13 {
		t.Fatalf("missing ids = %#v", got)
	}
	got = MissingPostIDs([]int64{10, 11, 12}, nil, 1)
	if len(got) != 1 || got[0] != 10 {
		t.Fatalf("bounded ids = %#v", got)
	}
}
