package alertmanager

import (
	"context"
	"encoding/json"
	"fmt"
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
	Annotations  map[string]string `json:"annotations"`
	EndsAt       time.Time         `json:"endsAt"`
	Fingerprint  string            `json:"fingerprint"`
	GeneratorURL string            `json:"generatorURL"`
	Labels       map[string]string `json:"labels"`
	StartsAt     time.Time         `json:"startsAt"`
	Status       struct {
		InhibitedBy []string `json:"inhibitedBy"`
		SilencedBy  []string `json:"silencedBy"`
		State       string   `json:"state"`
	} `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Alert は Alertmanager に今あるアラート 1 件である。
type Alert struct {
	// Fingerprint は Webhook と同じ作り方 (push.GenerateFingerprint) でラベルから作る
	Fingerprint  string
	Alertname    string
	Labels       map[string]string
	Annotations  map[string]string
	GeneratorURL string
	StartsAt     time.Time
	// Suppressed はサイレンス・抑制で通知が止められていることを表す (発生はしている)
	Suppressed bool
}

// ListAlerts は Alertmanager に今あるアラートを、サイレンス・抑制中のものも含めて返す。
// 突き合わせでは「Alertmanager から消えたか」を見るため、通知が止められているだけのものも含める。
func (c *Client) ListAlerts(ctx context.Context) ([]Alert, error) {
	reqURL := fmt.Sprintf("%s/api/v2/alerts?active=true&silenced=true&inhibited=true&unprocessed=true", c.baseURL)
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

	alerts := make([]Alert, 0, len(rawAlerts))
	for _, a := range rawAlerts {
		name := a.Labels["alertname"]
		if name == "" {
			name = "Alert"
		}
		fp := a.Fingerprint
		if len(a.Labels) > 0 {
			fp = push.GenerateFingerprint(a.Labels)
		}
		alerts = append(alerts, Alert{
			Fingerprint:  fp,
			Alertname:    name,
			Labels:       a.Labels,
			Annotations:  a.Annotations,
			GeneratorURL: a.GeneratorURL,
			StartsAt:     a.StartsAt,
			Suppressed:   a.Status.State == "suppressed",
		})
	}

	slog.Debug("fetched alerts from alertmanager", "count", len(alerts))
	return alerts, nil
}
