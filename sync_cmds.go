package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/davison/timetrack/internal/store"
)

func runMerge(args []string) error {
	fs, db := newFlagSet("merge")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: tt merge <other.db>")
	}
	otherPath := fs.Arg(0)
	if _, err := os.Stat(otherPath); err != nil {
		return fmt.Errorf("cannot read %s: %w", otherPath, err)
	}
	other, err := store.Open(otherPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", otherPath, err)
	}
	defer other.Close()
	snap, err := other.ExportSnapshot()
	if err != nil {
		return err
	}

	local, err := openStore(*db)
	if err != nil {
		return err
	}
	defer local.Close()
	stats, err := local.MergeSnapshot(snap)
	if err != nil {
		return err
	}
	fmt.Printf("merged %s: %s\n", otherPath, stats)
	return nil
}

func runExport(args []string) error {
	fs, db := newFlagSet("export")
	out := fs.String("o", "", "write to file instead of stdout")
	fs.Parse(args)

	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	snap, err := s.ExportSnapshot()
	if err != nil {
		return err
	}
	w := io.Writer(os.Stdout)
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}

func runImport(args []string) error {
	fs, db := newFlagSet("import")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: tt import <file.json | ->")
	}
	r := io.Reader(os.Stdin)
	if name := fs.Arg(0); name != "-" {
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	var snap store.Snapshot
	if err := json.NewDecoder(r).Decode(&snap); err != nil {
		return fmt.Errorf("parse snapshot: %w", err)
	}

	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	stats, err := s.MergeSnapshot(snap)
	if err != nil {
		return err
	}
	fmt.Printf("imported: %s\n", stats)
	return nil
}
