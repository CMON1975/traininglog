package plan

type Item struct {
	Kind    string // "check" or "sets" or "heading"
	Label   string
	Sets    int // number of inputs to show for "sets"; ignored for "check" and "heading"
	RepsMin int
	RepsMax int
	Note    string // optional suffix like "(20-60 secs)"
}

func Day(n int) []Item {
	switch n {
	case 1:
		return strengthA("green face pulls", true, false)
	case 3:
		return strengthB("band curls", true)
	case 5:
		return strengthA("purple dips", true, false)
	case 7:
		return strengthB("green face pulls", false)
	case 9:
		return strengthA("band curls", false, true)
	case 11:
		return strengthB("purple dips", false)
	case 2, 4, 6, 8, 10, 12:
		return easyDay()
	default:
		return easyDay()
	}
}

func commonStarts() []Item {
	return []Item{
		{Kind: "check", Label: "foam roll"},
		{Kind: "check", Label: "walk 55 mins 1%, 5 mins 0%"},
		{Kind: "heading", Label: "3-4 circuits"},
	}
}

func easyDay() []Item {
	return []Item{
		{Kind: "check", Label: "foam roll"},
		{Kind: "check", Label: "walk 55 mins 1%, 5 mins 0%"},
		{Kind: "check", Label: "stretch"},
		{Kind: "check", Label: "breathe"},
	}
}

func strengthA(finisher string, includePlank bool, includeKneeRaises bool) []Item {
	items := append([]Item{}, commonStarts()...)
	items = append(items,
		Item{Kind: "sets", Label: "inc 2 pushups", Sets: 4, RepsMin: 8, RepsMax: 15},
		Item{Kind: "sets", Label: "green rows", Sets: 4, RepsMin: 8, RepsMax: 12},
		Item{Kind: "sets", Label: "bw squats", Sets: 4, RepsMin: 10, RepsMax: 15},
	)
	if includePlank {
		items = append(items, Item{Kind: "sets", Label: "plank", Sets: 4, RepsMin: 20, RepsMax: 60, Note: "secs"})
	}
	if includeKneeRaises {
		items = append(items, Item{Kind: "sets", Label: "knee raises", Sets: 4, RepsMin: 6, RepsMax: 12})
	}
	items = append(items,
		Item{Kind: "sets", Label: finisher, Sets: 1, RepsMin: 12, RepsMax: 20, Note: "finisher"},
		Item{Kind: "check", Label: "stretch"},
		Item{Kind: "check", Label: "breathe"},
	)
	return items
}

// TODO: GPT analyzed my existing routine and decided to hardcode these baseline items instead
// of making them mutable; this will have to be fixed when extending the code.
// strengthB: pushups + band pullups + split squats + optional knee raises + finisher
func strengthB(finisher string, includeKneeRaises bool) []Item {
	items := append([]Item{}, commonStarts()...)
	items = append(items,
		Item{Kind: "sets", Label: "inc 2 pushups", Sets: 4, RepsMin: 8, RepsMax: 15},
		Item{Kind: "sets", Label: "purp/red band pullups", Sets: 4, RepsMin: 6, RepsMax: 10},
		Item{Kind: "sets", Label: "bw split squats", Sets: 4, RepsMin: 10, RepsMax: 15},
	)
	if includeKneeRaises {
		items = append(items, Item{Kind: "sets", Label: "knee raises", Sets: 4, RepsMin: 6, RepsMax: 12})
	}
	items = append(items,
		Item{Kind: "sets", Label: finisher, Sets: 1, RepsMin: 12, RepsMax: 20, Note: "finisher"},
		Item{Kind: "check", Label: "stretch"},
		Item{Kind: "check", Label: "breathe"},
	)
	return items
}
