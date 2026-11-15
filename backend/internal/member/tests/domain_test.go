package tests

import (
	"errors"
	"testing"

	"builder/backend/internal/member"
)

func TestCheckCanJoin(t *testing.T) {
	tests := []struct {
		name    string
		info    member.SplitInfo
		user    int64
		already bool
		count   int
		want    error
	}{
		{"открытый набор без лимита", openSplit(nil), student, false, 1000, nil},
		{"есть место", openSplit(ptr(3)), student, false, 2, nil},
		{"мест нет", openSplit(ptr(3)), student, false, 3, member.ErrSplitFull},
		{"лимит превышен данными", openSplit(ptr(3)), student, false, 5, member.ErrSplitFull},
		{"повторное вступление", openSplit(nil), student, true, 1, member.ErrAlreadyMember},
		{"владелец не вступает в своё пространство", openSplit(nil), owner, false, 0, member.ErrAlreadyMember},
		{"черновик", member.SplitInfo{OwnerID: owner, Status: "Draft"}, student, false, 0, member.ErrSplitNotOpen},
		{"набор закрыт", member.SplitInfo{OwnerID: owner, Status: "Closed"}, student, false, 0, member.ErrSplitNotOpen},
		{"завершено", member.SplitInfo{OwnerID: owner, Status: "Finished"}, student, false, 0, member.ErrSplitNotOpen},
		{"повтор важнее закрытого набора", member.SplitInfo{OwnerID: owner, Status: "Closed"}, student, true, 0, member.ErrAlreadyMember},
		{"закрытый набор важнее нехватки мест", member.SplitInfo{OwnerID: owner, Status: "Closed", MaxMembers: ptr(1)}, student, false, 1, member.ErrSplitNotOpen},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := member.CheckCanJoin(tc.info, tc.user, tc.already, tc.count)
			if !errors.Is(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
