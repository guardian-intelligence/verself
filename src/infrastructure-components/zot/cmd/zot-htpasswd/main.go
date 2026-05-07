package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	username := flag.String("username", "", "htpasswd username")
	passwordFile := flag.String("password-file", "", "path containing the plaintext password")
	flag.Parse()
	if err := run(*username, *passwordFile); err != nil {
		fmt.Fprintf(os.Stderr, "zot-htpasswd: %v\n", err)
		os.Exit(1)
	}
}

func run(username, passwordFile string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("--username is required")
	}
	if strings.ContainsAny(username, ":\n\r") {
		return errors.New("--username cannot contain ':', CR, or LF")
	}
	if passwordFile == "" {
		return errors.New("--password-file is required")
	}
	raw, err := os.ReadFile(passwordFile)
	if err != nil {
		return fmt.Errorf("read password file: %w", err)
	}
	password := []byte(strings.TrimSpace(string(raw)))
	if len(password) == 0 {
		return errors.New("password file is empty")
	}
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("generate bcrypt hash: %w", err)
	}
	fmt.Printf("%s:%s\n", username, hash)
	return nil
}
