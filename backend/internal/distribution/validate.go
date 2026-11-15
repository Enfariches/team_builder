package distribution

import (
	"fmt"
	"math"
	"slices"
)

// Validate независимо от алгоритма проверяет инварианты результата и
// возвращает перечень нарушений. Пустой результат — распределение корректно.
// Сервис вызывает её перед сохранением: нарушение означает баг алгоритма, а не
// ошибку клиента.
func Validate(rules Rules, cands []Candidate, res Result) []string {
	var v []string
	add := func(format string, args ...any) { v = append(v, fmt.Sprintf(format, args...)) }

	want := make(map[int64]bool, len(cands))
	for _, c := range cands {
		want[c.MemberID] = true
	}
	seen := make(map[int64]int, len(cands))
	for _, t := range res.Teams {
		for _, m := range t.Members {
			seen[m.MemberID]++
		}
	}
	for _, id := range res.Waitlist {
		seen[id]++
	}
	for id, n := range seen {
		if !want[id] {
			add("участник %d не из состава", id)
		}
		if n != 1 {
			add("участник %d встречается %d раз", id, n)
		}
	}
	for id := range want {
		if seen[id] == 0 {
			add("участник %d не распределён", id)
		}
	}

	n := len(cands)
	k := rules.TeamSize
	smin, smax := 0, 0
	roles := make(map[string]Role, len(rules.Roles))
	for _, r := range rules.Roles {
		smin += r.Min
		smax += r.Max
		roles[r.Name] = r
	}

	wantTeams := 0
	switch rules.Policy {
	case PolicyWaitlist:
		wantTeams = n / k
		if len(res.Waitlist) != n-wantTeams*k {
			add("в листе ожидания %d чел., ожидалось %d", len(res.Waitlist), n-wantTeams*k)
		}
	case PolicySpread:
		wantTeams = n / k
	case PolicySmallerLastTeam:
		wantTeams = (n + k - 1) / k
	}
	if rules.Policy != PolicyWaitlist && len(res.Waitlist) > 0 {
		add("лист ожидания допустим только при политике waitlist")
	}
	if len(res.Teams) != wantTeams {
		add("команд %d, ожидалось %d", len(res.Teams), wantTeams)
	}

	minSize, maxSize := math.MaxInt, 0
	for _, t := range res.Teams {
		size := len(t.Members)
		minSize, maxSize = min(minSize, size), max(maxSize, size)
		switch rules.Policy {
		case PolicyWaitlist:
			if size != k {
				add("%s: %d чел., ожидалось %d", t.Name, size, k)
			}
		case PolicySpread:
			if size < k || size > smax {
				add("%s: %d чел. вне [%d, %d]", t.Name, size, k, smax)
			}
		case PolicySmallerLastTeam:
			if size > k || size < smin {
				add("%s: %d чел. вне [%d, %d]", t.Name, size, smin, k)
			}
		}
		count := map[string]int{}
		for _, m := range t.Members {
			if _, ok := roles[m.Role]; !ok {
				add("%s: участник %d с неизвестной ролью %q", t.Name, m.MemberID, m.Role)
				continue
			}
			count[m.Role]++
		}
		for _, r := range rules.Roles {
			if c := count[r.Name]; c < r.Min || c > r.Max {
				add("%s: роль %s — %d чел. вне [%d, %d]", t.Name, r.Name, c, r.Min, r.Max)
			}
		}
	}
	if rules.Policy == PolicySpread && len(res.Teams) > 0 && maxSize-minSize > 1 {
		add("размеры команд различаются больше чем на 1: от %d до %d", minSize, maxSize)
	}
	return v
}

// ComputeMetrics считает показатели качества для сравнения прогонов.
func ComputeMetrics(rules Rules, cands []Candidate, res Result) Metrics {
	byID := make(map[int64]Candidate, len(cands))
	for _, c := range cands {
		byID[c.MemberID] = c
	}
	m := Metrics{
		TeamsCount:          len(res.Teams),
		MembersWaitlisted:   len(res.Waitlist),
		RoleQuotasSatisfied: len(Validate(rules, cands, res)) == 0,
	}

	roles := slices.Clone(rules.Roles)
	withPrefs, gotFirst := 0, 0
	avgs := make([]float64, 0, len(res.Teams))
	for _, t := range res.Teams {
		sum := 0
		for _, a := range t.Members {
			m.MembersAssigned++
			c := byID[a.MemberID]
			sum += c.Experience
			if p := knownPreferences(c, roles); len(p) > 0 {
				withPrefs++
				if p[0] == a.Role {
					gotFirst++
				}
			}
		}
		if len(t.Members) > 0 {
			avgs = append(avgs, float64(sum)/float64(len(t.Members)))
		}
	}
	if withPrefs > 0 {
		m.FirstChoiceRoleShare = round2(float64(gotFirst) / float64(withPrefs))
	}
	if len(avgs) > 1 {
		mean := 0.0
		for _, a := range avgs {
			mean += a
		}
		mean /= float64(len(avgs))
		variance := 0.0
		for _, a := range avgs {
			variance += (a - mean) * (a - mean)
		}
		m.ExperienceSpread = round2(math.Sqrt(variance / float64(len(avgs))))
	}
	return m
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
