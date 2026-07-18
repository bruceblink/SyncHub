import { useCallback, useEffect, useState } from "react";
import { Check, Copy, KeyRound, Plus, RefreshCw, Trash2 } from "lucide-react";
import {
  errorMessage,
  formatDate,
  type APIKey,
  type BillingOverview,
  type RequestOptions,
} from "./api";

type AccountRequest = <T>(path: string, options?: Omit<RequestOptions, "token">) => Promise<T>;

export function Account({ request, onError }: { request: AccountRequest; onError: (message: string) => void }) {
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [billing, setBilling] = useState<BillingOverview | null>(null);
  const [name, setName] = useState("");
  const [application, setApplication] = useState<APIKey["application"]>("kvideo");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const [keyData, billingData] = await Promise.all([
        request<{ items: APIKey[] }>("/account/api-keys"),
        request<BillingOverview>("/account/billing"),
      ]);
      setKeys(keyData.items);
      setBilling(billingData);
    } catch (error) {
      onError(errorMessage(error));
    }
  }, [onError, request]);

  useEffect(() => { void load(); }, [load]);

  async function createKey(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    try {
      const data = await request<{ api_key: APIKey; secret: string }>("/account/api-keys", {
        method: "POST",
        body: { name, application },
      });
      setSecret(data.secret);
      setName("");
      await load();
    } catch (error) {
      onError(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  async function revoke(id: string) {
    if (!window.confirm("撤销后使用此 Key 的应用会立即停止同步，确认继续？")) return;
    try {
      await request(`/account/api-keys/${id}`, { method: "DELETE" });
      await load();
    } catch (error) { onError(errorMessage(error)); }
  }

  async function updateRenewal(cancel: boolean) {
    setBusy(true);
    try {
      await request(`/account/subscription/${cancel ? "cancel" : "resume"}`, { method: "POST" });
      await load();
    } catch (error) { onError(errorMessage(error)); }
    finally { setBusy(false); }
  }

  const subscription = billing?.subscription;
  return (
    <div className="account-page">
      <header className="section-header">
        <div><p className="eyebrow">账户服务</p><h1>API Key 与订阅</h1></div>
        <button className="icon-button" title="刷新" onClick={() => void load()}><RefreshCw size={18} /></button>
      </header>

      <section className="account-summary">
        <div><span>当前套餐</span><strong>{subscription?.plan === "pro" ? "Pro" : "Free"}</strong></div>
        <div><span>订阅状态</span><strong>{subscription?.status ?? "--"}</strong></div>
        <div><span>周期费用</span><strong>{subscription ? `${(subscription.unit_amount / 100).toFixed(2)} ${subscription.currency}` : "--"}</strong></div>
        <div><span>周期结束</span><strong>{formatDate(subscription?.current_period_end ?? subscription?.expires_at)}</strong></div>
      </section>
      {subscription?.plan === "pro" && (
        <div className="renewal-row">
          <span>{subscription.cancel_at_period_end ? "已设置在当前周期结束后取消" : "订阅将自动续订"}</span>
          <button className="secondary" disabled={busy} onClick={() => void updateRenewal(!subscription.cancel_at_period_end)}>
            {subscription.cancel_at_period_end ? "恢复续订" : "取消续订"}
          </button>
        </div>
      )}

      <section className="key-section">
        <div className="section-title"><div><h2>应用 API Key</h2><p>每个 Key 仅能访问绑定应用的同步集合。</p></div></div>
        <form className="key-form" onSubmit={createKey}>
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Key 名称，例如客厅电视" required maxLength={100} />
          <select value={application} onChange={(event) => setApplication(event.target.value as APIKey["application"])}>
            <option value="kvideo">KVideo</option><option value="latestnews">LatestNews</option>
          </select>
          <button className="primary" disabled={busy}><Plus size={17} />创建 Key</button>
        </form>
        {secret && (
          <div className="secret-notice"><KeyRound size={20} /><div><strong>请立即保存，此 Key 只显示一次</strong><code>{secret}</code></div><button className="icon-button" title="复制 Key" onClick={() => void navigator.clipboard.writeText(secret).then(() => setSecret(""))}><Copy size={17} /></button></div>
        )}
        <div className="key-list">
          {keys.length === 0 ? <div className="compact-empty">还没有 API Key</div> : keys.map((key) => (
            <div className={`key-row ${key.revoked_at ? "revoked" : ""}`} key={key.id}>
              <div><strong>{key.name}</strong><span>{key.application} · {key.key_prefix}...</span></div>
              <div><span>最近使用</span><strong>{formatDate(key.last_used_at)}</strong></div>
              <div><span>创建时间</span><strong>{formatDate(key.created_at)}</strong></div>
              {key.revoked_at ? <span className="status-pill">已撤销</span> : <button className="icon-button danger" title="撤销 Key" onClick={() => void revoke(key.id)}><Trash2 size={17} /></button>}
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
