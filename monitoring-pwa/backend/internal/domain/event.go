package domain

// BrowserEventType はブラウザ向け SSE ストリームで配信するイベント種別である。
type BrowserEventType string

const (
	BrowserEventBotStatus      BrowserEventType = "bot_status"
	BrowserEventMessageCreated BrowserEventType = "message_created"
	BrowserEventMessageUpdated BrowserEventType = "message_updated"
)

// BrowserEvent はブラウザ側の SSE ストリームへ配信するドメインイベントである。
type BrowserEvent struct {
	Type      BrowserEventType
	ChannelID string
	Message   *Message
	BotOnline *bool
}
