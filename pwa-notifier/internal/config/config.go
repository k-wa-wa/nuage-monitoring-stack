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
		Port:            getEnv("PORT", "8080"),
		VAPIDPublicKey:  os.Getenv("VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey: os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:    getEnv("VAPID_SUBJECT", "mailto:admin@example.com"),
		WebhookToken:    os.Getenv("WEBHOOK_TOKEN"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
