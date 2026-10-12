package alertmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetActiveAlerts(t *testing.T) {
	mockResponse := `[
		{
			"annotations": {
				"summary": "Disk Space Low",
				"description": "Root filesystem is almost full"
			},
			"endsAt": "2026-10-12T02:00:00Z",
			"fingerprint": "fp-disk-low",
			"generatorURL": "http://grafana/explore",
			"labels": {
				"alertname": "DiskSpaceLow",
				"severity": "critical"
			},
			"startsAt": "2026-10-12T01:00:00Z",
			"status": {
				"state": "active"
			},
			"updatedAt": "2026-10-12T01:30:00Z"
		},
		{
			"annotations": {
				"summary": "Watchdog firing",
				"description": "Watchdog dummy alert"
			},
			"fingerprint": "fp-watchdog",
			"labels": {
				"alertname": "Watchdog",
				"severity": "none"
			},
			"status": {
				"state": "active"
			}
		},
		{
			"annotations": {
				"summary": "Info Inhibitor",
				"description": "Info inhibitor dummy alert"
			},
			"fingerprint": "fp-info-inhibitor",
			"labels": {
				"alertname": "InfoInhibitor",
				"severity": "none"
			},
			"status": {
				"state": "active"
			}
		},
		{
			"annotations": {
				"summary": "Suppressed Alert",
				"description": "This alert is suppressed"
			},
			"fingerprint": "fp-suppressed",
			"labels": {
				"alertname": "SomeWarning",
				"severity": "warning"
			},
			"status": {
				"state": "suppressed"
			}
		}
	]`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/alerts" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockResponse))
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	alerts, err := client.GetActiveAlerts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(alerts) != 1 {
		t.Fatalf("expected 1 active alert, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Alertname != "DiskSpaceLow" {
		t.Errorf("expected alertname DiskSpaceLow, got %s", a.Alertname)
	}
	if a.Level != "error" {
		t.Errorf("expected level error for critical severity, got %s", a.Level)
	}
	if a.Summary != "Disk Space Low" {
		t.Errorf("expected summary 'Disk Space Low', got %s", a.Summary)
	}
	if a.Status != "firing" {
		t.Errorf("expected status firing, got %s", a.Status)
	}
	if a.ID == 0 {
		t.Errorf("expected non-zero ID, got %d", a.ID)
	}
}
