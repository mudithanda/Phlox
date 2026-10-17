package main

import (
	"fmt"
	"strings"
)

// execute runs one parsed command and writes the reply to w.
// args[0] is the command name, args is never empty.

func execute(w *Writer, args []string) {
	switch strings.ToUpper(args[0]) {
	case "PING":
		switch len(args) {
		case 1:
			w.SimpleString("PONG")
		case 2:
			w.Bulk(args[1])
		default:
			wrongArgs(w, "ping")
		}
	case "ECHO":
		if len(args) != 2 {
			wrongArgs(w, "echo")
			return
		}
		w.Bulk(args[1])
	default:
		w.Error(fmt.Sprintf("ERR unknown command '%s'", args[0]))
	}
}

func wrongArgs(w *Writer, name string) {
	w.Error(fmt.Sprintf("ERR wrong number of arguments for '%s' command", name))
}