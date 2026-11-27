package usecase

import (
	"context"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// publishMessageCreated はメッセージ作成を画面 (SSE)・bot (Socket Mode)・Web Push へ一括通知する。
// Web Push が不要な場合 (人間の投稿や解決返信) は push が nil になる。
func publishMessageCreated(
	ctx context.Context,
	events EventPublisher,
	pushSender PushSender,
	ch domain.Channel,
	msg domain.Message,
	push *domain.PushNotification,
) (deliveredToBot bool) {
	if events != nil {
		deliveredToBot = events.PublishMessageCreated(ch, msg)
	}
	if push != nil && pushSender != nil {
		if ch.IsIM && ch.DMUserID != "" {
			pushSender.BroadcastToUser(ctx, ch.DMUserID, *push)
		} else {
			pushSender.Broadcast(ctx, *push)
		}
	}
	return deliveredToBot
}

// publishMessageUpdated はメッセージ更新を画面 (SSE) へ通知し、必要なら Web Push を送る。
func publishMessageUpdated(
	ctx context.Context,
	events EventPublisher,
	pushSender PushSender,
	ch domain.Channel,
	msg domain.Message,
	push *domain.PushNotification,
) {
	if events != nil {
		events.PublishMessageUpdated(ch, msg)
	}
	if push != nil && pushSender != nil {
		pushSender.Broadcast(ctx, *push)
	}
}
