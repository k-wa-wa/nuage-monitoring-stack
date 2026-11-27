package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// NotificationUsecase は Argo CD・Keel・汎用 Webhook からの通知投稿を扱う。
type NotificationUsecase struct {
	messages MessageRepository
	events   EventPublisher
	push     PushSender
}

// NewNotificationUsecase は新しい NotificationUsecase を生成する。
func NewNotificationUsecase(
	messages MessageRepository,
	events EventPublisher,
	push PushSender,
) *NotificationUsecase {
	return &NotificationUsecase{
		messages: messages,
		events:   events,
		push:     push,
	}
}

// PostNotification は通知をその送り手のチャンネルに投稿し、画面・bot・Web Push へ知らせる。
func (u *NotificationUsecase) PostNotification(ctx context.Context, n domain.Notification) (*domain.Message, error) {
	chID := domain.ChannelForSource(n.Source)
	username := "Nuage Notify"
	switch n.Source {
	case "argocd":
		username = "Argo CD"
	case "keel":
		username = "Keel"
	}
	level := n.Level
	if level == "" {
		level = "info"
	}
	title := strings.TrimSpace(n.Title)
	body := strings.TrimSpace(n.Body)
	text := fmt.Sprintf("**%s**\n%s", title, body)
	if body == "" || body == title {
		text = fmt.Sprintf("**%s**", title)
	}

	msg, err := u.messages.CreateMessage(domain.CreateMessageParams{
		ChannelID:  chID,
		UserID:     domain.SystemBotUserID,
		BotID:      domain.SystemBotID,
		Username:   username,
		Text:       text,
		Level:      level,
		SourceKind: "notification",
		Details:    n.Details,
		URL:        n.URL,
	})
	if err != nil {
		return nil, err
	}

	ch := domain.Channel{ID: chID, IsChannel: true}
	publishMessageCreated(ctx, u.events, u.push, ch, *msg, &domain.PushNotification{
		Title:      n.Title,
		Body:       n.Body,
		URL:        domain.ThreadURL(chID, msg.TS),
		TargetURL:  n.URL,
		DirectOpen: n.DirectOpen,
		Level:      level,
		Tag:        fmt.Sprintf("msg-%s-%s", chID, msg.TS),
	})
	return msg, nil
}
