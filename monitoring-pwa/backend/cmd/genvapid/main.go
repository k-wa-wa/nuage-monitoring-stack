// genvapid は monitoring-pwa デプロイメント用の新規 VAPID キーペアを出力する。
package main

import (
	"fmt"
	"log"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

func main() {
	priv, pub, err := push.GenerateVAPIDKeys()
	if err != nil {
		log.Fatalf("failed to generate VAPID keys: %v", err)
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\n", pub)
	fmt.Printf("VAPID_PRIVATE_KEY=%s\n", priv)
}
