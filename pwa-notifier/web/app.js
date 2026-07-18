const statusEl = document.getElementById("status");
const subscribeBtn = document.getElementById("subscribe");
const testNotifyBtn = document.getElementById("test-notify");

function log(msg) {
  statusEl.textContent = `${new Date().toLocaleTimeString()}  ${msg}\n${statusEl.textContent}`;
}

function urlBase64ToUint8Array(base64String) {
  const padding = "=".repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, "+").replace(/_/g, "/");
  const rawData = atob(base64);
  return Uint8Array.from([...rawData].map((c) => c.charCodeAt(0)));
}

async function init() {
  if (!("serviceWorker" in navigator) || !("PushManager" in window)) {
    log("このブラウザは Web Push に対応していません(iOSはホーム画面に追加したPWAでのみ対応)");
    subscribeBtn.disabled = true;
    return;
  }

  const registration = await navigator.serviceWorker.register("/sw.js");
  log("service worker registered");

  const existing = await registration.pushManager.getSubscription();
  if (existing) {
    log("既に購読済みです");
    testNotifyBtn.disabled = false;
  }
}

async function subscribe() {
  subscribeBtn.disabled = true;
  try {
    const permission = await Notification.requestPermission();
    if (permission !== "granted") {
      log(`通知が許可されませんでした: ${permission}`);
      return;
    }

    const registration = await navigator.serviceWorker.ready;
    const { publicKey } = await fetch("/api/vapid-public-key").then((r) => r.json());

    const subscription = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(publicKey),
    });

    const res = await fetch("/api/subscribe", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(subscription.toJSON()),
    });

    if (!res.ok) {
      throw new Error(`subscribe failed: ${res.status}`);
    }

    log("購読が完了しました");
    testNotifyBtn.disabled = false;
  } catch (err) {
    log(`エラー: ${err.message}`);
  } finally {
    subscribeBtn.disabled = false;
  }
}

async function sendTestNotify() {
  testNotifyBtn.disabled = true;
  try {
    const res = await fetch("/api/test-notify", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: "テスト通知",
        body: "pwa-notifier からのテスト通知です",
      }),
    });
    log(res.ok ? "テスト通知を送信しました" : `送信失敗: ${res.status}`);
  } catch (err) {
    log(`エラー: ${err.message}`);
  } finally {
    testNotifyBtn.disabled = false;
  }
}

subscribeBtn.addEventListener("click", subscribe);
testNotifyBtn.addEventListener("click", sendTestNotify);

init();
