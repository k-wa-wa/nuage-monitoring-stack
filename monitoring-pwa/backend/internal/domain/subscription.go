package domain

// PushSubscription はブラウザから登録された Web Push 購読情報である。
type PushSubscription struct {
	Endpoint string
	P256dh   string
	Auth     string
	UserID   string
}
