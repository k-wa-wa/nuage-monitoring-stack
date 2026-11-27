package usecase

import (
	"context"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// SubscriptionUsecase は Web Push 購読の登録・削除・VAPID 公開鍵・テスト通知を扱う。
type SubscriptionUsecase struct {
	vapidPublicKey string
	subs           SubscriptionRepository
	push           PushSender
}

// NewSubscriptionUsecase は新しい SubscriptionUsecase を生成する。
func NewSubscriptionUsecase(vapidPublicKey string, subs SubscriptionRepository, push PushSender) *SubscriptionUsecase {
	return &SubscriptionUsecase{
		vapidPublicKey: vapidPublicKey,
		subs:           subs,
		push:           push,
	}
}

// VAPIDPublicKey はブラウザの購読開始に使う公開鍵を返す。
func (u *SubscriptionUsecase) VAPIDPublicKey() string {
	return u.vapidPublicKey
}

// Subscribe はブラウザの PushSubscription を保存する。
func (u *SubscriptionUsecase) Subscribe(sub domain.PushSubscription, userID string) error {
	if sub.Endpoint == "" || sub.P256dh == "" || sub.Auth == "" {
		return ErrInvalidInput
	}
	return u.subs.AddForUser(sub, userID)
}

// Unsubscribe は指定 endpoint の購読を削除する。
func (u *SubscriptionUsecase) Unsubscribe(endpoint string) error {
	if endpoint == "" {
		return ErrInvalidInput
	}
	u.subs.Remove(endpoint)
	return nil
}

// SendTestNotification は履歴に残さない疎通確認用のテスト通知を送信する。
func (u *SubscriptionUsecase) SendTestNotification(ctx context.Context, n domain.PushNotification) {
	if n.Title == "" {
		n.Title = "テスト通知"
	}
	if n.Body == "" {
		n.Body = "monitoring-pwa からのテスト通知である。"
	}
	if n.Level == "" {
		n.Level = "info"
	}
	if n.URL == "" {
		n.URL = "/"
	}
	if u.push != nil {
		u.push.Broadcast(ctx, n)
	}
}
