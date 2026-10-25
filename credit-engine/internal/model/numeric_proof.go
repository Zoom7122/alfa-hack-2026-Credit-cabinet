package model

import (
	"math"
	"math/big"
	"sort"
)

type numberInfo struct {
	kind                           NumericKind
	integer                        bool
	lower, upper                   *big.Rat
	lowerInclusive, upperInclusive bool
}

func (v *validator) numericKind(p Parameter) NumericKind {
	if p.Source.Kind == Supplier && p.Number != nil && p.Number.Integer {
		return NumericInt64
	}
	if binding, ok := v.bindings[p.Source]; ok && binding.NumericKind != "" {
		return binding.NumericKind
	}
	return NumericFloat64
}

func configRat(n NumberLiteral, kind NumericKind) *big.Rat {
	if kind == NumericFloat64 {
		return new(big.Rat).SetFloat64(numberFloat(n))
	}
	x, _ := numberRat(n)
	return x
}

func numberInfoFor(p Parameter, kind NumericKind) numberInfo {
	maximum := new(big.Rat).SetFloat64(math.MaxFloat64)
	info := numberInfo{kind: kind, integer: p.Number.Integer, lower: new(big.Rat).Neg(maximum), upper: new(big.Rat).Set(maximum), lowerInclusive: true, upperInclusive: true}
	if kind == NumericInt64 {
		info.lower = new(big.Rat).SetInt(big.NewInt(-1 << 63))
		info.upper = new(big.Rat).SetInt(big.NewInt(1<<63 - 1))
	}
	if kind == NumericInt64 || kind == NumericInteger {
		info.integer = true
	}
	if bound := p.Number.Lower; bound != nil {
		x := configRat(bound.Value, kind)
		cmp := x.Cmp(info.lower)
		if cmp > 0 {
			info.lower = x
			info.lowerInclusive = bound.Inclusive
		} else if cmp == 0 {
			info.lowerInclusive = info.lowerInclusive && bound.Inclusive
		}
	}
	if bound := p.Number.Upper; bound != nil {
		x := configRat(bound.Value, kind)
		cmp := x.Cmp(info.upper)
		if cmp < 0 {
			info.upper = x
			info.upperInclusive = bound.Inclusive
		} else if cmp == 0 {
			info.upperInclusive = info.upperInclusive && bound.Inclusive
		}
	}
	return info
}

func (n numberInfo) contains(x *big.Rat) bool {
	if x == nil || n.integer && !x.IsInt() {
		return false
	}
	lo, hi := x.Cmp(n.lower), x.Cmp(n.upper)
	if lo < 0 || lo == 0 && !n.lowerInclusive || hi > 0 || hi == 0 && !n.upperInclusive {
		return false
	}
	if n.kind == NumericFloat64 {
		f, exact := x.Float64()
		return exact && finite(f)
	}
	return true
}

func ceilRat(x *big.Rat) *big.Int {
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(x.Num(), x.Denom(), remainder)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

func leastDiscrete(lower *big.Rat, inclusive bool, floating, integer bool) *big.Rat {
	if floating {
		f, _ := lower.Float64()
		if math.IsInf(f, -1) {
			f = -math.MaxFloat64
		}
		if !finite(f) {
			return nil
		}
		x := new(big.Rat).SetFloat64(f)
		if x.Cmp(lower) < 0 || x.Cmp(lower) == 0 && !inclusive {
			f = math.Nextafter(f, math.Inf(1))
		}
		if integer && finite(f) && math.Trunc(f) != f {
			f = math.Ceil(f)
		}
		if !finite(f) {
			return nil
		}
		return new(big.Rat).SetFloat64(f)
	}
	x := ceilRat(lower)
	if new(big.Rat).SetInt(x).Cmp(lower) == 0 && !inclusive {
		x.Add(x, big.NewInt(1))
	}
	return new(big.Rat).SetInt(x)
}

func (v *validator) numberDomainNonempty(p Parameter) bool {
	info := numberInfoFor(p, v.numericKind(p))
	cmp := info.lower.Cmp(info.upper)
	if cmp > 0 || cmp == 0 && (!info.lowerInclusive || !info.upperInclusive) {
		return false
	}
	if info.kind == NumericRational && !info.integer {
		if cmp == 0 {
			return info.contains(info.lower)
		}
		return true
	}
	x := leastDiscrete(info.lower, info.lowerInclusive, info.kind == NumericFloat64, info.integer)
	return info.contains(x)
}

func ratCopy(x *big.Rat) *big.Rat { return new(big.Rat).Set(x) }

func sortedRats(values []*big.Rat) []*big.Rat {
	sort.Slice(values, func(i, j int) bool { return values[i].Cmp(values[j]) < 0 })
	result := values[:0]
	for _, x := range values {
		if len(result) == 0 || result[len(result)-1].Cmp(x) != 0 {
			result = append(result, x)
		}
	}
	return result
}

type numberCell struct {
	lower, upper *big.Rat
	point        bool
}

// orderedWitness constructs one assignment for a weak ordering of variables
// in an open cell. Discrete groups are placed at the least available value in
// order. Rational-only groups fit densely between those discrete values.
func orderedWitness(groups [][]string, cell numberCell, infos map[string]numberInfo) (map[string]*big.Rat, bool) {
	values := make([]*big.Rat, len(groups))
	previous := cell.lower
	for i, group := range groups {
		floating, integer := false, false
		for _, id := range group {
			info := infos[id]
			floating = floating || info.kind == NumericFloat64
			integer = integer || info.integer
		}
		if !floating && !integer {
			continue
		}
		x := leastDiscrete(previous, false, floating, integer)
		if x == nil || x.Cmp(cell.upper) >= 0 {
			return nil, false
		}
		for _, id := range group {
			if !infos[id].contains(x) {
				return nil, false
			}
		}
		values[i] = x
		previous = x
	}
	for start := 0; start < len(groups); {
		if values[start] != nil {
			start++
			continue
		}
		end := start
		for end < len(groups) && values[end] == nil {
			end++
		}
		lower, upper := cell.lower, cell.upper
		if start > 0 {
			lower = values[start-1]
		}
		if end < len(groups) {
			upper = values[end]
		}
		step := new(big.Rat).Sub(upper, lower)
		step.Quo(step, new(big.Rat).SetInt64(int64(end-start+1)))
		if step.Sign() <= 0 {
			return nil, false
		}
		for i := start; i < end; i++ {
			x := new(big.Rat).Mul(step, new(big.Rat).SetInt64(int64(i-start+1)))
			x.Add(x, lower)
			for _, id := range groups[i] {
				if !infos[id].contains(x) {
					return nil, false
				}
			}
			values[i] = x
		}
		start = end
	}
	result := map[string]*big.Rat{}
	for i, group := range groups {
		for _, id := range group {
			result[id] = values[i]
		}
	}
	return result, true
}

// numericCandidates enumerates exact predicate cells and weak orderings, then
// collects their witnesses. The existing Cartesian proof exhausts these values;
// it contains every witness and only additional legal domain assignments.
func (v *validator) numericCandidates(ids []string, conditions []Condition) (map[string][]proofValue, bool) {
	infos := map[string]numberInfo{}
	cuts := []*big.Rat{}
	for _, id := range ids {
		info := numberInfoFor(v.parameters[id], v.numericKind(v.parameters[id]))
		infos[id] = info
		cuts = append(cuts, ratCopy(info.lower), ratCopy(info.upper))
	}
	for _, condition := range conditions {
		info, exists := infos[condition.Parameter]
		if !exists {
			continue
		}
		if condition.Value != nil && condition.Value.Number != nil {
			cuts = append(cuts, configRat(*condition.Value.Number, info.kind))
		}
		for _, literal := range condition.Values {
			if literal.Number != nil {
				cuts = append(cuts, configRat(*literal.Number, info.kind))
			}
		}
	}
	cuts = sortedRats(cuts)
	cells := []numberCell{}
	for i, x := range cuts {
		cells = append(cells, numberCell{lower: x, upper: x, point: true})
		if i+1 < len(cuts) {
			cells = append(cells, numberCell{lower: x, upper: cuts[i+1]})
		}
	}
	sets := map[string]map[string]*big.Rat{}
	for _, id := range ids {
		sets[id] = map[string]*big.Rat{}
	}
	failed := false
	spend := func() bool {
		v.coverageStates++
		if v.coverageStates > MaxCoverageStates {
			failed = true
			return false
		}
		return true
	}
	add := func(witness map[string]*big.Rat) {
		for id, x := range witness {
			sets[id][x.RatString()] = ratCopy(x)
		}
	}
	assignments := map[string]int{}
	var assign func(int)
	assign = func(index int) {
		if failed || !spend() {
			return
		}
		if index < len(ids) {
			id := ids[index]
			info := infos[id]
			for i, cell := range cells {
				if cell.point {
					if !info.contains(cell.lower) {
						continue
					}
				} else {
					if info.lower.Cmp(cell.upper) >= 0 || info.upper.Cmp(cell.lower) <= 0 {
						continue
					}
					if _, ok := orderedWitness([][]string{{id}}, cell, infos); !ok {
						continue
					}
				}
				assignments[id] = i
				assign(index + 1)
				if failed {
					break
				}
			}
			delete(assignments, id)
			return
		}
		occupied := map[int][]string{}
		for _, id := range ids {
			occupied[assignments[id]] = append(occupied[assignments[id]], id)
		}
		indexes := make([]int, 0, len(occupied))
		for i := range occupied {
			indexes = append(indexes, i)
		}
		sort.Ints(indexes)
		witness := map[string]*big.Rat{}
		var fill func(int)
		fill = func(position int) {
			if failed || !spend() {
				return
			}
			if position == len(indexes) {
				add(witness)
				return
			}
			cellIndex := indexes[position]
			cell := cells[cellIndex]
			members := occupied[cellIndex]
			if cell.point {
				for _, id := range members {
					witness[id] = cell.lower
				}
				fill(position + 1)
				return
			}
			var order func(int, [][]string)
			order = func(next int, groups [][]string) {
				if failed || !spend() {
					return
				}
				if next == len(members) {
					values, ok := orderedWitness(groups, cell, infos)
					if !ok {
						return
					}
					for id, x := range values {
						witness[id] = x
					}
					fill(position + 1)
					return
				}
				id := members[next]
				for groupIndex := range groups {
					copyGroups := make([][]string, len(groups))
					for i, g := range groups {
						copyGroups[i] = append([]string{}, g...)
					}
					copyGroups[groupIndex] = append(copyGroups[groupIndex], id)
					order(next+1, copyGroups)
				}
				for insert := 0; insert <= len(groups); insert++ {
					copyGroups := make([][]string, 0, len(groups)+1)
					copyGroups = append(copyGroups, groups[:insert]...)
					copyGroups = append(copyGroups, []string{id})
					copyGroups = append(copyGroups, groups[insert:]...)
					order(next+1, copyGroups)
				}
			}
			order(0, nil)
		}
		fill(0)
	}
	assign(0)
	if failed {
		return nil, false
	}
	result := map[string][]proofValue{}
	for _, id := range ids {
		values := make([]*big.Rat, 0, len(sets[id]))
		for _, x := range sets[id] {
			values = append(values, x)
		}
		values = sortedRats(values)
		for _, x := range values {
			approximate, _ := x.Float64()
			value := proofValue{known: true, typ: Number, number: approximate}
			if infos[id].kind != NumericFloat64 {
				value.exact = ratCopy(x)
			}
			result[id] = append(result[id], value)
		}
	}
	return result, true
}
