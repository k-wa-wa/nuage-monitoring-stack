package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// BotStatus は画面に返す bot の識別情報とオンライン状態である。
type BotStatus struct {
	ID     string
	UserID string
	Name   string
	Online bool
}

// BootstrapResult は初期画面向けのユーザー・チャンネル・bot 状態の組である。
type BootstrapResult struct {
	User     domain.User
	Channels []domain.Channel
	Bot      BotStatus
}

// PostHumanMessageResult は人間ユーザーの投稿結果である。
type PostHumanMessageResult struct {
	Message        domain.Message
	BotOnline      bool
	DeliveredToBot bool
}

// ChatUsecase はチャンネル・メッセージ・スレッド・既読・検索・SSE 購読を扱う。
type ChatUsecase struct {
	channels ChannelRepository
	messages MessageRepository
	events   EventPublisher
	browser  BrowserEventSubscriber
	push     PushSender
}

// NewChatUsecase は新しい ChatUsecase を生成する。
func NewChatUsecase(
	channels ChannelRepository,
	messages MessageRepository,
	events EventPublisher,
	browser BrowserEventSubscriber,
	push PushSender,
) *ChatUsecase {
	return &ChatUsecase{
		channels: channels,
		messages: messages,
		events:   events,
		browser:  browser,
		push:     push,
	}
}

// Bootstrap はログイン後の初期表示データを返す。
func (c *ChatUsecase) Bootstrap(user domain.User) (BootstrapResult, error) {
	chs, err := c.channels.ListChannelsForUser(user.ID)
	if err != nil {
		return BootstrapResult{}, err
	}
	online := false
	if c.events != nil {
		online = c.events.IsBotOnline()
	}
	return BootstrapResult{
		User:     user,
		Channels: chs,
		Bot: BotStatus{
			ID:     domain.SlackBotID,
			UserID: domain.SlackBotUserID,
			Name:   domain.SlackBotName,
			Online: online,
		},
	}, nil
}

// ListChannelsForUser はユーザーがアクセス可能なチャンネル一覧を返す。
func (c *ChatUsecase) ListChannelsForUser(userID string) ([]domain.Channel, error) {
	return c.channels.ListChannelsForUser(userID)
}

// ListAllChannels は Slack API 向けにチャンネル一覧を返す。
func (c *ChatUsecase) ListAllChannels(includePublic, includeIM bool) ([]domain.Channel, error) {
	return c.channels.ListAllChannels(includePublic, includeIM)
}

// GetChannel は ID でチャンネルを取得する (アクセス制御なし、Slack API 用)。
func (c *ChatUsecase) GetChannel(channelID string) (*domain.Channel, error) {
	return c.channels.GetChannelByID(channelID)
}

func (c *ChatUsecase) authorizeChannelAccess(userID, channelID string) (*domain.Channel, error) {
	ch, err := c.channels.GetChannelByID(channelID)
	if err != nil {
		return nil, err
	}
	if ch.IsIM && ch.DMUserID != userID {
		return nil, ErrForbidden
	}
	return ch, nil
}

// ListMessages は人間向け UI 向けにチャンネルのメッセージ一覧（昇順）を返す。
func (c *ChatUsecase) ListMessages(userID, channelID, beforeTS string, limit int) (*domain.Channel, []domain.Message, bool, error) {
	ch, err := c.authorizeChannelAccess(userID, channelID)
	if err != nil {
		return nil, nil, false, err
	}
	msgs, hasMore, err := c.messages.ListChannelMessagesAsc(ch.ID, beforeTS, limit)
	if err != nil {
		return nil, nil, false, err
	}
	return ch, msgs, hasMore, nil
}

// ListMessagesDesc は Slack API (`conversations.history`) 向けにチャンネルのメッセージ一覧（降順）を返す。
func (c *ChatUsecase) ListMessagesDesc(channelID, oldest, latest string, inclusive bool, limit int) ([]domain.Message, bool, error) {
	if _, err := c.channels.GetChannelByID(channelID); err != nil {
		return nil, false, err
	}
	return c.messages.ListChannelMessagesDesc(channelID, oldest, latest, inclusive, limit)
}

// GetThreadForUser は人間向け UI 向けにスレッドの親メッセージと返信一覧を返す。
func (c *ChatUsecase) GetThreadForUser(userID, channelID, parentTS string) (*domain.Channel, *domain.Message, []domain.Message, error) {
	ch, err := c.authorizeChannelAccess(userID, channelID)
	if err != nil {
		return nil, nil, nil, err
	}
	parent, replies, err := c.messages.GetThreadMessages(ch.ID, parentTS)
	if err != nil {
		return nil, nil, nil, err
	}
	return ch, parent, replies, nil
}

// GetThread は Slack API (`conversations.replies`) 向けにスレッドの親メッセージと返信一覧を返す。
func (c *ChatUsecase) GetThread(channelID, parentTS string) (*domain.Message, []domain.Message, error) {
	return c.messages.GetThreadMessages(channelID, parentTS)
}

// PostHumanMessage は人間ユーザーからのメッセージを保存し、既読を更新し、SSE・Socket Mode へ配信する。
func (c *ChatUsecase) PostHumanMessage(ctx context.Context, user domain.User, channelID, rawText, threadTS string) (PostHumanMessageResult, error) {
	ch, err := c.authorizeChannelAccess(user.ID, channelID)
	if err != nil {
		return PostHumanMessageResult{}, err
	}
	text := strings.TrimSpace(rawText)
	if text == "" {
		return PostHumanMessageResult{}, ErrInvalidInput
	}

	normalizedText := domain.NormalizeBotMentions(text)
	threadTS = strings.TrimSpace(threadTS)
	if threadTS != "" {
		if _, err := c.messages.GetMessageByTS(ch.ID, threadTS); err != nil {
			if errors.Is(err, ErrNotFound) {
				return PostHumanMessageResult{}, ErrThreadNotFound
			}
			return PostHumanMessageResult{}, err
		}
	}

	msg, err := c.messages.CreateMessage(domain.CreateMessageParams{
		ChannelID: ch.ID,
		ThreadTS:  threadTS,
		UserID:    user.ID,
		Username:  user.DisplayName,
		Text:      normalizedText,
	})
	if err != nil {
		return PostHumanMessageResult{}, err
	}

	_ = c.channels.MarkChannelRead(user.ID, ch.ID, msg.TS)
	delivered := publishMessageCreated(ctx, c.events, nil, *ch, *msg, nil)
	online := false
	if c.events != nil {
		online = c.events.IsBotOnline()
	}

	return PostHumanMessageResult{
		Message:        *msg,
		BotOnline:      online,
		DeliveredToBot: delivered,
	}, nil
}

// PostBotMessage は Slack bot からのメッセージを保存し、SSE・Socket Mode・Web Push へ配信する。
func (c *ChatUsecase) PostBotMessage(ctx context.Context, channelID, threadTS, username, text, blocksJSON string) (*domain.Message, error) {
	ch, err := c.channels.GetChannelByID(channelID)
	if err != nil {
		return nil, err
	}
	if threadTS != "" {
		if _, err := c.messages.GetMessageByTS(channelID, threadTS); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrThreadNotFound
			}
			return nil, err
		}
	}
	if username == "" {
		username = domain.SlackBotName
	}

	msg, err := c.messages.CreateMessage(domain.CreateMessageParams{
		ChannelID: channelID,
		ThreadTS:  threadTS,
		UserID:    domain.SlackBotUserID,
		BotID:     domain.SlackBotID,
		Username:  username,
		Text:      text,
		Blocks:    blocksJSON,
		Level:     "info",
	})
	if err != nil {
		return nil, err
	}

	pushURL := domain.ChannelURL(channelID)
	if msg.ThreadTS != "" {
		pushURL = domain.ThreadURL(channelID, msg.ThreadTS)
	}
	pushTitle := fmt.Sprintf("%s (#%s)", username, ch.Name)
	if ch.IsIM {
		pushTitle = fmt.Sprintf("%s (DM)", username)
	}
	publishMessageCreated(ctx, c.events, c.push, *ch, *msg, &domain.PushNotification{
		Title: pushTitle,
		Body:  text,
		URL:   pushURL,
		Level: "info",
		Tag:   fmt.Sprintf("msg-%s-%s", channelID, msg.TS),
	})

	return msg, nil
}

// UpdateBotMessage は Slack bot からのメッセージ更新を保存し、SSE へ配信する。
func (c *ChatUsecase) UpdateBotMessage(ctx context.Context, channelID, ts, text, blocksJSON string) (*domain.Message, error) {
	ch, err := c.channels.GetChannelByID(channelID)
	if err != nil {
		return nil, err
	}
	updated, err := c.messages.UpdateMessage(channelID, ts, text, blocksJSON)
	if err != nil {
		return nil, err
	}
	publishMessageUpdated(ctx, c.events, nil, *ch, *updated, nil)
	return updated, nil
}

// MarkRead は指定ユーザーのチャンネル既読位置を更新する。
func (c *ChatUsecase) MarkRead(userID, channelID, ts string) error {
	ch, err := c.authorizeChannelAccess(userID, channelID)
	if err != nil {
		return err
	}
	return c.channels.MarkChannelRead(userID, ch.ID, ts)
}

// Search はユーザーが閲覧可能なチャンネル内をキーワード検索する。
func (c *ChatUsecase) Search(userID, query, channelID string, limit int) ([]domain.SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []domain.SearchResult{}, nil
	}
	return c.messages.SearchMessages(q, channelID, userID, limit)
}

// SubscribeEvents はブラウザ SSE 向けのイベントストリームを購読する。
func (c *ChatUsecase) SubscribeEvents(userID string) (<-chan domain.BrowserEvent, func(), bool) {
	ch, cancel := c.browser.SubscribeBrowser(userID)
	return ch, cancel, c.browser.IsBotOnline()
}
