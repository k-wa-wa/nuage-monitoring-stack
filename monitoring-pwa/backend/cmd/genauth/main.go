package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	passphrase := flag.String("passphrase", "", "合言葉（必須）")
	userID := flag.String("user-id", "U01ADMIN", "ユーザー ID (U で始まる)")
	displayName := flag.String("display-name", "Admin", "表示名")
	flag.Parse()

	if *passphrase == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/genauth -passphrase <your-passphrase> [-user-id U01ADMIN] [-display-name Admin]")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(*passphrase), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bcrypt generation failed: %v\n", err)
		os.Exit(1)
	}

	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		fmt.Fprintf(os.Stderr, "random key generation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("COOKIE_SIGNING_KEY=%s\n", hex.EncodeToString(keyBytes))
	fmt.Printf("USERS_JSON=[{\"id\":%q,\"display_name\":%q,\"passphrase_hash\":%q}]\n", *userID, *displayName, string(hash))
}
