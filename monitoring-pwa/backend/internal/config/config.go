package config

import "os"

// Config はアプリケーションの設定情報を保持する構造体である。
type Config struct {
	Port            string
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	WebhookToken    string
	DBPath          string
	PrometheusURL   string
}

// Load は環境変数から設定情報をロードする。デフォルト値があるものはフォールバックする。
func Load() Config {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./monitoring-pwa.db"
	}

	promURL := os.Getenv("PROMETHEUS_URL")
	if promURL == "" {
		promURL = "http://kube-prometheus-stack-prometheus.nuage-monitoring-stack.svc.cluster.local:9090"
	}

	return Config{
		Port:            os.Getenv("PORT"),
		VAPIDPublicKey:  os.Getenv("VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey: os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:    os.Getenv("VAPID_SUBJECT"),
		WebhookToken:    os.Getenv("WEBHOOK_TOKEN"),
		DBPath:          dbPath,
		PrometheusURL:   promURL,
	}
}
