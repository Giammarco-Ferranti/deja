package main

import (
	"fmt"
	"os"

	"github.com/giammarcoferranti/deja/internal/store"
)

func main() {
	fmt.Println("starting deja...")
	db, err := store.InitDB("deja.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "init db failed: %v\n", err)
		os.Exit(1)
	}
	_ = db
	fmt.Println("db ready")
}
