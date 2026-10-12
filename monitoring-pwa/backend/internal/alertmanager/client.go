package alertmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

// Client は Alertmanager API と通信するクライアントである。
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient は新しい Alertmanager クライアントを初期化する。
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// alertmanagerAlert は Alertmanager /api/v2/alerts のレスポンス要素である。
type alertmanagerAlert struct {
	Annotations map[string]string `json:"annotations"`
	EndsAt      time.Time         `json:"endsAt"`
	Fingerprint string            `json:"fingerprint"`
	GeneratorURL string           `json:"generatorURL"`
	Labels      map[string]string `json:"labels"`
	StartsAt    time.Time         `json:"startsAt"`
	Status      struct {
		InhibitedBy []string `json:"inhibitedBy"`
		SilencedBy  []string `json:"silencedBy"`
		State       string   `json:"state"`
	} `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// idFromFingerprint は fingerprint 文字列から一意な正の int64 ID を生成する。
func idFromFingerprint(fp string) int64 {
	h := fnv.New64a()
	h.Write([]byte(fp))
	return int64(h.Sum64() & 0x7FFFFFFFFFFFFFFF)
}

// GetActiveAlerts は Alertmanager から現在 active なアラート一覧を取得し、push.Alert 型に変換して返す。
func (c *Client) GetActiveAlerts(ctx context.Context) ([]push.Alert, error) {
	reqURL := fmt.Sprintf("%s/api/v2/alerts?active=true&silenced=false&inhibited=false", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch alerts from alertmanager: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from alertmanager: %d", resp.StatusCode)
	}

	var rawAlerts []alertmanagerAlert
	if err := json.NewDecoder(resp.Body).Decode(&rawAlerts); err != nil {
		return nil, fmt.Errorf("failed to decode alertmanager response: %w", err)
	}

	var activeAlerts []push.Alert
	for _, a := range rawAlerts {
		// 抑制状態のアラートは除外する
		if a.Status.State != "active" {
			continue
		}

		alertname := a.Labels["alertname"]
		if alertname == "" {
			alertname = "Alert"
		}

		// 常時発報のダミー/ヘルスチェックアラートは除外する
		if alertname == "Watchdog" || alertname == "InfoInhibitor" {
			continue
		}

		level := "warning"
		severity := a.Labels["severity"]
		if severity == "critical" {
			level = "error"
		} else if severity == "info" {
			level = "info"
		}

		summary := a.Annotations["summary"]
		if summary == "" {
			summary = a.Annotations["description"]
		}
		if summary == "" {
			summary = alertname
		}
		description := a.Annotations["description"]

		detailsBytes, _ := json.Marshal(a)

		id := idFromFingerprint(a.Fingerprint)

		activeAlerts = append(activeAlerts, push.Alert{
			ID:          id,
			Fingerprint: a.Fingerprint,
			Alertname:   alertname,
			Status:      "firing",
			Level:       level,
			Summary:     summary,
			Description: description,
			URL:         a.GeneratorURL,
			Details:     string(detailsBytes),
			FiredAt:     a.StartsAt,
			CreatedAt:   a.StartsAt,
			UpdatedAt:   a.UpdatedAt,
		})
	}

	slog.Debug("fetched active alerts from alertmanager", "count", len(activeAlerts))
	return activeAlerts, nil
}
