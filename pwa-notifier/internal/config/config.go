package config

import "os"

type Config struct {
	Port            string
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	WebhookToken    string
}

func Load() Config {
	return Config{
		Port:            os.Getenv("PORT"),
		VAPIDPublicKey:  os.Getenv("VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey: os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:    os.Getenv("VAPID_SUBJECT"),
		WebhookToken:    os.Getenv("WEBHOOK_TOKEN"),
	}
}
