package usecase

import (
	"context"
	"strings"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// SocketUsecase は SPIFFE JWT-SVID 検証と Slack Socket Mode の接続チケット発行・認可・登録・ack 処理を扱う。
type SocketUsecase struct {
	hub        SocketHub
	verifier   BotTokenVerifier
	bots       []domain.BotIdentity
	botsBySub  map[string]domain.BotIdentity
	botsByUser map[string]domain.BotIdentity
}

// NewSocketUsecase は新しい SocketUsecase を生成する。
func NewSocketUsecase(hub SocketHub, verifier BotTokenVerifier, bots []domain.BotIdentity) *SocketUsecase {
	bySub := make(map[string]domain.BotIdentity, len(bots))
	byUser := make(map[string]domain.BotIdentity, len(bots))
	copied := make([]domain.BotIdentity, len(bots))
	for i, b := range bots {
		copied[i] = b
		if b.Sub != "" {
			bySub[b.Sub] = b
		}
		if b.UserID != "" {
			byUser[b.UserID] = b
		}
	}
	return &SocketUsecase{
		hub:        hub,
		verifier:   verifier,
		bots:       copied,
		botsBySub:  bySub,
		botsByUser: byUser,
	}
}

// VerifyBotToken は JWT-SVID を検証し、許可リスト (BOT_USERS_JSON) に一致する bot の識別情報を返す。
func (u *SocketUsecase) VerifyBotToken(ctx context.Context, token string) (domain.BotIdentity, error) {
	if u.verifier == nil || strings.TrimSpace(token) == "" {
		return domain.BotIdentity{}, ErrInvalidAuth
	}
	sub, err := u.verifier.Verify(ctx, token)
	if err != nil {
		return domain.BotIdentity{}, ErrInvalidAuth
	}
	bot, ok := u.botsBySub[sub]
	if !ok {
		return domain.BotIdentity{}, ErrInvalidAuth
	}
	return bot, nil
}

// OpenConnectionTicket は JWT-SVID を検証し、呼び出し元 bot に紐づいた Socket Mode 用のワンタイムチケットを発行する。
func (u *SocketUsecase) OpenConnectionTicket(ctx context.Context, token string) (string, error) {
	bot, err := u.VerifyBotToken(ctx, token)
	if err != nil {
		return "", err
	}
	return u.hub.IssueTicket(bot), nil
}

// AuthorizeSocket はワンタイムチケットのみで Socket Mode 接続を認可し、紐づく bot の識別情報を返す。
func (u *SocketUsecase) AuthorizeSocket(ticket string) (domain.BotIdentity, bool) {
	return u.hub.ConsumeTicket(ticket)
}

// RegisterSocket は bot に紐づいた Socket Mode 接続を登録し、クリーンアップ関数を返す。
func (u *SocketUsecase) RegisterSocket(bot domain.BotIdentity, sess SocketSession) (uint64, func(), error) {
	return u.hub.RegisterSocket(bot, sess)
}

// HandleAck は Socket Mode クライアントから返された ack を処理する。
func (u *SocketUsecase) HandleAck(raw []byte) (string, bool) {
	return u.hub.HandleAck(raw)
}

// FindBotByUserID は登録された bot を user_id で検索する。
func (u *SocketUsecase) FindBotByUserID(userID string) (domain.BotIdentity, bool) {
	bot, ok := u.botsByUser[userID]
	return bot, ok
}

// DefaultBot は代表 bot を返す。
func (u *SocketUsecase) DefaultBot() domain.BotIdentity {
	return domain.DefaultBot(u.bots)
}
