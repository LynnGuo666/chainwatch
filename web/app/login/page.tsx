'use client';

import { FormEvent, useState } from 'react';
import { Activity, LockKeyhole } from 'lucide-react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';

export default function Login() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError('');
    try {
      const response = await fetch('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password }) });
      if (response.status === 204) { window.location.assign('/'); return; }
      setError(response.status === 429 ? '尝试次数过多，请稍后再试。' : '用户名或密码错误。');
    } catch { setError('无法连接监控中心。'); }
    finally { setBusy(false); }
  }
  return <main className="flex min-h-screen items-center justify-center bg-background px-4"><div className="w-full max-w-sm"><div className="mb-6 flex items-center justify-center gap-2"><Activity aria-hidden="true" /><span className="text-xl font-semibold">Chainwatch</span><Badge variant="secondary">v0.2.0</Badge></div><Card><CardHeader><CardTitle>登录监控仪表盘</CardTitle><CardDescription>查看链路、流量与故障证据</CardDescription></CardHeader><CardContent><form id="login-form" onSubmit={submit}><FieldGroup><Field><FieldLabel htmlFor="username">用户名</FieldLabel><Input id="username" autoComplete="username" required maxLength={128} value={username} onChange={(e) => setUsername(e.target.value)} /></Field><Field><FieldLabel htmlFor="password">密码</FieldLabel><Input id="password" type="password" autoComplete="current-password" required maxLength={1024} value={password} onChange={(e) => setPassword(e.target.value)} /></Field></FieldGroup></form>{error && <Alert variant="destructive" className="mt-4"><AlertDescription>{error}</AlertDescription></Alert>}</CardContent><CardFooter><Button type="submit" form="login-form" className="w-full" disabled={busy}><LockKeyhole data-icon="inline-start" />{busy ? '登录中…' : '登录'}</Button></CardFooter></Card><p className="mt-4 text-center text-xs text-muted-foreground">仅通过 HTTPS 提供登录。会话 12 小时后过期。</p></div></main>;
}
