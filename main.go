package main

import (
	"os"

	"github.com/desktopgame/backup/internal/app"
)

func main() {
	os.Exit(app.New().Main(os.Args[1:]))
}
