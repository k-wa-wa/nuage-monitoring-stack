package usecase

import (
	"context"
	"errors"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrChannelNotFound   = errors.New("channel not found")
	ErrChannelFetch      = errors.New("failed to fetch channel")
	ErrThreadNotFound    = errors.New("thread parent not found")
	ErrThreadVerify      = errors.New("failed to verify thread parent")
	ErrForbidden         = errors.New("forbidden")
	ErrInvalidInput      = errors.New("invalid input")
	ErrInvalidPassphrase = errors.New("invalid passphrase")
	ErrInvalidSession    = errors.New("invalid or expired session")
	ErrTooManyRequests   = errors.New("too many login attempts")
	ErrRequestCancelled  = errors.New("request cancelled")
	ErrInvalidAuth       = errors.New("invalid auth")
)

// ChannelRepository はチャンネルと既読位置の永続化ポートである。
type ChannelRepository interface {
	EnsureUserChannels(users []domain.User) error
	GetChannelByID(id string) (*domain.Channel, error)
	ListChannelsForUser(userID string) ([]domain.Channel, error)
	ListAllChannels(includePublic, includeIM bool) ([]domain.Channel, error)
	MarkChannelRead(userID, channelID, ts string) error
}

// MessageRepository はメッセージ・スレッド・検索・通知一覧の永続化ポートである。
type MessageRepository interface {
	CreateMessage(p domain.CreateMessageParams) (*domain.Message, error)
	UpdateMessage(channelID, ts, text, blocks string) (*domain.Message, error)
	UpdateAlertRoot(channelID, ts string, u domain.AlertRootUpdate) (*domain.Message, error)
	GetMessageByTS(channelID, ts string) (*domain.Message, error)
	ListChannelMessagesDesc(channelID, oldest, latest string, inclusive bool, limit int) ([]domain.Message, bool, error)
	ListChannelMessagesAsc(channelID, beforeTS string, limit int) ([]domain.Message, bool, error)
	GetThreadMessages(channelID, parentTS string) (*domain.Message, []domain.Message, error)
	SearchMessages(query, channelID, userID string, limit int) ([]domain.SearchResult, error)
	ListActivity(filter domain.ActivityFilter, beforeTS string, limit int) ([]domain.ActivityItem, bool, error)
	CountFiringAlerts() (int, error)
}

// AlertStateRepository は発生中アラートのスレッド追跡状態の永続化ポートである。
type AlertStateRepository interface {
	GetAlertState(fingerprint string) (*domain.AlertState, error)
	SetAlertOpenThread(fingerprint, alertname, threadTS string) error
	ListOpenAlerts() ([]domain.AlertState, error)
}

// SubscriptionRepository は Web Push 購読の永続化ポートである。
type SubscriptionRepository interface {
	AddForUser(sub domain.PushSubscription, userID string) error
	Remove(endpoint string)
	List() []domain.PushSubscription
	ListByUser(userID string) []domain.PushSubscription
}

// PushSender は Web Push 通知を送信するポートである。
type PushSender interface {
	Broadcast(ctx context.Context, n domain.PushNotification)
	BroadcastToUser(ctx context.Context, userID string, n domain.PushNotification)
}

// AlertLister は Alertmanager に今あるアラート一覧を取得するポートである。
type AlertLister interface {
	ListAlerts(ctx context.Context) ([]domain.ActiveAlert, error)
}

// Locker はキー単位の排他制御ポートである。
type Locker interface {
	Lock(ctx context.Context, key string) (unlock func(), err error)
}

// EventPublisher はメッセージの作成・更新をブラウザ (SSE) と bot (Socket Mode) へ配信するポートである。
type EventPublisher interface {
	PublishMessageCreated(ch domain.Channel, msg domain.Message) (deliveredToBot bool)
	PublishMessageUpdated(ch domain.Channel, msg domain.Message)
	IsBotOnline() bool
}

// BrowserEventSubscriber はブラウザ向け SSE ストリームの購読ポートである。
type BrowserEventSubscriber interface {
	SubscribeBrowser(userID string) (<-chan domain.BrowserEvent, func())
	IsBotOnline() bool
}

// SocketSession は接続中の Socket Mode クライアントへの送信抽象である。
type SocketSession interface {
	SendJSON(v interface{}) error
}

// SocketHub は Socket Mode のチケット・接続・ack を管理するポートである。
type SocketHub interface {
	IssueTicket() string
	ConsumeTicket(ticket string) bool
	RegisterSocket(sess SocketSession) (uint64, func(), error)
	HandleAck(raw []byte) (string, bool)
	IsBotOnline() bool
}
