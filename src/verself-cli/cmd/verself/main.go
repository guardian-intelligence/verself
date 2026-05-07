package main

import (
	"os"

	"github.com/verself/verself-cli/internal/app"
)

func main() {
	os.Exit(app.MainWithBinary("verself"))
}
