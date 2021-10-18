package ecrm

type set map[string]struct{}

func newSet() set {
	return make(map[string]struct{})
}

func (s set) add(v string) {
	s[v] = struct{}{}
}

func (s set) remove(v string) {
	delete(s, v)
}

func (s set) isEmpty() bool {
	return len(s) == 0
}

func (s set) members() []string {
	var members []string
	for k := range s {
		members = append(members, k)
	}
	return members
}
