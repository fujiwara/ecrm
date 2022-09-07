package ecrm

import (
	"bufio"
	"context"
	"log"
	"os"
	"strings"
)

func (app *App) readExcludeFiles(ctx context.Context, paths []string, images map[string]set) error {
	for _, path := range paths {
		log.Println("[info] reading exclude file", path)
		if err := readExcludeFile(ctx, path, images); err != nil {
			return err
		}
	}
	return nil
}

func readExcludeFile(ctx context.Context, path string, images map[string]set) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		img := scanner.Text()
		if strings.HasPrefix(img, "#") || img == "" {
			continue
		}
		if !strings.Contains(img, ".dkr.ecr.") {
			log.Println("[warn] skipping line", img)
			continue
		}
		if images[img] == nil {
			images[img] = newSet()
		}
		log.Printf("[info] %s is defined in %s", img, path)
		images[img].add(path)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
