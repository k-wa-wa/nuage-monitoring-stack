# pwa-notifier

Receives Alertmanager webhooks and delivers them as Web Push notifications to
a PWA installed on the home screen (iOS Safari 16.4+, Android Chrome, etc.),
without going through a third-party relay like ntfy. Delivery to iOS still
transits Apple's push service — that's unavoidable for any web push — but the
payload, subscriber list, and server are entirely self-hosted.

## Layout

- `cmd/server` — HTTP server: serves the PWA (`web/`) and the API below.
- `cmd/genvapid` — one-off VAPID keypair generator.
- `internal/push` — subscription store + Web Push sending (webpush-go).
- `internal/handler` — HTTP handlers.
- `web/` — the PWA itself (manifest, service worker, subscribe UI).

## Endpoints

- `GET  /health`
- `GET  /api/vapid-public-key` — public key the PWA needs to subscribe.
- `POST /api/subscribe` — store a browser's PushSubscription.
- `POST /api/unsubscribe`
- `POST /api/test-notify` — broadcast an arbitrary `{title, body}` to every
  subscriber, without Alertmanager. Use this to verify the subscribe → push →
  service worker path first.
- `POST /webhook/alertmanager` — Alertmanager `webhook_config` target. If
  `WEBHOOK_TOKEN` is set, requires `Authorization: Bearer <token>`.

## Run locally

```sh
go run ./cmd/genvapid                 # prints VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY
VAPID_PUBLIC_KEY=... VAPID_PRIVATE_KEY=... go run ./cmd/server
```

Open `http://localhost:8080` (Web Push requires either `localhost` or HTTPS —
plain HTTP on any other host will not work, and iOS additionally requires the
page to be added to the home screen before push permission can be granted).

## Secrets

- `local`: `k8s/overlays/local/secrets.yaml` is a plain committed `Secret`
  with a throwaway VAPID keypair — fine for a disposable local/kind cluster,
  not meant to protect anything.
- `prod`: SOPS-encrypted, same pattern as `../pechka`'s prod overlay, decrypted
  at sync time by `argocd-vault-plugin` (`AVP_TYPE=sops`, see
  `manifests/apps/multi-repo-deploy.yaml` in `nuage-cluster`):
  - `k8s/overlays/prod/secrets/prod-secrets.yaml` — flat `KEY: value` file,
    SOPS-encrypted in place. **Currently committed as plaintext** — it must
    be encrypted before this is pushed:
    ```sh
    sops -e -i k8s/overlays/prod/secrets/prod-secrets.yaml
    ```
    (age recipients for this path are already declared in the repo-root
    `.sops.yaml`: `admin_key` + `argocd_key`, matching pechka's.)
  - `k8s/overlays/prod/secrets/secrets.yaml` — the actual `pwa-notifier-secret`
    `Secret` manifest, templated with AVP's `<path:secrets/prod-secrets.yaml#KEY>`
    substitution syntax. Committed as-is; no real values live in this file.

  The VAPID keypair and webhook token already in `prod-secrets.yaml` were
  freshly generated for this — replace them (`go run ./cmd/genvapid` /
  `openssl rand -hex 32`) if you'd rather mint your own before encrypting.

## Deploy

1. Encrypt `k8s/overlays/prod/secrets/prod-secrets.yaml` with `sops` (above),
   then commit and push to `master` — GitHub Actions builds and pushes
   `ghcr.io/k-wa-wa/nuage-monitoring-stack-pwa-notifier`, and Argo CD
   (`nuage-monitoring-stack-app`) auto-syncs `k8s/overlays/prod`.
2. Visit the PWA (`https://notify.cluster.wpc`), tap "通知を有効にする", add
   to the iOS home screen, then tap "テスト通知を送信" to confirm delivery
   before relying on the Alertmanager wiring in
   `k8s/base/helm/prometheus-stack-custom.yaml`.

`WEBHOOK_TOKEN` in `prod-secrets.yaml` protects `/webhook/alertmanager` — the
matching `http_config.authorization.credentials` still needs to be added to
the `webhook_configs` entry in `prometheus-stack-custom.yaml` so Alertmanager
authenticates (not yet wired up).

For local dev without a cluster, `go run ./cmd/server` still works directly
(see above) — the `local` overlay's secret is only relevant when running
inside a local/kind cluster.
