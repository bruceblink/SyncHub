import { useState } from "react";
import { LogIn } from "lucide-react";
import {
  errorMessage,
  request,
  tokenKey,
  userKey,
  type AuthResponse,
  type User,
} from "./api";

export function Auth({
  onAuthenticated,
}: {
  onAuthenticated: (token: string, user: User) => void;
}) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const data = await request<AuthResponse>(`/auth/${mode}`, {
        method: "POST",
        body: { email, password },
      });
      localStorage.setItem(tokenKey, data.tokens.access_token);
      localStorage.setItem(userKey, JSON.stringify(data.user));
      onAuthenticated(data.tokens.access_token, data.user);
    } catch (error) {
      setError(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="auth-page">
      <section className="auth-panel">
        <div className="brand-mark">S</div>
        <p className="eyebrow">SYNCHUB</p>
        <h1>{mode === "login" ? "访问你的云端文件" : "创建 SyncHub 账户"}</h1>
        <form onSubmit={submit}>
          <label>
            邮箱
            <input type="email" value={email} onChange={(event) => setEmail(event.target.value)} required autoComplete="email" />
          </label>
          <label>
            密码
            <input type="password" value={password} onChange={(event) => setPassword(event.target.value)} required minLength={8} autoComplete={mode === "login" ? "current-password" : "new-password"} />
          </label>
          {error && <p className="form-error">{error}</p>}
          <button className="primary wide" disabled={busy}>
            <LogIn size={18} />
            {busy ? "正在处理..." : mode === "login" ? "登录" : "注册并登录"}
          </button>
        </form>
        <button className="text-button" onClick={() => { setMode(mode === "login" ? "register" : "login"); setError(""); }}>
          {mode === "login" ? "没有账户？创建一个" : "已有账户？登录"}
        </button>
      </section>
    </main>
  );
}
