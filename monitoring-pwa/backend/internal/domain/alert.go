package domain

import "time"

// IgnoredAlertnames は Alertmanager の route で PWA に送らないアラートである
// (k8s/base/helm/victoria-stack-custom.yaml の "null" receiver と揃える)。
// 突き合わせで「取りこぼした発生」を作る時に、これらを拾わないようにする。
var IgnoredAlertnames = map[string]bool{
	"Watchdog":      true,
	"InfoInhibitor": true,
}

// AlertState はアラート (fingerprint) ごとの「今発生中の回のスレッド」を覚える。
// アラートの中身 (状態・詳細・リンク) はスレッドの親メッセージ (messages) を正とし、ここには写さない。
type AlertState struct {
	Fingerprint  string
	Alertname    string
	OpenThreadTS string
	UpdatedAt    time.Time
}

// AlertRootUpdate はアラートのスレッドの親メッセージ (ルート) に書き戻す状態である。
type AlertRootUpdate struct {
	Status     string
	Level      string
	ResolvedAt *time.Time
	Details    string // 空なら変えない
	URL        string // 空なら変えない
}

// IncomingAlert は Alertmanager の Webhook で届いたアラート 1 件である。
type IncomingAlert struct {
	Fingerprint  string
	Status       string
	Labels       map[string]string
	Annotations  map[string]string
	GeneratorURL string
	Details      string
}

// ActiveAlert は Alertmanager API 上で今あるアラート 1 件である。
type ActiveAlert struct {
	Fingerprint  string
	Alertname    string
	Labels       map[string]string
	Annotations  map[string]string
	GeneratorURL string
	StartsAt     time.Time
	Suppressed   bool
}

// AlertView はアラートの表示名・サマリ・レベル等の正規化済みビューである。
type AlertView struct {
	Fingerprint string
	Name        string
	Summary     string
	Level       string
	Details     string
	URL         string
}

// ViewOfAlert は IncomingAlert から表示用のルールを適用した AlertView を作る。
func ViewOfAlert(a IncomingAlert) AlertView {
	name := a.Labels["alertname"]
	if name == "" {
		name = "Alert"
	}
	summary := a.Annotations["summary"]
	if summary == "" {
		summary = a.Annotations["description"]
	}
	if summary == "" {
		summary = name
	}
	level := "warning"
	if a.Labels["severity"] == "critical" {
		level = "error"
	}
	return AlertView{
		Fingerprint: a.Fingerprint,
		Name:        name,
		Summary:     summary,
		Level:       level,
		Details:     a.Details,
		URL:         a.GeneratorURL,
	}
}
