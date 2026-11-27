package alertmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListAlerts(t *testing.T) {
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
		// 突き合わせでは、サイレンス・抑制中のものも含めて取る
		q := r.URL.Query()
		if q.Get("silenced") != "true" || q.Get("inhibited") != "true" {
			t.Errorf("expected silenced=true&inhibited=true, got %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockResponse))
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	alerts, err := client.ListAlerts(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Watchdog・InfoInhibitor・抑制中のものも含め、Alertmanager にあるものはすべて返す
	if len(alerts) != 4 {
		t.Fatalf("expected 4 alerts, got %d", len(alerts))
	}

	a := alerts[0]
	if a.Alertname != "DiskSpaceLow" || a.Suppressed {
		t.Errorf("unexpected first alert: %+v", a)
	}
	if a.Annotations["summary"] != "Disk Space Low" || a.GeneratorURL != "http://grafana/explore" {
		t.Errorf("annotations / generatorURL not carried: %+v", a)
	}
	// Alertmanager が振る fingerprint をそのまま使う (Webhook の fingerprint と同じ値)
	if a.Fingerprint != "fp-disk-low" {
		t.Errorf("expected alertmanager's fingerprint fp-disk-low, got %q", a.Fingerprint)
	}
	if !alerts[3].Suppressed {
		t.Errorf("expected suppressed state to be reported for %s", alerts[3].Alertname)
	}
}

func TestListAlertsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer ts.Close()
	if _, err := NewClient(ts.URL).ListAlerts(context.Background()); err == nil {
		t.Fatal("expected an error when alertmanager responds with non-200")
	}
}
