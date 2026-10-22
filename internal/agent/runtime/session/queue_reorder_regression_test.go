package sessionruntime

import (
	"context"
	"fmt"
	"testing"
)

func TestFollowUpRepeatedReorderPreservesCurrentOrder(t *testing.T) {
	b, key, _ := liveQueueFixture(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c", "d"} {
		if _, err := b.EnqueueFollowUp(ctx, key, id, id, []byte(id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.ReorderFollowUp(ctx, key, FollowUpPendingRef{ItemID: "d"}, FollowUpPendingRef{ItemID: "a"}); err != nil {
		t.Fatal(err)
	}
	got, err := b.ReorderFollowUp(ctx, key, FollowUpPendingRef{ItemID: "c"}, FollowUpPendingRef{ItemID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, item := range got {
		ids = append(ids, string(item.ID))
	}
	if fmt.Sprint(ids) != "[d a c b]" {
		t.Fatalf("second reorder = %v, want [d a c b]; first move was lost", ids)
	}
}
