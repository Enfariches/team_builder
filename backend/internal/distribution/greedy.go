package distribution

import (
	"cmp"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"sort"
)

// Distribute — алгоритм greedy_v1. Результат зависит только от состава
// кандидатов (не от порядка в срезе), правил и seed.
//
//  1. PlanSizes определяет размеры команд и лист ожидания по политике остатка.
//  2. Для каждой команды набираются слоты ролей: сначала min, остальное — ролям
//     с наибольшим неудовлетворённым спросом на первый выбор.
//  3. Роли раздаются кандидатам раундами по приоритету из анкеты в порядке,
//     перемешанном по seed; остаток добирается свободными ролями.
//  4. Внутри роли кандидаты по убыванию опыта идут в команду с наименьшей
//     суммой опыта среди тех, где слот роли свободен.
func Distribute(rules Rules, cands []Candidate, seed int64) (Result, error) {
	if err := checkRules(rules); err != nil {
		return Result{}, err
	}
	sorted := slices.Clone(cands)
	slices.SortFunc(sorted, func(a, b Candidate) int { return cmp.Compare(a.MemberID, b.MemberID) })

	plan, err := PlanSizes(len(sorted), rules)
	if err != nil {
		return Result{}, err
	}

	// в лист ожидания уходят последние вступившие
	pool := sorted
	var waitlist []int64
	if plan.Waitlisted > 0 {
		byJoin := slices.Clone(sorted)
		slices.SortFunc(byJoin, func(a, b Candidate) int {
			if c := a.JoinedAt.Compare(b.JoinedAt); c != 0 {
				return c
			}
			return cmp.Compare(a.MemberID, b.MemberID)
		})
		cut := len(byJoin) - plan.Waitlisted
		for _, c := range byJoin[cut:] {
			waitlist = append(waitlist, c.MemberID)
		}
		slices.Sort(waitlist)
		out := make(map[int64]bool, len(waitlist))
		for _, id := range waitlist {
			out[id] = true
		}
		pool = pool[:0:0]
		for _, c := range sorted {
			if !out[c.MemberID] {
				pool = append(pool, c)
			}
		}
	}

	roles := slices.Clone(rules.Roles)
	slices.SortFunc(roles, func(a, b Role) int {
		if c := cmp.Compare(a.SortOrder, b.SortOrder); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})

	rng := rand.New(rand.NewPCG(uint64(seed), 0x9E3779B97F4A7C15))
	order := shuffled(len(pool), rng) // order[i] — индекс в pool в порядке обработки
	prefs := make([][]string, len(pool))
	for i, c := range pool {
		prefs[i] = knownPreferences(c, roles)
	}

	slots := allocateSlots(plan.Sizes, roles, pool, prefs)
	total := make(map[string]int, len(roles))
	for _, team := range slots {
		for role, n := range team {
			total[role] += n
		}
	}

	roleOf := assignRoles(roles, order, prefs, total)

	teams := placeInTeams(plan.Sizes, roles, pool, order, roleOf, slots)
	return Result{Teams: teams, Waitlist: waitlist}, nil
}

// SizePlan — размеры команд и число участников в листе ожидания.
type SizePlan struct {
	Sizes      []int
	Waitlisted int
}

// PlanSizes раскладывает n участников по командам согласно политике остатка.
func PlanSizes(n int, rules Rules) (SizePlan, error) {
	k := rules.TeamSize
	smin, smax := 0, 0
	for _, r := range rules.Roles {
		smin += r.Min
		smax += r.Max
	}

	switch rules.Policy {
	case PolicyWaitlist:
		t := n / k
		if t == 0 {
			return SizePlan{}, ErrNotEnoughMembers
		}
		sizes := make([]int, t)
		for i := range sizes {
			sizes[i] = k
		}
		return SizePlan{Sizes: sizes, Waitlisted: n - t*k}, nil

	case PolicySpread:
		t := n / k
		if t == 0 {
			return SizePlan{}, ErrNotEnoughMembers
		}
		base, extra := n/t, n%t
		largest := base
		if extra > 0 {
			largest++
		}
		if largest > smax {
			return SizePlan{}, infeasible(
				"при политике spread в команде получается %d чел., а роли вмещают не более %d; выберите waitlist или smaller_last_team, либо расширьте perTeamMax",
				largest, smax)
		}
		sizes := make([]int, t)
		for i := range sizes {
			sizes[i] = base
			if i < extra {
				sizes[i]++
			}
		}
		return SizePlan{Sizes: sizes}, nil

	case PolicySmallerLastTeam:
		if n == 0 || n < smin {
			return SizePlan{}, ErrNotEnoughMembers
		}
		t := (n + k - 1) / k
		sizes := make([]int, t)
		for i := range sizes {
			sizes[i] = k
		}
		last := n - (t-1)*k
		sizes[t-1] = last
		if last < smin {
			// добираем последнюю команду за счёт полных, пока у них есть запас над min
			need := smin - last
			if capacity := (t - 1) * (k - smin); need > capacity {
				return SizePlan{}, infeasible(
					"при политике smaller_last_team последняя команда получается из %d чел., а квоты требуют не меньше %d, и полные команды не могут отдать недостающих; выберите waitlist или spread",
					last, smin)
			}
			for i := 0; need > 0; i = (i + 1) % (t - 1) {
				if sizes[i] > smin {
					sizes[i]--
					sizes[t-1]++
					need--
				}
			}
		}
		return SizePlan{Sizes: sizes}, nil
	}
	return SizePlan{}, fmt.Errorf("unknown remainder policy %q", rules.Policy)
}

// shuffled возвращает перестановку 0..n-1 (Фишер—Йейтс на заданном генераторе).
func shuffled(n int, rng *rand.Rand) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := int(rng.Uint64N(uint64(i + 1)))
		p[i], p[j] = p[j], p[i]
	}
	return p
}

// knownPreferences — роли из анкеты, существующие в пространстве, по возрастанию
// ранга (при равных рангах — в порядке анкеты), без повторов.
func knownPreferences(c Candidate, roles []Role) []string {
	known := make(map[string]bool, len(roles))
	for _, r := range roles {
		known[r.Name] = true
	}
	ps := slices.Clone(c.Preferences)
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Rank < ps[j].Rank })
	var out []string
	seen := map[string]bool{}
	for _, p := range ps {
		if known[p.Role] && !seen[p.Role] {
			seen[p.Role] = true
			out = append(out, p.Role)
		}
	}
	return out
}

// allocateSlots решает, сколько мест каждой роли в каждой команде. Свободные
// сверх min места достаются роли с наибольшим неудовлетворённым спросом.
func allocateSlots(sizes []int, roles []Role, pool []Candidate, prefs [][]string) []map[string]int {
	demand := make(map[string]int, len(roles))
	for _, p := range prefs {
		if len(p) > 0 {
			demand[p[0]]++
		}
	}
	given := make(map[string]int, len(roles))
	out := make([]map[string]int, len(sizes))
	for t, size := range sizes {
		slots := make(map[string]int, len(roles))
		used := 0
		for _, r := range roles {
			slots[r.Name] = r.Min
			given[r.Name] += r.Min
			used += r.Min
		}
		for ; used < size; used++ {
			best := -1
			bestUnmet := 0
			for i, r := range roles {
				if slots[r.Name] >= r.Max {
					continue
				}
				// roles отсортированы по SortOrder: при равенстве побеждает первая
				if unmet := demand[r.Name] - given[r.Name]; best < 0 || unmet > bestUnmet {
					best, bestUnmet = i, unmet
				}
			}
			name := roles[best].Name
			slots[name]++
			given[name]++
		}
		out[t] = slots
	}
	return out
}

// assignRoles раздаёт роли в пределах общей ёмкости total. Каждый кандидат
// получает ровно одну роль, потому что сумма ёмкостей равна числу кандидатов.
func assignRoles(roles []Role, order []int, prefs [][]string, total map[string]int) []string {
	capLeft := maps.Clone(total)
	roleOf := make([]string, len(order))

	maxLen := 0
	for _, p := range prefs {
		maxLen = max(maxLen, len(p))
	}
	for round := 0; round < maxLen; round++ {
		for _, i := range order {
			if roleOf[i] != "" || round >= len(prefs[i]) {
				continue
			}
			if r := prefs[i][round]; capLeft[r] > 0 {
				roleOf[i] = r
				capLeft[r]--
			}
		}
	}
	for _, i := range order {
		if roleOf[i] != "" {
			continue
		}
		for _, r := range roles {
			if capLeft[r.Name] > 0 {
				roleOf[i] = r.Name
				capLeft[r.Name]--
				break
			}
		}
	}
	return roleOf
}

// placeInTeams расставляет кандидатов с уже известными ролями по командам.
func placeInTeams(sizes []int, roles []Role, pool []Candidate, order []int, roleOf []string, slots []map[string]int) []Team {
	rank := make([]int, len(pool)) // позиция в перемешанном порядке: tie-break по опыту
	for pos, i := range order {
		rank[i] = pos
	}
	free := make([]map[string]int, len(slots))
	for t := range slots {
		free[t] = maps.Clone(slots[t])
	}
	sum := make([]int, len(sizes))
	teams := make([]Team, len(sizes))
	for t := range teams {
		teams[t].Name = fmt.Sprintf("Команда %d", t+1)
	}

	for _, role := range roles {
		var holders []int
		for i := range pool {
			if roleOf[i] == role.Name {
				holders = append(holders, i)
			}
		}
		slices.SortFunc(holders, func(a, b int) int {
			if c := cmp.Compare(pool[b].Experience, pool[a].Experience); c != 0 {
				return c
			}
			return cmp.Compare(rank[a], rank[b])
		})
		for _, i := range holders {
			best := -1
			for t := range teams {
				if free[t][role.Name] > 0 && (best < 0 || sum[t] < sum[best]) {
					best = t
				}
			}
			free[best][role.Name]--
			sum[best] += pool[i].Experience
			teams[best].Members = append(teams[best].Members, Assignment{MemberID: pool[i].MemberID, Role: role.Name})
		}
	}

	roleIdx := make(map[string]int, len(roles))
	for i, r := range roles {
		roleIdx[r.Name] = i
	}
	for t := range teams {
		slices.SortFunc(teams[t].Members, func(a, b Assignment) int {
			if c := cmp.Compare(roleIdx[a.Role], roleIdx[b.Role]); c != 0 {
				return c
			}
			return cmp.Compare(a.MemberID, b.MemberID)
		})
	}
	return teams
}

// checkRules защищает алгоритм от правил, которые split не должен был пропустить.
// Это ошибка программы, а не клиента, поэтому не InfeasibleError.
func checkRules(rules Rules) error {
	if rules.TeamSize < 1 || len(rules.Roles) == 0 {
		return fmt.Errorf("invalid rules: teamSize=%d roles=%d", rules.TeamSize, len(rules.Roles))
	}
	smin, smax := 0, 0
	for _, r := range rules.Roles {
		if r.Min < 0 || r.Min > r.Max {
			return fmt.Errorf("invalid rules: role %q min=%d max=%d", r.Name, r.Min, r.Max)
		}
		smin += r.Min
		smax += r.Max
	}
	if smin > rules.TeamSize || rules.TeamSize > smax {
		return fmt.Errorf("invalid rules: sum(min)=%d teamSize=%d sum(max)=%d", smin, rules.TeamSize, smax)
	}
	return nil
}
