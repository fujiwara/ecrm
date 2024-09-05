package ecrm

type set map[string]struct{}

func newSet() set {
	return make(map[string]struct{})
}

func (s set) add(v string) bool {
	if _, ok := s[v]; ok {
		return false // already exists
	}
	s[v] = struct{}{}
	return true // added
}

func (s set) contains(v string) bool {
	_, ok := s[v]
	return ok
}

func (s set) remove(v string) {
	delete(s, v)
}

func (s set) isEmpty() bool {
	if s == nil {
		return true
	}
	return len(s) == 0
}

func (s set) members() []string {
	var members []string
	for k := range s {
		members = append(members, k)
	}
	return members
}

func (s set) union(o set) set {
	if o == nil {
		return s
	}
	u := newSet()
	for k := range s {
		u.add(k)
	}
	for k := range o {
		u.add(k)
	}
	return u
}
