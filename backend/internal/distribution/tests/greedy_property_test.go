package tests

import (
	"errors"
	"math/rand/v2"
	"reflect"
	"testing"

	"builder/backend/internal/distribution"
)

const (
	metaSeed = 20261116
	cases    = 3000
)

// independentChecks — минимальный набор инвариантов, написанный отдельно от
// distribution.Validate, чтобы тест не доверял проверяющему коду.
func independentChecks(t *testing.T, tc testCase, res distribution.Result) {
	t.Helper()
	n := len(tc.cands)
	k := tc.rules.TeamSize
	seen := map[int64]int{}
	for _, tm := range res.Teams {
		for _, m := range tm.Members {
			seen[m.MemberID]++
		}
	}
	for _, id := range res.Waitlist {
		seen[id]++
	}
	if len(seen) != n {
		t.Fatalf("%s: распределено %d из %d", tc, len(seen), n)
	}
	for _, c := range tc.cands {
		if seen[c.MemberID] != 1 {
			t.Fatalf("%s: участник %d учтён %d раз", tc, c.MemberID, seen[c.MemberID])
		}
	}
	if len(res.Waitlist) > 0 && tc.rules.Policy != distribution.PolicyWaitlist {
		t.Fatalf("%s: лист ожидания при политике %s", tc, tc.rules.Policy)
	}

	minSize, maxSize := 1<<30, 0
	for _, tm := range res.Teams {
		size := len(tm.Members)
		minSize, maxSize = min(minSize, size), max(maxSize, size)
		counts := map[string]int{}
		for _, m := range tm.Members {
			counts[m.Role]++
		}
		for _, r := range tc.rules.Roles {
			if counts[r.Name] < r.Min || counts[r.Name] > r.Max {
				t.Fatalf("%s: %s, роль %s: %d вне [%d,%d]", tc, tm.Name, r.Name, counts[r.Name], r.Min, r.Max)
			}
		}
		switch tc.rules.Policy {
		case distribution.PolicyWaitlist:
			if size != k {
				t.Fatalf("%s: размер %d, ждали %d", tc, size, k)
			}
		case distribution.PolicySpread:
			if size < k {
				t.Fatalf("%s: размер %d меньше %d", tc, size, k)
			}
		case distribution.PolicySmallerLastTeam:
			if size > k {
				t.Fatalf("%s: размер %d больше %d", tc, size, k)
			}
		}
	}
	switch tc.rules.Policy {
	case distribution.PolicyWaitlist:
		if len(res.Waitlist) >= k {
			t.Fatalf("%s: в листе ожидания %d >= %d", tc, len(res.Waitlist), k)
		}
	case distribution.PolicySpread:
		if maxSize-minSize > 1 {
			t.Fatalf("%s: разброс размеров %d..%d", tc, minSize, maxSize)
		}
	}
}

func checkCase(t *testing.T, tc testCase) {
	t.Helper()
	res, err := distribution.Distribute(tc.rules, tc.cands, tc.seed)
	if err != nil {
		// штатные отказы: участников мало или состав не влезает в квоты
		if errors.Is(err, distribution.ErrNotEnoughMembers) || errors.Is(err, distribution.ErrInfeasible) {
			return
		}
		t.Fatalf("%s: неожиданная ошибка: %v", tc, err)
	}

	independentChecks(t, tc, res)
	if v := distribution.Validate(tc.rules, tc.cands, res); len(v) != 0 {
		t.Fatalf("%s: Validate: %v", tc, v)
	}

	// детерминизм: тот же seed — тот же результат
	again, err := distribution.Distribute(tc.rules, tc.cands, tc.seed)
	if err != nil || !reflect.DeepEqual(res, again) {
		t.Fatalf("%s: повторный прогон отличается (err=%v)", tc, err)
	}
	// порядок входа не влияет
	rng := rand.New(rand.NewPCG(uint64(tc.seed), 7))
	perm := append([]distribution.Candidate(nil), tc.cands...)
	rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
	shuffledRes, err := distribution.Distribute(tc.rules, perm, tc.seed)
	if err != nil || !reflect.DeepEqual(res, shuffledRes) {
		t.Fatalf("%s: результат зависит от порядка входа (err=%v)", tc, err)
	}
}

// Инварианты распределения на случайных входах. Мета-seed фиксирован, поэтому
// падение воспроизводится; тест печатает параметры упавшего случая.
func TestDistributeProperties(t *testing.T) {
	rng := rand.New(rand.NewPCG(metaSeed, 1))
	for i := 0; i < cases; i++ {
		checkCase(t, genCase(rng))
	}
}
