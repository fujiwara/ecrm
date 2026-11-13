package ecrm_test

import (
	"bytes"
	"flag"
	"os"
	"testing"

	"github.com/fatih/color"
	"github.com/fujiwara/ecrm"
	"github.com/google/go-cmp/cmp"
)

var updateGolden = flag.Bool("update", false, "update golden files")

var testSummaryTable = ecrm.SummaryTable{
	{Repo: "app/web", Type: ecrm.SummaryTypeImage, TotalImages: 120, ExpiredImages: 100, TotalImageSize: 64_000_000_000, ExpiredImageSize: 52_000_000_000},
	{Repo: "app/web", Type: ecrm.SummaryTypeImageIndex, TotalImages: 12, ExpiredImages: 2, TotalImageSize: 24_000, ExpiredImageSize: 4_000},
	{Repo: "app/web", Type: ecrm.SummaryTypeSociIndex},
	{Repo: "batch", Type: ecrm.SummaryTypeImage, TotalImages: 3, TotalImageSize: 1_500_000_000},
	{Repo: "old/very-long-repository-name", Type: ecrm.SummaryTypeImage, TotalImages: 5, ExpiredImages: 5, TotalImageSize: 800_000_000, ExpiredImageSize: 800_000_000},
}

func TestSummaryTablePrintTable(t *testing.T) {
	tests := []struct {
		name    string
		noColor bool
		golden  string
	}{
		{name: "no color", noColor: true, golden: "testdata/summary_table.txt"},
		{name: "color", noColor: false, golden: "testdata/summary_table_color.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := color.NoColor
			color.NoColor = tt.noColor
			t.Cleanup(func() { color.NoColor = orig })

			b := new(bytes.Buffer)
			if err := testSummaryTable.Print(b, ecrm.FormatTable); err != nil {
				t.Fatal(err)
			}
			if *updateGolden {
				if err := os.WriteFile(tt.golden, b.Bytes(), 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(tt.golden)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(string(want), b.String()); diff != "" {
				t.Errorf("unexpected table output (-want +got):\n%s", diff)
			}
		})
	}
}
