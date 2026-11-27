package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/notify"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/realtime"
)

var updateGolden = flag.Bool("update", false, "update golden files")

type contractPushRecord struct {
	TargetUser string          `json:"target_user,omitempty"`
	Payload    json.RawMessage `json:"payload"`
}

type contractPushSpy struct {
	mu      sync.Mutex
	records []contractPushRecord
}

func (s *contractPushSpy) Broadcast(payload []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, contractPushRecord{
		Payload: append(json.RawMessage(nil), payload...),
	})
}

func (s *contractPushSpy) BroadcastToUser(userID string, payload []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, contractPushRecord{
		TargetUser: userID,
		Payload:    append(json.RawMessage(nil), payload...),
	})
}

func (s *contractPushSpy) drain() []contractPushRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.records
	s.records = nil
	return out
}

type contractEnv struct {
	server  *httptest.Server
	cfg     config.Config
	pushSpy *contractPushSpy
}

func newContractEnv(t *testing.T) *contractEnv {
	t.Helper()
	hashAlice, _ := bcrypt.GenerateFromPassword([]byte("alice-secret"), bcrypt.MinCost)
	hashBob, _ := bcrypt.GenerateFromPassword([]byte("bob-secret"), bcrypt.MinCost)

	cfg := config.Config{
		Port:             "8080",
		VAPIDPublicKey:   "test-vapid-public-key",
		VAPIDPrivateKey:  "test-vapid-private-key",
		VAPIDSubject:     "mailto:test@example.com",
		CookieSigningKey: strings.Repeat("s", 32),
		SlackBotToken:    "xoxb-contract-bot-token",
		SlackAppToken:    "xapp-contract-app-token",
		Users: []config.UserConfig{
			{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: string(hashAlice)},
			{ID: "U02BOB", Name: "bob", DisplayName: "Bob", PassphraseHash: string(hashBob)},
		},
	}

	db, err := push.InitDB(filepath.Join(t.TempDir(), "contract.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.EnsureUserChannels(cfg.Users); err != nil {
		t.Fatalf("EnsureUserChannels failed: %v", err)
	}

	hub := realtime.NewHub()
	spy := &contractPushSpy{}
	svc := notify.New(db, hub, spy, notify.NewLocalLocker())
	e := NewRouter(cfg, db, hub, spy, svc)

	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)

	return &contractEnv{
		server:  srv,
		cfg:     cfg,
		pushSpy: spy,
	}
}

type sseEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type sseReader struct {
	resp   *http.Response
	events chan sseEvent
	errs   chan error
}

func startSSE(t *testing.T, baseURL string, cookie *http.Cookie) *sseReader {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/events", nil)
	if err != nil {
		t.Fatalf("new sse request failed: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sse connect failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sse status = %d", resp.StatusCode)
	}

	r := &sseReader{
		resp:   resp,
		events: make(chan sseEvent, 32),
		errs:   make(chan error, 1),
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	go func() {
		scanner := bufio.NewScanner(resp.Body)
		var curEvent string
		var curData string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, ":") {
				continue
			}
			if line == "" {
				if curEvent != "" || curData != "" {
					r.events <- sseEvent{Event: curEvent, Data: json.RawMessage(curData)}
					curEvent = ""
					curData = ""
				}
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				curEvent = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				curData = strings.TrimPrefix(line, "data: ")
			}
		}
		if err := scanner.Err(); err != nil {
			r.errs <- err
		}
	}()
	return r
}

func (r *sseReader) next(t *testing.T) sseEvent {
	t.Helper()
	select {
	case ev := <-r.events:
		return ev
	case err := <-r.errs:
		t.Fatalf("sse read error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for sse event")
	}
	return sseEvent{}
}

func (r *sseReader) expectNone(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case ev := <-r.events:
		t.Fatalf("unexpected sse event: %s %s", ev.Event, string(ev.Data))
	case <-time.After(wait):
	}
}

type httpSnapshot struct {
	Status int         `json:"status"`
	Body   interface{} `json:"body,omitempty"`
}

func doJSONReq(t *testing.T, method, fullURL string, cookie *http.Cookie, headers map[string]string, body interface{}) (int, http.Header, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			reader = strings.NewReader(v)
		case url.Values:
			reader = strings.NewReader(v.Encode())
		default:
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("marshal request body failed: %v", err)
			}
			reader = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequest(method, fullURL, reader)
	if err != nil {
		t.Fatalf("NewRequest %s %s failed: %v", method, fullURL, err)
	}
	if body != nil {
		if _, isForm := body.(url.Values); isForm {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do %s %s failed: %v", method, fullURL, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	return resp.StatusCode, resp.Header, raw
}

func toSnapshot(status int, raw []byte) httpSnapshot {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return httpSnapshot{Status: status}
	}
	var parsed interface{}
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return httpSnapshot{Status: status, Body: string(trimmed)}
	}
	return httpSnapshot{Status: status, Body: parsed}
}

type normalizer struct {
	tsMap  map[string]string
	tsSeq  int
	host   string
}

func newNormalizer(host string) *normalizer {
	return &normalizer{
		tsMap: make(map[string]string),
		host:  host,
	}
}

var (
	tsRegex     = regexp.MustCompile(`\b\d{10}\.\d{6}\b`)
	envIDRegex  = regexp.MustCompile(`\benv-[0-9a-f]+\b`)
	evIDRegex   = regexp.MustCompile(`\bEv-[0-9a-f]+\b`)
	ticketRegex = regexp.MustCompile(`ticket=[0-9a-f]+`)
	isoTimeRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)
)

func (n *normalizer) mapTS(ts string) string {
	if mapped, ok := n.tsMap[ts]; ok {
		return mapped
	}
	n.tsSeq++
	mapped := fmt.Sprintf("TS_%02d", n.tsSeq)
	n.tsMap[ts] = mapped
	return mapped
}

func (n *normalizer) normalizeString(s string) string {
	if isoTimeRe.MatchString(s) {
		return "<TIME>"
	}
	if n.host != "" {
		s = strings.ReplaceAll(s, n.host, "<HOST>")
	}
	s = ticketRegex.ReplaceAllString(s, "ticket=<TICKET>")
	s = envIDRegex.ReplaceAllString(s, "<ENVELOPE_ID>")
	s = evIDRegex.ReplaceAllString(s, "<EVENT_ID>")
	s = tsRegex.ReplaceAllStringFunc(s, func(match string) string {
		return n.mapTS(match)
	})
	return s
}

func (n *normalizer) collectTS(v interface{}, seen map[string]struct{}) {
	switch x := v.(type) {
	case map[string]interface{}:
		for _, val := range x {
			n.collectTS(val, seen)
		}
	case []interface{}:
		for _, item := range x {
			n.collectTS(item, seen)
		}
	case string:
		for _, m := range tsRegex.FindAllString(x, -1) {
			seen[m] = struct{}{}
		}
	}
}

func (n *normalizer) normalizeValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := x[k]
			switch k {
			case "created", "last_set", "event_time":
				out[k] = "<UNIX_TIME>"
			case "started_connecting":
				out[k] = "<TIME>"
			case "created_at", "updated_at", "fired_at", "resolved_at":
				if val != nil && val != "" {
					out[k] = "<TIME>"
				} else {
					out[k] = val
				}
			default:
				out[k] = n.normalizeValue(val)
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, item := range x {
			out[i] = n.normalizeValue(item)
		}
		return out
	case string:
		return n.normalizeString(x)
	default:
		return v
	}
}

func (n *normalizer) normalizeJSON(raw interface{}) interface{} {
	b, err := json.Marshal(raw)
	if err != nil {
		return raw
	}
	var parsed interface{}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return raw
	}
	seen := make(map[string]struct{})
	n.collectTS(parsed, seen)
	sortedTS := make([]string, 0, len(seen))
	for ts := range seen {
		sortedTS = append(sortedTS, ts)
	}
	sort.Strings(sortedTS)
	for _, ts := range sortedTS {
		n.mapTS(ts)
	}
	return n.normalizeValue(parsed)
}

func assertGolden(t *testing.T, name string, got interface{}, n *normalizer) {
	t.Helper()
	normalized := n.normalizeJSON(got)
	pretty, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden %s failed: %v", name, err)
	}
	pretty = append(pretty, '\n')

	goldenPath := filepath.Join("testdata", name+".golden.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
			t.Fatalf("mkdir testdata failed: %v", err)
		}
		if err := os.WriteFile(goldenPath, pretty, 0644); err != nil {
			t.Fatalf("write golden %s failed: %v", goldenPath, err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s failed (run with -update to create): %v", goldenPath, err)
	}
	if string(pretty) != string(want) {
		t.Fatalf("golden mismatch for %s:\n--- got ---\n%s\n--- want ---\n%s", name, string(pretty), string(want))
	}
}

func TestContractHumanAPI(t *testing.T) {
	env := newContractEnv(t)
	base := env.server.URL
	u, _ := url.Parse(base)
	norm := newNormalizer(u.Host)

	results := map[string]interface{}{}

	// 1. Auth: unauthenticated /me -> 401
	st, _, raw := doJSONReq(t, http.MethodGet, base+"/api/auth/me", nil, nil, nil)
	results["auth_me_unauthenticated"] = toSnapshot(st, raw)

	// 2. Auth: bad login -> 401
	st, _, raw = doJSONReq(t, http.MethodPost, base+"/api/auth/login", nil, nil, map[string]string{"passphrase": "wrong"})
	results["auth_login_invalid"] = toSnapshot(st, raw)

	// 3. Auth: valid login -> 200 + cookie
	req, _ := http.NewRequest(http.MethodPost, base+"/api/auth/login", strings.NewReader(`{"passphrase":"alice-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	loginResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	loginRaw, _ := io.ReadAll(loginResp.Body)
	loginResp.Body.Close()
	results["auth_login_ok"] = toSnapshot(loginResp.StatusCode, loginRaw)

	var aliceCookie *http.Cookie
	for _, c := range loginResp.Cookies() {
		if c.Name == "nuage_notify_session" {
			aliceCookie = c
		}
	}
	if aliceCookie == nil {
		t.Fatal("expected session cookie")
	}

	// 4. Auth: /me authenticated
	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/auth/me", aliceCookie, nil, nil)
	results["auth_me_authenticated"] = toSnapshot(st, raw)

	// 5. Bootstrap & Channels
	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/bootstrap", aliceCookie, nil, nil)
	results["bootstrap"] = toSnapshot(st, raw)

	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/channels", aliceCookie, nil, nil)
	results["channels_list"] = toSnapshot(st, raw)

	// 6. Post root message & thread reply in C05GENERAL
	st, _, raw = doJSONReq(t, http.MethodPost, base+"/api/channels/C05GENERAL/messages", aliceCookie, nil, map[string]string{
		"text": "hello @bot check status",
	})
	results["post_message"] = toSnapshot(st, raw)
	var postRes struct {
		Message struct {
			TS string `json:"ts"`
		} `json:"message"`
	}
	_ = json.Unmarshal(raw, &postRes)
	rootTS := postRes.Message.TS

	st, _, raw = doJSONReq(t, http.MethodPost, base+"/api/channels/C05GENERAL/messages", aliceCookie, nil, map[string]string{
		"text":      "follow-up reply",
		"thread_ts": rootTS,
	})
	results["post_thread_reply"] = toSnapshot(st, raw)

	// 7. Messages list & Thread get
	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/channels/C05GENERAL/messages", aliceCookie, nil, nil)
	results["list_messages"] = toSnapshot(st, raw)

	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/channels/C05GENERAL/threads/"+rootTS, aliceCookie, nil, nil)
	results["get_thread"] = toSnapshot(st, raw)

	// 8. Mark read
	st, _, raw = doJSONReq(t, http.MethodPost, base+"/api/channels/C05GENERAL/read", aliceCookie, nil, map[string]string{
		"ts": rootTS,
	})
	results["mark_read"] = toSnapshot(st, raw)

	// 9. Search (matching & empty)
	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/search?q=follow-up", aliceCookie, nil, nil)
	results["search_match"] = toSnapshot(st, raw)

	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/search?q=", aliceCookie, nil, nil)
	results["search_empty"] = toSnapshot(st, raw)

	// 10. Web Push endpoints
	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/vapid-public-key", aliceCookie, nil, nil)
	results["vapid_public_key"] = toSnapshot(st, raw)

	st, _, raw = doJSONReq(t, http.MethodPost, base+"/api/subscribe", aliceCookie, nil, map[string]interface{}{
		"endpoint": "https://push.example/sub1",
		"keys": map[string]string{
			"p256dh": "test-p256dh",
			"auth":   "test-auth",
		},
	})
	results["subscribe_ok"] = toSnapshot(st, raw)

	st, _, raw = doJSONReq(t, http.MethodPost, base+"/api/subscribe", aliceCookie, nil, map[string]interface{}{
		"endpoint": "https://push.example/bad",
	})
	results["subscribe_invalid"] = toSnapshot(st, raw)

	st, _, raw = doJSONReq(t, http.MethodPost, base+"/api/unsubscribe", aliceCookie, nil, map[string]string{
		"endpoint": "https://push.example/sub1",
	})
	results["unsubscribe_ok"] = toSnapshot(st, raw)

	st, _, raw = doJSONReq(t, http.MethodPost, base+"/api/test-notify", aliceCookie, nil, map[string]interface{}{})
	results["test_notify_default"] = toSnapshot(st, raw)
	results["test_notify_pushes"] = env.pushSpy.drain()

	// 11. Logout
	st, hdr, raw := doJSONReq(t, http.MethodPost, base+"/api/auth/logout", aliceCookie, nil, nil)
	results["auth_logout"] = map[string]interface{}{
		"response":   toSnapshot(st, raw),
		"set_cookie": hdr.Values("Set-Cookie"),
	}

	assertGolden(t, "human_api", results, norm)
}

func TestContractWebhooksAndActivity(t *testing.T) {
	env := newContractEnv(t)
	base := env.server.URL
	u, _ := url.Parse(base)
	norm := newNormalizer(u.Host)

	// Login as Alice to observe SSE and /api/activity
	req, _ := http.NewRequest(http.MethodPost, base+"/api/auth/login", strings.NewReader(`{"passphrase":"alice-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	loginResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	loginResp.Body.Close()
	aliceCookie := loginResp.Cookies()[0]

	sse := startSSE(t, base, aliceCookie)
	initialSSE := sse.next(t)

	results := map[string]interface{}{
		"sse_initial": initialSSE,
	}

	// 1. Alertmanager firing
	alertBody := func(status string) map[string]interface{} {
		return map[string]interface{}{
			"status": status,
			"alerts": []map[string]interface{}{
				{
					"fingerprint": "fp-contract-1",
					"status":      status,
					"labels": map[string]string{
						"alertname": "PodCrashLooping",
						"namespace": "monitoring",
						"severity":  "critical",
					},
					"annotations": map[string]string{
						"summary": "Pod is crash looping",
					},
					"generatorURL": "http://vm-grafana.nuage-monitoring-stack.svc/explore",
				},
			},
		}
	}

	st, _, raw := doJSONReq(t, http.MethodPost, base+"/webhook/alertmanager", nil, nil, alertBody("firing"))
	results["webhook_alert_firing_resp"] = toSnapshot(st, raw)
	results["webhook_alert_firing_sse"] = sse.next(t)
	results["webhook_alert_firing_push"] = env.pushSpy.drain()

	// 2. Alertmanager repeat firing (updates root)
	st, _, raw = doJSONReq(t, http.MethodPost, base+"/webhook/alertmanager", nil, nil, alertBody("firing"))
	results["webhook_alert_repeat_resp"] = toSnapshot(st, raw)
	results["webhook_alert_repeat_sse"] = sse.next(t)
	results["webhook_alert_repeat_push"] = env.pushSpy.drain()

	// 3. Alertmanager resolved (reply + root update, no push)
	st, _, raw = doJSONReq(t, http.MethodPost, base+"/webhook/alertmanager", nil, nil, alertBody("resolved"))
	results["webhook_alert_resolved_resp"] = toSnapshot(st, raw)
	results["webhook_alert_resolved_sse_reply"] = sse.next(t)
	results["webhook_alert_resolved_sse_root"] = sse.next(t)
	results["webhook_alert_resolved_push"] = env.pushSpy.drain()

	// 4. Generic webhook (Argo CD inferred from title)
	st, _, raw = doJSONReq(t, http.MethodPost, base+"/webhook/generic", nil, nil, map[string]interface{}{
		"title":       "[ArgoCD] nuage-app synced",
		"body":        "Deployment completed",
		"url":         "https://argocd.example/apps/nuage",
		"direct_open": true,
		"level":       "success",
	})
	results["webhook_generic_resp"] = toSnapshot(st, raw)
	results["webhook_generic_sse"] = sse.next(t)
	results["webhook_generic_push"] = env.pushSpy.drain()

	// 5. Keel webhook
	st, _, raw = doJSONReq(t, http.MethodPost, base+"/webhook/keel", nil, nil, map[string]interface{}{
		"name":    "monitoring-pwa",
		"message": "updated to v1.2.3",
		"type":    "update",
	})
	results["webhook_keel_resp"] = toSnapshot(st, raw)
	results["webhook_keel_sse"] = sse.next(t)
	results["webhook_keel_push"] = env.pushSpy.drain()

	// 6. Alertmanager second alert staying firing so firing filter has an item
	st, _, raw = doJSONReq(t, http.MethodPost, base+"/webhook/alertmanager", nil, nil, map[string]interface{}{
		"status": "firing",
		"alerts": []map[string]interface{}{
			{
				"fingerprint":  "fp-contract-2",
				"status":       "firing",
				"labels":       map[string]string{"alertname": "HighMemory", "severity": "warning"},
				"annotations":  map[string]string{"summary": "Memory above 90%"},
				"generatorURL": "http://vmalert/graph",
			},
		},
	})
	results["webhook_alert2_firing_resp"] = toSnapshot(st, raw)
	results["webhook_alert2_firing_sse"] = sse.next(t)
	results["webhook_alert2_firing_push"] = env.pushSpy.drain()

	// 7. Resulting channel messages & /api/activity filters
	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/channels/C01ALERTMGR/messages", aliceCookie, nil, nil)
	results["alertmanager_messages"] = toSnapshot(st, raw)
	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/channels/C02ARGOCD/messages", aliceCookie, nil, nil)
	results["argocd_messages"] = toSnapshot(st, raw)
	st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/channels/C03KEEL/messages", aliceCookie, nil, nil)
	results["keel_messages"] = toSnapshot(st, raw)

	for _, f := range []string{"", "all", "firing", "resolved", "notice", "bogus"} {
		q := ""
		key := "activity_default"
		if f != "" {
			q = "?filter=" + f
			key = "activity_" + f
		}
		st, _, raw = doJSONReq(t, http.MethodGet, base+"/api/activity"+q, aliceCookie, nil, nil)
		results[key] = toSnapshot(st, raw)
	}

	assertGolden(t, "webhooks_and_activity", results, norm)
}

func TestContractSlackAndSocketMode(t *testing.T) {
	env := newContractEnv(t)
	base := env.server.URL
	u, _ := url.Parse(base)
	norm := newNormalizer(u.Host)

	loginUser := func(pass string) *http.Cookie {
		req, _ := http.NewRequest(http.MethodPost, base+"/api/auth/login", strings.NewReader(`{"passphrase":"`+pass+`"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}
		resp.Body.Close()
		return resp.Cookies()[0]
	}

	aliceCookie := loginUser("alice-secret")
	bobCookie := loginUser("bob-secret")

	aliceSSE := startSSE(t, base, aliceCookie)
	bobSSE := startSSE(t, base, bobCookie)
	_ = aliceSSE.next(t) // initial bot_status
	_ = bobSSE.next(t)   // initial bot_status

	callSlack := func(method, token string, body interface{}) httpSnapshot {
		headers := map[string]string{}
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		}
		st, _, raw := doJSONReq(t, http.MethodPost, base+"/slack/api/"+method, nil, headers, body)
		return toSnapshot(st, raw)
	}

	results := map[string]interface{}{}

	// 1. Auth errors & unknown_method
	results["invalid_auth_bot"] = callSlack("auth.test", "xoxb-wrong", map[string]interface{}{})
	results["invalid_auth_connections_open"] = callSlack("apps.connections.open", env.cfg.SlackBotToken, map[string]interface{}{})
	results["unknown_method"] = callSlack("emoji.list", env.cfg.SlackBotToken, map[string]interface{}{})

	// 2. apps.connections.open & Socket Mode connect
	openSnap := callSlack("apps.connections.open", env.cfg.SlackAppToken, map[string]interface{}{})
	results["apps_connections_open"] = openSnap

	wsURL := openSnap.Body.(map[string]interface{})["url"].(string)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial failed: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })

	var helloFrame map[string]interface{}
	if err := ws.ReadJSON(&helloFrame); err != nil {
		t.Fatalf("ws read hello failed: %v", err)
	}
	results["socket_hello"] = helloFrame
	results["sse_bot_online_alice"] = aliceSSE.next(t)
	results["sse_bot_online_bob"] = bobSSE.next(t)

	// 3. auth.test, conversations.list/info, users.info
	results["auth_test"] = callSlack("auth.test", env.cfg.SlackBotToken, map[string]interface{}{})

	listForm := url.Values{}
	listForm.Set("types", "public_channel,im")
	results["conversations_list"] = callSlack("conversations.list", env.cfg.SlackBotToken, listForm)
	results["conversations_info"] = callSlack("conversations.info", env.cfg.SlackBotToken, map[string]string{"channel": "C01ALERTMGR"})
	results["users_info_human"] = callSlack("users.info", env.cfg.SlackBotToken, map[string]string{"user": "U01ALICE"})
	results["users_info_bot"] = callSlack("users.info", env.cfg.SlackBotToken, map[string]string{"user": "U01BOT"})
	results["users_info_system"] = callSlack("users.info", env.cfg.SlackBotToken, map[string]string{"user": "U00SYSTEM"})
	results["users_info_not_found"] = callSlack("users.info", env.cfg.SlackBotToken, map[string]string{"user": "U99MISSING"})

	// 4. Human posts @bot mention in C05GENERAL -> SSE + Socket Mode message & app_mention
	st, _, raw := doJSONReq(t, http.MethodPost, base+"/api/channels/C05GENERAL/messages", aliceCookie, nil, map[string]string{
		"text": "@bot status please",
	})
	results["human_mention_post"] = toSnapshot(st, raw)
	results["human_mention_sse_alice"] = aliceSSE.next(t)
	results["human_mention_sse_bob"] = bobSSE.next(t)

	var wsEvents []map[string]interface{}
	for i := 0; i < 2; i++ {
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		var envMsg map[string]interface{}
		if err := ws.ReadJSON(&envMsg); err != nil {
			t.Fatalf("ws read event %d failed: %v", i, err)
		}
		wsEvents = append(wsEvents, envMsg)
		_ = ws.WriteJSON(map[string]string{"envelope_id": envMsg["envelope_id"].(string)})
	}
	results["socket_human_mention_envelopes"] = wsEvents

	// 5. Bot chat.postMessage (root with blocks + reply + DM) and chat.update
	postSnap := callSlack("chat.postMessage", env.cfg.SlackBotToken, map[string]interface{}{
		"channel": "C05GENERAL",
		"text":    "bot reply text",
		"blocks": []map[string]interface{}{
			{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": "*bot reply text*"}},
		},
	})
	results["chat_postMessage"] = postSnap
	botMsgTS := postSnap.Body.(map[string]interface{})["ts"].(string)
	results["chat_postMessage_sse_alice"] = aliceSSE.next(t)
	results["chat_postMessage_sse_bob"] = bobSSE.next(t)
	results["chat_postMessage_push"] = env.pushSpy.drain()

	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var botPostEnv map[string]interface{}
	if err := ws.ReadJSON(&botPostEnv); err != nil {
		t.Fatalf("ws read bot post failed: %v", err)
	}
	results["socket_bot_post_envelope"] = botPostEnv

	replySnap := callSlack("chat.postMessage", env.cfg.SlackBotToken, map[string]interface{}{
		"channel":   "C05GENERAL",
		"thread_ts": botMsgTS,
		"text":      "bot thread detail",
	})
	results["chat_postMessage_reply"] = replySnap
	_ = aliceSSE.next(t)
	_ = bobSSE.next(t)
	results["chat_postMessage_reply_push"] = env.pushSpy.drain()

	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var botReplyEnv map[string]interface{}
	if err := ws.ReadJSON(&botReplyEnv); err != nil {
		t.Fatalf("ws read bot reply failed: %v", err)
	}
	results["socket_bot_reply_envelope"] = botReplyEnv

	// DM post to Alice: only Alice's SSE receives it, only Alice receives Push
	dmSnap := callSlack("chat.postMessage", env.cfg.SlackBotToken, map[string]interface{}{
		"channel": "D01ALICE",
		"text":    "private for alice",
	})
	results["chat_postMessage_dm"] = dmSnap
	results["chat_postMessage_dm_sse_alice"] = aliceSSE.next(t)
	bobSSE.expectNone(t, 100*time.Millisecond)
	results["chat_postMessage_dm_push"] = env.pushSpy.drain()

	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var dmEnv map[string]interface{}
	if err := ws.ReadJSON(&dmEnv); err != nil {
		t.Fatalf("ws read dm failed: %v", err)
	}
	results["socket_dm_envelope"] = dmEnv

	// chat.update
	updSnap := callSlack("chat.update", env.cfg.SlackBotToken, map[string]interface{}{
		"channel": "C05GENERAL",
		"ts":      botMsgTS,
		"text":    "bot updated text",
	})
	results["chat_update"] = updSnap
	results["chat_update_sse_alice"] = aliceSSE.next(t)
	results["chat_update_sse_bob"] = bobSSE.next(t)

	// 6. conversations.history & conversations.replies
	results["conversations_history"] = callSlack("conversations.history", env.cfg.SlackBotToken, map[string]interface{}{
		"channel": "C05GENERAL",
	})
	results["conversations_replies"] = callSlack("conversations.replies", env.cfg.SlackBotToken, map[string]interface{}{
		"channel": "C05GENERAL",
		"ts":      botMsgTS,
	})

	assertGolden(t, "slack_and_socket", results, norm)
}
