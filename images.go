package ecrm

import "strings"

// ImageID is a type of ID of an ECR image.
type ImageID string

func (id ImageID) String() string {
	return string(id)
}

func (id ImageID) Short() string {
	return strings.SplitN(string(id), "/", 2)[1]
}

type Images map[ImageID]set

func (i Images) Add(id ImageID, usedBy string) bool {
	if _, ok := i[id]; !ok {
		i[id] = newSet()
	}
	return i[id].add(usedBy)
}

func (i Images) Contains(id ImageID) bool {
	return !i[id].isEmpty()
}

func (i Images) Merge(j Images) {
	for k, v := range j {
		i[k] = i[k].union(v)
	}
}
