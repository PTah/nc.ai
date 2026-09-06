package main

import (
	"fmt"
	"os"
	"path/filepath"

	"notcursor.ai/app/internal/chatstore"
)

func main() {
	appData := filepath.Join(os.Getenv("APPDATA"), "NotCursor")
	s, err := chatstore.New(appData)
	if err != nil {
		panic(err)
	}
	proj := `E:\Soft\Git\nc.ai`
	b, err := s.List(proj)
	if err != nil {
		panic(err)
	}
	sess, err := s.Get(proj, b.ActiveID)
	if err != nil {
		panic(err)
	}
	fmt.Printf("sessions=%d active=%s itemsLen=%d hist=%d\n", len(b.Sessions), b.ActiveID[:8], len(sess.ItemsJSON), len(sess.History))
}
