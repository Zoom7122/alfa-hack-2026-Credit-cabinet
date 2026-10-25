package scoring

// Thresholds and weights are fixed in Go for model v2, not loaded from data.
func increasing(x, a, b, c, d, max float64) float64 {
	switch {
	case x < a:
		return 0
	case x < b:
		return max / 4
	case x < c:
		return max / 2
	case x < d:
		return max * 3 / 4
	default:
		return max
	}
}
func decreasing(x, a, b, c, d, max float64) float64 {
	switch {
	case x <= a:
		return max
	case x <= b:
		return max * 3 / 4
	case x <= c:
		return max / 2
	case x <= d:
		return max / 4
	default:
		return 0
	}
}
func margin(x, a, b, c, max float64) float64 {
	switch {
	case x <= 0:
		return 0
	case x < a:
		return max / 4
	case x < b:
		return max / 2
	case x < c:
		return max * 3 / 4
	default:
		return max
	}
}
func economicPoints(id string, x float64) (points float64, max int) {
	switch id {
	case "IP_E01":
		return margin(x, .05, .1, .15, 25), 25
	case "IP_E02":
		return decreasing(x, .15, .3, .5, .75, 30), 30
	case "IP_E03":
		return decreasing(x, .5, 1, 2, 3, 25), 25
	case "IP_E04":
		return increasing(x, .25, .5, 1, 2, 25), 25
	case "IP_E05":
		return increasing(x, 6, 8, 10, 12, 20), 20
	case "IP_E06":
		return decreasing(x, .1, .2, .35, .5, 15), 15
	case "IP_E07":
		return increasing(x, -.2, -.05, .05, .15, 15), 15
	case "IP_E08":
		return increasing(x, -.15, -.05, .05, .15, 10), 10
	case "IP_E09":
		return decreasing(x, .15, .3, .5, .7, 15), 15
	case "IP_E10":
		return increasing(x, .8, .9, .95, .99, 20), 20
	case "IP_E11":
		return decreasing(x, .01, .03, .05, .1, 15), 15
	case "IP_E12":
		return increasing(x, .05, .1, .2, .3, 15), 15
	case "IP_E13":
		return margin(x, .1, .2, .3, 20), 20
	case "LE_E01":
		return increasing(x, .7, 1, 1.3, 1.8, 20), 20
	case "LE_E02":
		return increasing(x, .3, .5, .8, 1, 15), 15
	case "LE_E03":
		return increasing(x, .05, .1, .2, .3, 10), 10
	case "LE_E04":
		return margin(x, .1, .25, .4, 25), 25
	case "LE_E05":
		return increasing(x, .4, .5, .6, .8, 15), 15
	case "LE_E06":
		return increasing(x, -.2, 0, .1, .2, 15), 15
	case "LE_E07":
		return decreasing(x, 1, 2, 3, 4, 20), 20
	case "LE_E08":
		return increasing(x, 1, 1.5, 2.5, 4, 20), 20
	case "LE_E09":
		return decreasing(x, .2, .4, .6, .8, 10), 10
	case "LE_E10":
		return margin(x, .1, .2, .3, 10), 10
	case "LE_E11":
		return margin(x, .05, .1, .2, 15), 15
	case "LE_E12":
		return margin(x, .02, .05, .1, 10), 10
	case "LE_E13":
		return margin(x, .02, .05, .1, 15), 15
	case "LE_E14":
		return margin(x, .05, .1, .2, 10), 10
	case "LE_E15":
		return increasing(x, -.2, -.05, .05, .15, 10), 10
	case "LE_E16":
		return increasing(x, .5, 1, 1.5, 2, 10), 10
	case "LE_E17":
		return decreasing(x, 30, 60, 90, 120, 10), 10
	case "LE_E18":
		return decreasing(x, 30, 60, 90, 180, 10), 10
	default:
		panic("unknown economic criterion: " + id)
	}
}
