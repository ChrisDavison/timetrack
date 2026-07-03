package main

import (
	"github.com/davison/timetrack/internal/web"
)

func runServe(args []string) error {
	fs, db := newFlagSet("serve")
	addr := fs.String("addr", ":8090", "listen address")
	capacity := fs.Float64("capacity", 8, "hours per day considered fully committed")
	fs.Parse(args)

	s, err := openStore(*db)
	if err != nil {
		return err
	}
	defer s.Close()
	return web.Serve(s, *addr, *capacity)
}
