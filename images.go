package ecrm

import "strings"

// ImageURI represents an image URI.
type ImageURI string

func (u ImageURI) IsECRImage() bool {
	return strings.Contains(string(u), ".dkr.ecr.")
}

func (u ImageURI) IsDigestURI() bool {
	return strings.Contains(string(u), "@")
}

func (u ImageURI) Tag() string {
	if u.IsDigestURI() {
		return ""
	}
	s := strings.SplitN(string(u), ":", 2)
	if len(s) == 2 {
		return s[1]
	}
	return ""
}

func (u ImageURI) Base() string {
	if u.IsDigestURI() {
		s := strings.SplitN(string(u), "@", 2)
		return s[0]
	} else if strings.Contains(string(u), ":") {
		s := strings.SplitN(string(u), ":", 2)
		return s[0]
	} else {
		return string(u)
	}
}

func (u ImageURI) String() string {
	return string(u)
}

func (u ImageURI) Short() string {
	return strings.SplitN(string(u), "/", 2)[1]
}

type Images map[ImageURI]set

func (i Images) Add(u ImageURI, usedBy string) bool {
	if _, ok := i[u]; !ok {
		i[u] = newSet()
	}
	return i[u].add(usedBy)
}

func (i Images) Contains(u ImageURI) bool {
	return !i[u].isEmpty()
}

func (i Images) Merge(j Images) {
	for k, v := range j {
		i[k] = i[k].union(v)
	}
}
