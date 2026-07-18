package push

import (
	"io"
	"log/slog"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Sender broadcasts a payload to every subscription in the Store using
// the VAPID keypair configured for this deployment.
type Sender struct {
	publicKey  string
	privateKey string
	subject    string
	store      *Store
}

func NewSender(publicKey, privateKey, subject string, store *Store) *Sender {
	return &Sender{
		publicKey:  publicKey,
		privateKey: privateKey,
		subject:    subject,
		store:      store,
	}
}

// Broadcast sends payload to every stored subscription, pruning any that
// the push service reports as gone (404/410).
func (s *Sender) Broadcast(payload []byte) {
	for _, sub := range s.store.List() {
		resp, err := webpush.SendNotification(payload, &sub, &webpush.Options{
			Subscriber:      s.subject,
			VAPIDPublicKey:  s.publicKey,
			VAPIDPrivateKey: s.privateKey,
			TTL:             60,
			Urgency:         webpush.UrgencyHigh,
		})
		if err != nil {
			slog.Error("push send failed", "endpoint", sub.Endpoint, "error", err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			slog.Info("subscription expired, removing", "endpoint", sub.Endpoint)
			s.store.Remove(sub.Endpoint)
		case resp.StatusCode >= 300:
			slog.Error("push service rejected notification", "endpoint", sub.Endpoint, "status", resp.StatusCode, "body", string(body))
		default:
			slog.Info("push sent", "endpoint", sub.Endpoint, "status", resp.StatusCode)
		}
	}
}
