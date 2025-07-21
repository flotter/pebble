package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	prefix := flag.String("prefix", "msg", "Prefix for each timestamp line")
	tickrate := flag.Int("t", 100, "Message frequence in ms.")
	flag.Parse()

	// Set up SIGINT handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT)

	ticker := time.NewTicker(time.Duration(*tickrate) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Printf("%s: %s\n", *prefix, time.Now().Format(time.RFC3339Nano))
		case <-sigChan:
			os.Exit(0)
		}
	}
}

