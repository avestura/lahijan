package main

import (
	"log"

	"github.com/avestura/lahijan/internal/app/lahijan/program"
)

func main() {
	if err := program.Start(); err != nil {
		log.Fatalf("program exited: %s", err.Error())
	}
}
