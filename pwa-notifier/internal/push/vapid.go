package push

import webpush "github.com/SherClockHolmes/webpush-go"

// GenerateVAPIDKeys creates a new VAPID keypair for signing web push
// requests. Run this once per deployment (see cmd/genvapid) and store the
// result as a Kubernetes secret; every server replica and every browser
// subscription must be created against the same keypair.
func GenerateVAPIDKeys() (publicKey, privateKey string, err error) {
	return webpush.GenerateVAPIDKeys()
}
