package usecase

import "crypto/subtle"

// SocketUsecase は Slack Socket Mode の接続チケット発行・認可・登録・ack 処理を扱う。
type SocketUsecase struct {
	hub      SocketHub
	botToken string
	appToken string
}

// NewSocketUsecase は新しい SocketUsecase を生成する。
func NewSocketUsecase(hub SocketHub, botToken, appToken string) *SocketUsecase {
	return &SocketUsecase{
		hub:      hub,
		botToken: botToken,
		appToken: appToken,
	}
}

// VerifyBotToken は Slack Bot トークンを定数時間で照合する。
func (u *SocketUsecase) VerifyBotToken(token string) bool {
	return tokenMatches(token, u.botToken)
}

// OpenConnectionTicket は App トークンを検証し、Socket Mode 用のワンタイムチケットを発行する。
func (u *SocketUsecase) OpenConnectionTicket(appToken string) (string, error) {
	if !tokenMatches(appToken, u.appToken) {
		return "", ErrInvalidAuth
	}
	return u.hub.IssueTicket(), nil
}

// AuthorizeSocket はチケットまたは App トークンで Socket Mode 接続を認可する。
func (u *SocketUsecase) AuthorizeSocket(ticket, bearerToken string) bool {
	return u.hub.ConsumeTicket(ticket) || tokenMatches(bearerToken, u.appToken)
}

// RegisterSocket は Socket Mode 接続を登録し、クリーンアップ関数を返す。
func (u *SocketUsecase) RegisterSocket(sess SocketSession) (uint64, func(), error) {
	return u.hub.RegisterSocket(sess)
}

// HandleAck は Socket Mode クライアントから返された ack を処理する。
func (u *SocketUsecase) HandleAck(raw []byte) (string, bool) {
	return u.hub.HandleAck(raw)
}

func tokenMatches(actual, expected string) bool {
	if actual == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}
