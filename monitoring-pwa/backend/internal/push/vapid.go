package push

import webpush "github.com/SherClockHolmes/webpush-go"

// GenerateVAPIDKeys は Web Push 送信の署名に用いる新規 VAPID キーペアを生成する。
func GenerateVAPIDKeys() (privateKey, publicKey string, err error) {
	return webpush.GenerateVAPIDKeys()
}
