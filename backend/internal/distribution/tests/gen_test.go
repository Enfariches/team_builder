package tests

import (
	"fmt"
	"math/rand/v2"
	"time"

	"builder/backend/internal/distribution"
)

var allPolicies = []distribution.RemainderPolicy{
	distribution.PolicySmallerLastTeam, distribution.PolicySpread, distribution.PolicyWaitlist,
}

type testCase struct {
	rules distribution.Rules
	cands []distribution.Candidate
	seed  int64
}

func (c testCase) String() string {
	return fmt.Sprintf("policy=%s k=%d roles=%v n=%d seed=%d", c.rules.Policy, c.rules.TeamSize, c.rules.Roles, len(c.cands), c.seed)
}

// genRules строит случайные, но допустимые правила: sum(min) <= k <= sum(max).
func genRules(rng *rand.Rand, policy distribution.RemainderPolicy) distribution.Rules {
	for {
		nRoles := 1 + rng.IntN(4)
		roles := make([]distribution.Role, nRoles)
		smin, smax := 0, 0
		for i := range roles {
			lo := rng.IntN(4)
			hi := max(lo, 1) + rng.IntN(4)
			roles[i] = distribution.Role{Name: fmt.Sprintf("R%d", i), Min: lo, Max: hi, SortOrder: rng.IntN(3)}
			smin += lo
			smax += hi
		}
		lo := max(smin, 1)
		if lo > smax {
			continue
		}
		return distribution.Rules{TeamSize: lo + rng.IntN(smax-lo+1), Roles: roles, Policy: policy}
	}
}

// genCandidates: уникальные id в случайном порядке, предпочтения с неизвестными
// ролями, повторами и пропусками рангов, часть анкет пустая.
func genCandidates(rng *rand.Rand, n int, roles []distribution.Role) []distribution.Candidate {
	ids := rng.Perm(n * 3)[:n]
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]distribution.Candidate, n)
	for i := range out {
		c := distribution.Candidate{
			MemberID:   int64(ids[i]) + 1,
			UserID:     int64(i) + 1000,
			JoinedAt:   base.Add(time.Duration(rng.IntN(20)) * time.Minute), // есть совпадения
			Experience: rng.IntN(11),
		}
		for j, cnt := 0, rng.IntN(len(roles)+2); j < cnt; j++ {
			name := fmt.Sprintf("R%d", rng.IntN(len(roles)+1)) // R<len> — неизвестная роль
			c.Preferences = append(c.Preferences, distribution.RolePreference{Role: name, Rank: 1 + rng.IntN(4)})
		}
		out[i] = c
	}
	return out
}

func genCase(rng *rand.Rand) testCase {
	policy := allPolicies[rng.IntN(len(allPolicies))]
	rules := genRules(rng, policy)
	var n int
	switch rng.IntN(3) {
	case 0:
		n = rng.IntN(3 * rules.TeamSize) // граница между «мало» и «хватает»
	case 1:
		n = rng.IntN(25)
	default:
		n = rng.IntN(121)
	}
	return testCase{rules: rules, cands: genCandidates(rng, n, rules.Roles), seed: int64(rng.Uint64())}
}
