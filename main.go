package main

import "log"

func main() {
	srv := NewServer(":6380")
	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
	select{} // block forever (add graceful shutdown later)
}