package domain

// BotIdentity は SPIFFE 認証で特定された呼び出し元 bot の識別情報を表す。
type BotIdentity struct {
	Sub       string
	UserID    string
	BotID     string
	Name      string
	IsDefault bool
}

// DefaultBot は登録された bot 一覧から代表 bot (is_default: true、未指定時は先頭) を返す。
func DefaultBot(bots []BotIdentity) BotIdentity {
	for _, b := range bots {
		if b.IsDefault {
			return b
		}
	}
	if len(bots) > 0 {
		return bots[0]
	}
	return BotIdentity{
		UserID: SlackBotUserID,
		BotID:  SlackBotID,
		Name:   SlackBotName,
	}
}
