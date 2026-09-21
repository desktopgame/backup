package main

import (
	"os"

	"backup/internal/app"
)

func main() {
	os.Exit(app.New().Main(os.Args[1:]))
}
