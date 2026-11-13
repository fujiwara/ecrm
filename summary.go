package ecrm

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecrTypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/renderer"
	"github.com/olekukonko/tablewriter/tw"
)

const (
	SummaryTypeImage      = "Image"
	SummaryTypeImageIndex = "Image index"
	SummaryTypeSociIndex  = "Soci index"
)

type RepoSummary []*Summary

func NewRepoSummary(repo RepositoryName) RepoSummary {
	return []*Summary{
		{Repo: repo, Type: SummaryTypeImage},
		{Repo: repo, Type: SummaryTypeImageIndex},
		{Repo: repo, Type: SummaryTypeSociIndex},
	}
}

func (s RepoSummary) toIndex(img ecrTypes.ImageDetail) int {
	if isContainerImage(img) {
		return 0
	} else if isImageIndex(img) {
		return 1
	} else if isSociIndex(img) {
		return 2
	}
	log.Printf("[warn] unknown image type: artifact:%s manifest:%s digest:%s",
		aws.ToString(img.ArtifactMediaType),
		aws.ToString(img.ImageManifestMediaType),
		aws.ToString(img.ImageDigest),
	)
	return -1
}

func (s RepoSummary) Add(img ecrTypes.ImageDetail) {
	index := s.toIndex(img)
	if index >= 0 {
		s[index].TotalImages++
		s[index].TotalImageSize += aws.ToInt64(img.ImageSizeInBytes)
	}
}

func (s RepoSummary) Expire(img ecrTypes.ImageDetail) {
	index := s.toIndex(img)
	if index >= 0 {
		s[index].ExpiredImages++
		s[index].ExpiredImageSize += aws.ToInt64(img.ImageSizeInBytes)
	}
}

type Summary struct {
	Repo             RepositoryName `json:"repository"`
	Type             string         `json:"type"`
	ExpiredImages    int64          `json:"expired_images"`
	TotalImages      int64          `json:"total_images"`
	ExpiredImageSize int64          `json:"expired_image_size"`
	TotalImageSize   int64          `json:"total_image_size"`
}

func (s *Summary) printable() bool {
	if s.Type == SummaryTypeImageIndex || s.Type == SummaryTypeSociIndex {
		return s.TotalImages > 0
	}
	return true
}

func (s *Summary) row() []string {
	return []string{
		string(s.Repo),
		s.Type,
		fmt.Sprintf("%d (%s)", s.TotalImages, humanize.Bytes(uint64(s.TotalImageSize))),
		fmt.Sprintf("%d (%s)", -s.ExpiredImages, humanize.Bytes(uint64(s.ExpiredImageSize))),
		fmt.Sprintf("%d (%s)", s.TotalImages-s.ExpiredImages, humanize.Bytes(uint64(s.TotalImageSize-s.ExpiredImageSize))),
	}
}

func newOutputFormatFrom(s string) outputFormat {
	switch s {
	case "table":
		return formatTable
	case "json":
		return formatJSON
	default:
		panic(fmt.Sprintf("invalid format name: %s", s))
	}
}

type outputFormat int

func (f outputFormat) String() string {
	switch f {
	case formatTable:
		return "table"
	case formatJSON:
		return "json"
	default:
		return "unknown"
	}
}

const (
	formatTable outputFormat = iota + 1
	formatJSON
)

type SummaryTable []*Summary

func (s SummaryTable) Sort() {
	sort.SliceStable(s, func(i, j int) bool {
		return s[i].Repo < s[j].Repo
	})
}

func (s *SummaryTable) Print(w io.Writer, format outputFormat) error {
	switch format {
	case formatTable:
		return s.printTable(w)
	case formatJSON:
		return s.printJSON(w)
	default:
		return fmt.Errorf("unknown output format: %s", format)
	}
}

func (s SummaryTable) printJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// must not be nil to print an empty array instead of null
	ss := make([]*Summary, 0, len(s))
	for _, _s := range s {
		if _s.printable() {
			ss = append(ss, _s)
		}
	}
	return enc.Encode(ss)
}

func (s SummaryTable) printTable(w io.Writer) error {
	t := tablewriter.NewTable(w,
		tablewriter.WithRenderer(renderer.NewBlueprint(tw.Rendition{
			Borders: tw.BorderNone,
			Symbols: tw.NewSymbols(tw.StyleASCII),
			Settings: tw.Settings{
				Lines:      tw.Lines{ShowHeaderLine: tw.On},
				Separators: tw.Separators{BetweenColumns: tw.On},
			},
		})),
		tablewriter.WithHeaderAlignment(tw.AlignCenter),
		tablewriter.WithRowAlignment(tw.AlignLeft),
	)
	t.Header(s.header())
	blue := color.New(color.FgBlue).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	for _, s := range s {
		row := s.row()
		if !s.printable() {
			continue
		}
		if strings.HasPrefix(row[3], "0 ") {
			row[3] = ""
		} else {
			row[3] = blue(row[3])
		}
		if strings.HasPrefix(row[4], "0 ") {
			row[4] = yellow(row[4])
		}
		if err := t.Append(row); err != nil {
			return err
		}
	}
	return t.Render()
}

func (s SummaryTable) header() []string {
	return []string{
		"repository",
		"type",
		"total",
		"expired",
		"keep",
	}
}
