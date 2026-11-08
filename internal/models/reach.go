package models

// Reached is where an asset counts (docs/stock-and-lending.md#pools): the
// categories it is in, the categories above them, and those its categories
// imply, with the categories above those, and so on through chains of
// links.
type Reached struct {
	// Counted holds every category it counts in, for totals, alerts and
	// reports.
	Counted map[string]bool
	// Linked maps each category a link names, among those it counts in
	// only through links, to the category whose link it comes through
	// first: one the asset is in, or one above it. These are the
	// categories it is a member of as if it were in them.
	Linked map[string]string
}

// Reach works out where an asset in the direct categories counts, given
// each category's parent and the categories it implies. A category reached
// through several paths counts once, and one reached without a link is not
// in Linked. It stops at a cycle, which links never make but a tree moved
// under them might.
func Reach(direct []string, parent func(string) string, implies func(string) []string) Reached {
	r := Reached{Counted: map[string]bool{}, Linked: map[string]string{}}
	type step struct{ id, through string }
	var queue []step
	// climb counts a category and those above it, each reached through
	// through, and queues them to follow their links.
	climb := func(id, through string) {
		for depth := 0; id != "" && depth < 100 && !r.Counted[id]; depth++ {
			r.Counted[id] = true
			queue = append(queue, step{id, through})
			id = parent(id)
		}
	}
	// Every category reached without a link first, so none of them is
	// taken for one reached through a link.
	for _, id := range direct {
		climb(id, "")
	}
	tree := make(map[string]bool, len(r.Counted))
	for id := range r.Counted {
		tree[id] = true
	}
	for i := 0; i < len(queue); i++ {
		s := queue[i]
		through := s.through
		if through == "" {
			through = s.id
		}
		for _, y := range implies(s.id) {
			if r.Counted[y] {
				// Above a category a link named, and named by one too.
				if _, ok := r.Linked[y]; !ok && !tree[y] {
					r.Linked[y] = through
				}
				continue
			}
			r.Linked[y] = through
			climb(y, through)
		}
	}
	return r
}
