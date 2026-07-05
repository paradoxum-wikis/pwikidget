package main

import (
	"flag"
	"log"

	"pwikidget/internal"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()
	if err := internal.Run(*configPath); err != nil {
		log.Fatal(err)
	}
}
