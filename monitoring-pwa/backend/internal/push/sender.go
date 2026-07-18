package push

import (
	"io"
	"log/slog"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Sender は VAPID キーペアを使用して、登録された全デバイスに Web Push 通知をブロードキャストする構造体である。
type Sender struct {
	publicKey  string
	privateKey string
	subject    string
	db         *DB
}

func NewSender(publicKey, privateKey, subject string, db *DB) *Sender {
	return &Sender{
		publicKey:  publicKey,
		privateKey: privateKey,
		subject:    subject,
		db:         db,
	}
}

// Broadcast は登録されたすべてのデバイスに通知を送信する。
// 送信先が 404/410 (無効化/期限切れ) を返した場合、データベースから自動削除する。
func (s *Sender) Broadcast(payload []byte) {
	for _, sub := range s.db.List() {
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
			s.db.Remove(sub.Endpoint)
		case resp.StatusCode >= 300:
			slog.Error("push service rejected notification", "endpoint", sub.Endpoint, "status", resp.StatusCode, "body", string(body))
		default:
			slog.Info("push sent", "endpoint", sub.Endpoint, "status", resp.StatusCode)
		}
	}
}
