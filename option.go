package ecrm

import (
	"fmt"
	"io"
	"os"
)

type Option struct {
	ScanOnly     bool
	Scan         bool
	Delete       bool
	Force        bool
	Repository   RepositoryName
	OutputFile   string
	Format       outputFormat
	ScannedFiles []string
}

func (opt *Option) Validate() error {
	if len(opt.ScannedFiles) == 0 && !opt.Scan {
		return fmt.Errorf("no --scanned-files and --no-scan provided. specify at least one")
	}
	return nil
}

// NopCloserWriter is a writer that does nothing on Close
type NopCloserWriter struct {
	io.Writer
}

func (NopCloserWriter) Close() error { return nil }

func (opt *Option) OutputWriter() (io.WriteCloser, error) {
	if opt.OutputFile == "" || opt.OutputFile == "-" {
		return NopCloserWriter{os.Stdout}, nil
	}
	return os.Create(opt.OutputFile)
}
