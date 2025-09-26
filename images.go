package ecrm

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
)

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

func (i Images) Print(w io.Writer) error {
	m := make([]string, 0, len(i))
	for k := range i {
		m = append(m, string(k))
	}
	sort.Strings(m)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("failed to encode image uris: %w", err)
	}
	return nil
}

func (i Images) LoadFile(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()
	in := []string{}
	if err := json.NewDecoder(f).Decode(&in); err != nil {
		return fmt.Errorf("failed to decode images: %w", err)
	}
	for _, u := range in {
		log.Println("[debug] ImageUri", u, "src", filename)
		i[ImageURI(u)] = newSet(filename)
	}
	return nil
}

func (i Images) LoadExternalJSON(src string, b []byte) error {
	in := []string{}
	if err := json.Unmarshal(b, &in); err != nil {
		return fmt.Errorf("failed to decode images: %w", err)
	}
	for _, u := range in {
		log.Println("[debug] ImageUri", u, "src", src)
		i[ImageURI(u)] = newSet(src)
	}
	return nil
}

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
