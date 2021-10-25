package ecrm

import (
	"fmt"
	"io"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/olekukonko/tablewriter"
)

type summary struct {
	repo             string
	expiredImages    int64
	totalImages      int64
	expiredImageSize int64
	totalImageSize   int64
}

func (s *summary) row() []string {
	return []string{
		s.repo,
		fmt.Sprintf("%d (%s)", s.totalImages, humanize.Bytes(uint64(s.totalImageSize))),
		fmt.Sprintf("%d (%s)", -s.expiredImages, humanize.Bytes(uint64(s.expiredImageSize))),
		fmt.Sprintf("%d (%s)", s.totalImages-s.expiredImages, humanize.Bytes(uint64(s.totalImageSize-s.expiredImageSize))),
	}
}

type summaries []*summary

func (s *summaries) print(w io.Writer) {
	t := tablewriter.NewWriter(w)
	t.SetHeader(s.header())
	t.SetBorder(false)
	for _, s := range *s {
		row := s.row()
		colors := make([]tablewriter.Colors, len(row))
		if strings.HasPrefix(row[2], "0 ") {
			row[2] = ""
		} else {
			colors[2] = tablewriter.Colors{tablewriter.FgBlueColor}
		}
		if strings.HasPrefix(row[3], "0 ") {
			colors[3] = tablewriter.Colors{tablewriter.FgYellowColor}
		}
		t.Rich(row, colors)
	}
	t.Render()
}

func (s *summaries) header() []string {
	return []string{
		"repository",
		"total",
		"expired",
		"keep",
	}
}
