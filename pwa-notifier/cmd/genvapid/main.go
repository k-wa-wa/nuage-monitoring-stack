// genvapid prints a fresh VAPID keypair for the pwa-notifier deployment.
//
// Usage: go run ./cmd/genvapid
package main

import (
	"fmt"
	"log"

	"github.com/k-wa-wa/nuage-monitoring-stack/pwa-notifier/internal/push"
)

func main() {
	pub, priv, err := push.GenerateVAPIDKeys()
	if err != nil {
		log.Fatalf("failed to generate VAPID keys: %v", err)
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\n", pub)
	fmt.Printf("VAPID_PRIVATE_KEY=%s\n", priv)
}
