'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Activity, ArrowRightLeft, HardDrive, RefreshCw, Server, ShieldAlert } from 'lucide-react';
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart';
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';

type Node = { id: string; name: string };
type Metric = { link_id: string; link_name: string; target: string; address: string; protocol: string; ping?: { loss_pct: number; avg_ms: number }; tcp?: { success_pct: number; avg_ms: number }; tailscale?: { path: string; online: boolean }; rx_delta?: number; tx_delta?: number };
type System = { version?: string; build_version?: string; load_1m: number; memory_pct: number; disk_pct: number; nic_rx_delta: number; nic_tx_delta: number };
type LiveNode = { id: string; name: string; ts: number; system: System };
type LiveLink = { node: string; ts: number; metric: Metric; bad: boolean };
type Snapshot = { nodes: LiveNode[]; links: LiveLink[]; storage_bytes: number; free_bytes: number };
type History = { node: string; ts: number; bad: boolean; metric: Metric };
type Hourly = { bucket: number; node: string; link: string; count: number; avg_ms: number; max_ms: number; max_loss: number; bad_count: number; rx_bytes: number; tx_bytes: number };
type HostHourly = { bucket: number; node: string; count: number; rx_bytes: number; tx_bytes: number };
type Incident = { node: string; link: string; link_name: string; start_ts: number; end_ts: number; samples: number; cause: string; confidence: string; evidence: string[] };
type MTR = { node: string; link: string; ts: number; mtr: { reason: string; target: string; hops: Array<{ count: number; host: string; 'Loss%': number; Avg: number }> } };
type Version = { hub: string; frontend: string; build: string };

const when = (ts: number) => new Date(ts * 1000).toLocaleString('zh-CN', { hour12: false });
const fmt = (v?: number, unit = '') => v === undefined || !Number.isFinite(v) ? '—' : `${v.toFixed(1)}${unit}`;
const bytes = (v: number) => v >= 1073741824 ? `${(v / 1073741824).toFixed(2)} GiB` : v >= 1048576 ? `${(v / 1048576).toFixed(1)} MiB` : `${(v / 1024).toFixed(0)} KiB`;
const isFresh = (ts: number) => Date.now() / 1000 - ts < 180;
const linkQuality = (l: LiveLink) => !isFresh(l.ts) ? '中断' : l.bad ? '异常' : '正常';
const metricValue = (m: Metric) => m.tcp ? `${m.tcp.success_pct}%` : m.ping ? `${100 - m.ping.loss_pct}%` : '—';
const version = (v?: string) => v || 'legacy · 请更新新版';
const frontendBuild = process.env.NEXT_PUBLIC_CHAINWATCH_BUILD_VERSION || 'dev';

function Stat({ icon: Icon, label, value, note }: { icon: typeof Server; label: string; value: string; note: string }) {
  return <Card><CardHeader><CardDescription>{label}</CardDescription><CardAction><Icon aria-hidden="true" /></CardAction></CardHeader><CardContent><div className="text-3xl font-semibold tabular-nums">{value}</div><p className="mt-1 text-xs text-muted-foreground">{note}</p></CardContent></Card>;
}

export default function Dashboard() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [history, setHistory] = useState<History[]>([]);
  const [hourly, setHourly] = useState<Hourly[]>([]);
  const [hostHourly, setHostHourly] = useState<HostHourly[]>([]);
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [mtrs, setMtrs] = useState<MTR[]>([]);
  const [versions, setVersions] = useState<Version | null>(null);
  const [selected, setSelected] = useState('');
  const [error, setError] = useState('');
  const [updated, setUpdated] = useState(0);

  const refresh = useCallback(async () => {
    try {
      const paths = ['/api/topology', '/api/snapshot', '/api/history?hours=6', '/api/hourly?days=30', '/api/host-hourly?days=1', '/api/diagnosis', '/api/mtr', '/api/version'];
      const responses = await Promise.all(paths.map((path) => fetch(path, { cache: 'no-store' })));
      if (responses.some((r) => r.status === 401)) { window.location.assign('/login'); return; }
      if (responses.some((r) => !r.ok)) throw new Error('数据获取失败');
      const [n, s, h, hh, hosts, d, m, v] = await Promise.all(responses.map((r) => r.json()));
      setNodes(n); setSnapshot(s); setHistory(h); setHourly(hh); setHostHourly(hosts); setIncidents(d); setMtrs(m); setVersions(v); setUpdated(Date.now()); setError('');
    } catch (e) { setError(e instanceof Error ? e.message : '连接失败'); }
  }, []);
  useEffect(() => { void refresh(); const timer = setInterval(() => { void refresh(); }, 30000); return () => clearInterval(timer); }, [refresh]);
  const name = (id: string) => nodes.find((n) => n.id === id)?.name || id;
  const links = snapshot?.links || [];
  const incident = incidents[0];
  const activeLink = selected || (incident ? `${incident.node}/${incident.link}` : '') || (links[0] ? `${links[0].node}/${links[0].metric.link_id}` : '');
  const active = links.find((l) => `${l.node}/${l.metric.link_id}` === activeLink);
  const chart = useMemo(() => history.filter((h) => `${h.node}/${h.metric.link_id}` === activeLink).sort((a, b) => a.ts - b.ts).map((h) => ({ time: new Date(h.ts * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }), success: h.metric.tcp?.success_pct ?? (h.metric.ping ? 100 - h.metric.ping.loss_pct : 100), rtt: h.metric.ping?.avg_ms || h.metric.tcp?.avg_ms || 0 })), [history, activeLink]);
  const hostTraffic = hostHourly.reduce((sum, h) => sum + h.rx_bytes + h.tx_bytes, 0);
  const selectedTraffic = hourly.filter((h) => `${h.node}/${h.link}` === activeLink).reduce((sum, h) => sum + h.rx_bytes + h.tx_bytes, 0);
  const healthy = links.filter((l) => linkQuality(l) === '正常').length;
  const recentMtr = incident ? mtrs.find((m) => m.node === incident.node && m.link === incident.link && Math.abs(m.ts - incident.end_ts) <= 600) : undefined;
  const comparator = incident ? links.find((l) => l.node === incident.node && l.metric.target === links.find((x) => x.node === incident.node && x.metric.link_id === incident.link)?.metric.target && l.metric.link_id !== incident.link && Boolean(l.metric.tailscale) !== Boolean(links.find((x) => x.node === incident.node && x.metric.link_id === incident.link)?.metric.tailscale)) : undefined;

  async function logout() { await fetch('/api/logout', { method: 'POST' }); window.location.assign('/login'); }

  return <main className="min-h-screen bg-background text-foreground"><div className="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-8 md:px-8">
    <header className="flex flex-wrap items-center justify-between gap-4"><div><div className="flex items-center gap-3"><Activity aria-hidden="true" className="text-primary" /><h1 className="text-2xl font-semibold tracking-tight">Chainwatch</h1><Badge variant="secondary">网络诊断</Badge></div><p className="mt-1 text-sm text-muted-foreground">{updated ? `最近更新 ${new Date(updated).toLocaleTimeString('zh-CN')}` : '等待数据'}</p></div><div className="flex gap-2"><Button variant="outline" size="sm" onClick={() => void refresh()}><RefreshCw data-icon="inline-start" />刷新</Button><Button variant="ghost" size="sm" onClick={() => void logout()}>退出</Button></div></header>
    {error && <Alert variant="destructive"><ShieldAlert aria-hidden="true" /><AlertTitle>监控数据暂不可用</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
    {!snapshot ? <Skeleton className="h-32 w-full" /> : <section className="grid grid-cols-2 gap-3 lg:grid-cols-4"><Stat icon={Server} label="在线主机" value={`${snapshot.nodes.filter((n) => isFresh(n.ts)).length}/${snapshot.nodes.length}`} note="最近 3 分钟上报" /><Stat icon={ArrowRightLeft} label="健康链路" value={`${healthy}/${links.length}`} note="按最新一次采样" /><Stat icon={Activity} label="24 小时主机流量" value={bytes(hostTraffic)} note="两端网卡收发总量" /><Stat icon={HardDrive} label="监控存储" value={bytes(snapshot.storage_bytes)} note={`可用 ${bytes(snapshot.free_bytes)}`} /></section>}

    <section className="grid gap-4 lg:grid-cols-[1.2fr_1fr]"><Card><CardHeader><CardTitle>故障定位</CardTitle><CardDescription>最近一次连续异常</CardDescription>{incident && <CardAction><Badge variant={incident.confidence === '高' ? 'destructive' : 'secondary'}>{incident.confidence}可信度</Badge></CardAction>}</CardHeader><CardContent>{incident ? <div className="flex flex-col gap-4"><div><div className="text-xl font-semibold">{incident.cause}</div><p className="text-sm text-muted-foreground">{name(incident.node)} → {incident.link_name} · {when(incident.start_ts)}–{when(incident.end_ts)} · {incident.samples} 次异常</p></div><div className="grid grid-cols-2 gap-3"><Stat icon={Activity} label="故障链路·当前" value={metricValue(links.find((l) => l.node === incident.node && l.metric.link_id === incident.link)?.metric || { link_id: '', link_name: '', target: '', address: '', protocol: '' })} note="TCP 成功率 / ICMP 到达率" /><Stat icon={ArrowRightLeft} label="对照链路·当前" value={comparator ? metricValue(comparator.metric) : '—'} note={comparator?.metric.link_name || '未配置同目标对照'} /></div><div className="flex flex-wrap gap-2">{incident.evidence.map((e, i) => <Badge key={i} variant="outline" className="max-w-full whitespace-normal text-left">{e}</Badge>)}</div>{recentMtr && <p className="text-xs text-muted-foreground">MTR 记录 {when(recentMtr.ts)} · {recentMtr.mtr.hops.length} 跳；中间跳不回应不等于转发故障。</p>}</div> : <Empty><EmptyHeader><EmptyTitle>暂无异常</EmptyTitle><EmptyDescription>故障发生后会自动合并同链路异常并展示对照数据。</EmptyDescription></EmptyHeader></Empty>}</CardContent></Card>
    <Card><CardHeader><CardTitle>主机与版本</CardTitle><CardDescription>采样状态及软件版本</CardDescription></CardHeader><CardContent><Table><TableHeader><TableRow><TableHead>主机</TableHead><TableHead>状态</TableHead><TableHead>版本</TableHead><TableHead>负载</TableHead></TableRow></TableHeader><TableBody>{snapshot?.nodes.map((n) => <TableRow key={n.id}><TableCell><div className="font-medium">{n.name}</div><div className="text-xs text-muted-foreground">{when(n.ts)}</div></TableCell><TableCell><Badge variant={isFresh(n.ts) ? 'secondary' : 'destructive'}>{isFresh(n.ts) ? '在线' : '中断'}</Badge></TableCell><TableCell className="font-mono text-xs">{version(n.system.version)}</TableCell><TableCell>{fmt(n.system.load_1m)}</TableCell></TableRow>)}</TableBody></Table><Separator className="my-4" /><div className="grid grid-cols-2 gap-3 text-sm"><div><span className="text-muted-foreground">前端</span><div className="font-mono">{version(versions?.frontend)}</div></div><div><span className="text-muted-foreground">构建</span><div className="truncate font-mono" title={versions?.build}>{version(versions?.build)}</div></div></div></CardContent></Card></section>

    <Card><CardHeader><CardTitle>链路波动</CardTitle><CardDescription>最近 6 小时逐次采样；成功率低于 100% 即有失败</CardDescription><CardAction><NativeSelect size="sm" aria-label="选择链路" value={activeLink} onChange={(e) => setSelected(e.target.value)}>{links.map((l) => <NativeSelectOption key={`${l.node}/${l.metric.link_id}`} value={`${l.node}/${l.metric.link_id}`}>{name(l.node)} → {l.metric.link_name}</NativeSelectOption>)}</NativeSelect></CardAction></CardHeader><CardContent>{chart.length ? <ChartContainer className="h-64 w-full aspect-auto" config={{ success: { label: '成功率', color: 'var(--chart-1)' }, rtt: { label: 'RTT', color: 'var(--chart-2)' } }}><LineChart data={chart}><CartesianGrid vertical={false} /><XAxis dataKey="time" tickLine={false} axisLine={false} minTickGap={28} /><YAxis domain={[0, 100]} unit="%" /><ChartTooltip content={<ChartTooltipContent />} /><Line dataKey="success" name="成功率" stroke="var(--color-success)" dot={false} strokeWidth={2} type="monotone" /></LineChart></ChartContainer> : <Empty><EmptyHeader><EmptyTitle>暂无采样</EmptyTitle></EmptyHeader></Empty>}<Separator className="my-4" /><div className="flex flex-wrap gap-4 text-sm"><span>当前 RTT <strong>{fmt(active?.metric.ping?.avg_ms ?? active?.metric.tcp?.avg_ms, ' ms')}</strong></span><span>当前丢包 <strong>{fmt(active?.metric.ping?.loss_pct, '%')}</strong></span><span>路径 <strong>{active?.metric.tailscale?.path || '公网 / 本机'}</strong></span><span>30 天链路流量 <strong>{bytes(selectedTraffic)}</strong></span></div></CardContent></Card>

    {recentMtr && <Card><CardHeader><CardTitle>故障时 MTR</CardTitle><CardDescription>{when(recentMtr.ts)} · 目标 {recentMtr.mtr.target} · 末端与连续多跳变化更有参考价值</CardDescription></CardHeader><CardContent><Table><TableHeader><TableRow><TableHead>跳</TableHead><TableHead>地址</TableHead><TableHead>丢包</TableHead><TableHead>平均 RTT</TableHead></TableRow></TableHeader><TableBody>{recentMtr.mtr.hops.map((hop, i) => <TableRow key={`${hop.count}-${i}`}><TableCell>{hop.count}</TableCell><TableCell className="font-mono text-xs">{hop.host}</TableCell><TableCell>{fmt(hop['Loss%'], '%')}</TableCell><TableCell>{fmt(hop.Avg, ' ms')}</TableCell></TableRow>)}</TableBody></Table></CardContent></Card>}

    <Card><CardHeader><CardTitle>所有有向链路</CardTitle><CardDescription>公网与 Tailscale 分开展示，方向独立</CardDescription></CardHeader><CardContent><Table><TableHeader><TableRow><TableHead>来源 → 目标</TableHead><TableHead>状态</TableHead><TableHead>TCP 成功率</TableHead><TableHead>丢包</TableHead><TableHead>RTT</TableHead><TableHead>路径</TableHead></TableRow></TableHeader><TableBody>{links.map((l) => <TableRow key={`${l.node}/${l.metric.link_id}`}><TableCell><div className="font-medium">{name(l.node)} → {l.metric.link_name}</div><div className="text-xs text-muted-foreground">{l.metric.address}</div></TableCell><TableCell><Badge variant={linkQuality(l) === '正常' ? 'secondary' : 'destructive'}>{linkQuality(l)}</Badge></TableCell><TableCell className="tabular-nums">{l.metric.tcp ? `${l.metric.tcp.success_pct}%` : '—'}</TableCell><TableCell className="tabular-nums">{fmt(l.metric.ping?.loss_pct, '%')}</TableCell><TableCell className="tabular-nums">{fmt(l.metric.ping?.avg_ms ?? l.metric.tcp?.avg_ms, ' ms')}</TableCell><TableCell>{l.metric.tailscale?.path || '公网 / 本机'}</TableCell></TableRow>)}</TableBody></Table></CardContent></Card>

    <footer className="flex flex-wrap justify-between gap-2 text-xs text-muted-foreground"><span>故障定位为证据推断；三次 TCP / MTR 和五次 ICMP 采样不能单独证明某个运营商节点故障。</span><span>主机 {version(snapshot?.nodes.find((n) => n.id === nodes[0]?.id)?.system.version)} · 从机 {version(snapshot?.nodes.find((n) => n.id !== nodes[0]?.id)?.system.version)} · 前端 {version(versions?.frontend)} · 构建 {version(versions?.build).slice(0, 12)}{frontendBuild !== 'dev' && versions?.build !== frontendBuild ? '（前后端版本不一致）' : ''}</span></footer>
  </div></main>;
}
