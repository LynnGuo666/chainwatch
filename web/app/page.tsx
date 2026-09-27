'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Activity, ArrowRightLeft, HardDrive, Pencil, RefreshCw, Server, ShieldAlert } from 'lucide-react';
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty';
import { Input } from '@/components/ui/input';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { Progress } from '@/components/ui/progress';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import { toast } from '@/components/ui/toast';

type Node = { id: string; name: string; note: string; public_ip?: string; tailscale_ip?: string };
type IPInfo = { ip: string; prefix?: string; asn?: string; holder?: string; error?: string };
type Metric = { link_id: string; link_name: string; target: string; address: string; port?: number; protocol: string; ping?: { loss_pct: number; avg_ms: number }; tcp?: { success_pct: number; avg_ms: number }; tailscale?: { path: string; endpoint?: string; online: boolean }; rx_delta?: number; tx_delta?: number };
type System = { version?: string; build_version?: string; load_1m: number; memory_pct: number; disk_pct: number; nic_rx_delta: number; nic_tx_delta: number };
type LiveNode = { id: string; name: string; ts: number; system: System };
type LiveLink = { node: string; ts: number; metric: Metric; bad: boolean };
type Snapshot = { nodes: LiveNode[]; links: LiveLink[]; storage_bytes: number; free_bytes: number };
type History = { node: string; ts: number; bad: boolean; metric: Metric };
type Hourly = { bucket: number; node: string; link: string; count: number; avg_ms: number; max_ms: number; max_loss: number; bad_count: number; rx_bytes: number; tx_bytes: number };
type Incident = { node: string; link: string; link_name: string; start_ts: number; end_ts: number; samples: number; cause: string; confidence: string; evidence: string[] };
type MTR = { node: string; link: string; ts: number; mtr: { reason: string; target: string; hops: Array<{ count: number; host: string; 'Loss%': number; Avg: number }> } };
type Version = { hub: string; frontend: string; build: string };
type TrafficStatus = { id: string; quota_gb: number; reset_day: number; billing_mode: 'sum' | 'rx' | 'tx' | 'max'; started_at: number; cycle_start: number; cycle_end: number; observed_rx: number; observed_tx: number; observed_billed: number; used_bytes: number; remaining_bytes: number; estimated_monitor_bytes: number; other_bytes: number; calibrated: boolean; partial: boolean };

const when = (ts: number) => new Date(ts * 1000).toLocaleString('zh-CN', { hour12: false });
const fmt = (v?: number, unit = '') => v === undefined || !Number.isFinite(v) ? '—' : `${v.toFixed(1)}${unit}`;
const bytes = (v: number) => v >= 1073741824 ? `${(v / 1073741824).toFixed(2)} GiB` : v >= 1048576 ? `${(v / 1048576).toFixed(1)} MiB` : `${(v / 1024).toFixed(0)} KiB`;
const gb = (v: number) => `${(v / 1_000_000_000).toFixed(2)} GB`;
const billingNames = { sum: '上传 + 下载', rx: '仅下载', tx: '仅上传', max: '上传/下载较大值' };
const isFresh = (ts: number) => Date.now() / 1000 - ts < 180;
const linkQuality = (l: LiveLink) => !isFresh(l.ts) ? '中断' : l.bad ? '异常' : '正常';
const metricValue = (m: Metric) => m.tcp ? `${m.tcp.success_pct}%` : m.ping ? `${100 - m.ping.loss_pct}%` : '—';
const version = (v?: string) => v || 'legacy · 请更新新版';
const frontendBuild = process.env.NEXT_PUBLIC_CHAINWATCH_BUILD_VERSION || 'dev';
const isTailIP = (ip: string) => { const parts = ip.split('.').map(Number); return parts.length === 4 && parts[0] === 100 && parts[1] >= 64 && parts[1] <= 127; };
const publicIP = (ip: string) => /^\d{1,3}(\.\d{1,3}){3}$/.test(ip) && !ip.startsWith('127.') && !ip.startsWith('10.') && !ip.startsWith('192.168.') && !ip.startsWith('169.254.') && !isTailIP(ip);
const ipSummary = (info?: IPInfo) => info?.asn ? `${info.prefix || ''} · ${info.asn}${info.holder ? ` · ${info.holder}` : ''}` : info?.error || '查询中…';

function route(l: LiveLink, nodes: Node[]) {
  const source = nodes.find((n) => n.id === l.node);
  const destination = nodes.find((n) => n.id === l.metric.target);
  const local = l.metric.address === '127.0.0.1' || l.metric.address === '::1';
  const tail = Boolean(l.metric.tailscale) || isTailIP(l.metric.address);
  const from = local ? '本机' : tail ? source?.tailscale_ip || source?.public_ip || l.node : source?.public_ip || source?.tailscale_ip || l.node;
  const to = `${l.metric.address}:${l.metric.port || 443}`;
  return { from, to, type: local ? '本机端口' : tail ? 'Tailscale' : '公网对照', description: local ? `本机 → ${to}（Xray 代理服务的本地入口）` : `${from} → ${to}`, destination: destination?.name || l.metric.target };
}

function Stat({ icon: Icon, label, value, note }: { icon: typeof Server; label: string; value: string; note: string }) {
  return <Card><CardHeader><CardDescription>{label}</CardDescription><CardAction><Icon aria-hidden="true" /></CardAction></CardHeader><CardContent><div className="text-3xl font-semibold tabular-nums">{value}</div><p className="mt-1 text-xs text-muted-foreground">{note}</p></CardContent></Card>;
}

function TrafficCard({ item, node, onEdit }: { item: TrafficStatus; node?: Node; onEdit: (item: TrafficStatus) => void }) {
  const quotaBytes = item.quota_gb * 1_000_000_000;
  const percentage = quotaBytes > 0 ? Math.min(100, item.used_bytes / quotaBytes * 100) : 0;
  return <Card><CardHeader><CardTitle className="font-mono">{node?.public_ip || node?.tailscale_ip || item.id}</CardTitle><CardDescription>{node?.name || item.id} · {billingNames[item.billing_mode]} · 每月 {item.reset_day} 日 00:00 UTC 结算</CardDescription><CardAction><Button variant="outline" size="sm" onClick={() => onEdit(item)}><Pencil data-icon="inline-start" />配置 / 校准</Button></CardAction></CardHeader><CardContent className="space-y-4"><div className="flex flex-wrap gap-2"><Badge variant={item.calibrated ? 'secondary' : 'outline'}>{item.calibrated ? '已校准本月用量' : '仅统计启用后的流量'}</Badge><Badge variant="outline">月额度 {item.quota_gb > 0 ? `${item.quota_gb} GB` : '未设置'}</Badge></div><div className="grid grid-cols-2 gap-3 sm:grid-cols-4"><div><div className="text-xs text-muted-foreground">本月已用</div><div className="text-xl font-semibold tabular-nums">{gb(item.used_bytes)}</div></div><div><div className="text-xs text-muted-foreground">剩余</div><div className="text-xl font-semibold tabular-nums">{item.quota_gb > 0 ? gb(item.remaining_bytes) : '—'}</div></div><div><div className="text-xs text-muted-foreground">监控探测估算</div><div className="text-xl font-semibold tabular-nums">{gb(item.estimated_monitor_bytes)}</div></div><div><div className="text-xs text-muted-foreground">其他 / 未归因</div><div className="text-xl font-semibold tabular-nums">≈{gb(item.other_bytes)}</div></div></div>{item.quota_gb > 0 && <Progress value={percentage} aria-label={`${node?.name || item.id} 本月流量已使用 ${percentage.toFixed(1)}%`} />}<div className="space-y-1 text-xs text-muted-foreground"><p>周期 {when(item.cycle_start)} – {when(item.cycle_end)}；开始采集 {when(item.started_at)}。</p><p>本机网卡已观测：下载 {gb(item.observed_rx)} · 上传 {gb(item.observed_tx)}。{item.partial && !item.calibrated ? '本周期开始前没有回填，当前已用仅含启用后的数据。' : ''}</p><p>“监控探测”按 TCP、ICMP、MTR 次数和签名上报估算；看板访问与代理业务计入“其他 / 未归因”。服务商计费可能与网卡计数不同，可用账单值校准。</p></div></CardContent></Card>;
}

export default function Dashboard() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [history, setHistory] = useState<History[]>([]);
  const [hourly, setHourly] = useState<Hourly[]>([]);
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [mtrs, setMtrs] = useState<MTR[]>([]);
  const [versions, setVersions] = useState<Version | null>(null);
  const [traffic, setTraffic] = useState<TrafficStatus[]>([]);
  const [activeTab, setActiveTab] = useState('overview');
  const [selected, setSelected] = useState('');
  const [error, setError] = useState('');
  const [updated, setUpdated] = useState(0);
  const [ipInfo, setIPInfo] = useState<Record<string, IPInfo>>({});
  const [editing, setEditing] = useState<Node | null>(null);
  const [editName, setEditName] = useState('');
  const [editNote, setEditNote] = useState('');
  const [trafficEditing, setTrafficEditing] = useState<TrafficStatus | null>(null);
  const [quotaInput, setQuotaInput] = useState('');
  const [resetInput, setResetInput] = useState('1');
  const [modeInput, setModeInput] = useState<TrafficStatus['billing_mode']>('sum');
  const [calibrationInput, setCalibrationInput] = useState('');
  const requestedIPs = useRef(new Set<string>());
  const detailFetchedAt = useRef({ links: 0, mtr: 0 });

  const refresh = useCallback(async (force = false) => {
    try {
      const paths = ['/api/topology', '/api/snapshot', '/api/diagnosis', '/api/version', '/api/traffic'];
      const fetchLinks = activeTab === 'links' && (force || Date.now() - detailFetchedAt.current.links >= 300000);
      const fetchMTR = activeTab === 'mtr' && (force || Date.now() - detailFetchedAt.current.mtr >= 300000);
      if (fetchLinks) paths.push('/api/history?hours=6', '/api/hourly?days=30');
      if (fetchMTR) paths.push('/api/mtr');
      const responses = await Promise.all(paths.map((path) => fetch(path, { cache: 'no-store' })));
      if (responses.some((r) => r.status === 401)) { window.location.assign('/login'); return; }
      if (responses.some((r) => !r.ok)) throw new Error('数据获取失败');
      const [n, s, d, v, usage, ...detail] = await Promise.all(responses.map((r) => r.json()));
      setNodes(n); setSnapshot(s); setIncidents(d); setVersions(v); setTraffic(usage);
      if (fetchLinks) { setHistory(detail[0]); setHourly(detail[1]); detailFetchedAt.current.links = Date.now(); }
      if (fetchMTR) { setMtrs(detail[0]); detailFetchedAt.current.mtr = Date.now(); }
      setUpdated(Date.now()); setError('');
    } catch (e) { setError(e instanceof Error ? e.message : '连接失败'); }
  }, [activeTab]);
  useEffect(() => {
    const visibleRefresh = () => { if (document.visibilityState === 'visible') void refresh(); };
    visibleRefresh();
    const timer = setInterval(visibleRefresh, 30000);
    document.addEventListener('visibilitychange', visibleRefresh);
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', visibleRefresh); };
  }, [refresh]);
  const name = (id: string) => nodes.find((n) => n.id === id)?.name || id;
  const links = snapshot?.links || [];
  useEffect(() => {
    const ips = Array.from(new Set([...nodes.map((n) => n.public_ip || ''), ...links.map((l) => l.metric.address), ...mtrs.flatMap((m) => m.mtr.hops.map((h) => h.host))])).filter(publicIP).slice(0, 24);
    for (const ip of ips) {
      if (requestedIPs.current.has(ip)) continue;
      requestedIPs.current.add(ip);
      void fetch(`/api/ip-info?ip=${encodeURIComponent(ip)}`).then((r) => r.ok ? r.json() : null).then((data: IPInfo | null) => { if (data) setIPInfo((prev) => ({ ...prev, [ip]: data })); }).catch(() => {});
    }
  }, [nodes, links, mtrs]);
  const incident = incidents[0];
  const incidentLink = incident ? links.find((l) => l.node === incident.node && l.metric.link_id === incident.link) : undefined;
  const activeLink = selected || (incident ? `${incident.node}/${incident.link}` : '') || (links[0] ? `${links[0].node}/${links[0].metric.link_id}` : '');
  const active = links.find((l) => `${l.node}/${l.metric.link_id}` === activeLink);
  const chart = useMemo(() => history.filter((h) => `${h.node}/${h.metric.link_id}` === activeLink).sort((a, b) => a.ts - b.ts).map((h) => ({ time: new Date(h.ts * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }), success: h.metric.tcp?.success_pct ?? (h.metric.ping ? 100 - h.metric.ping.loss_pct : 100), rtt: h.metric.ping?.avg_ms || h.metric.tcp?.avg_ms || 0 })), [history, activeLink]);
  const selectedTraffic = hourly.filter((h) => `${h.node}/${h.link}` === activeLink).reduce((sum, h) => sum + h.rx_bytes + h.tx_bytes, 0);
  const healthy = links.filter((l) => linkQuality(l) === '正常').length;
  const recentMtr = incident ? mtrs.find((m) => m.node === incident.node && m.link === incident.link && Math.abs(m.ts - incident.end_ts) <= 600) : undefined;
  const comparator = incident ? links.find((l) => l.node === incident.node && l.metric.target === links.find((x) => x.node === incident.node && x.metric.link_id === incident.link)?.metric.target && l.metric.link_id !== incident.link && Boolean(l.metric.tailscale) !== Boolean(links.find((x) => x.node === incident.node && x.metric.link_id === incident.link)?.metric.tailscale)) : undefined;

  async function logout() { await fetch('/api/logout', { method: 'POST' }); window.location.assign('/login'); }
  function editNode(n: Node) { setEditing(n); setEditName(n.name); setEditNote(n.note || ''); }
  async function saveNode() {
    if (!editing) return;
    try {
      const response = await fetch(`/api/nodes/${encodeURIComponent(editing.id)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: editName, note: editNote }) });
      if (!response.ok) throw new Error('保存失败');
      setEditing(null); toast.add({ type: 'success', title: '节点信息已保存' }); void refresh();
    } catch { toast.add({ type: 'error', title: '保存失败', description: '请检查名称和连接状态。' }); }
  }
  function editTraffic(item: TrafficStatus) {
    setTrafficEditing(item); setQuotaInput(String(item.quota_gb)); setResetInput(String(item.reset_day)); setModeInput(item.billing_mode); setCalibrationInput('');
  }
  async function saveTraffic() {
    if (!trafficEditing) return;
    try {
      const id = encodeURIComponent(trafficEditing.id);
      const response = await fetch(`/api/traffic-plan/${id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ quota_gb: Number(quotaInput), reset_day: Number(resetInput), billing_mode: modeInput }) });
      if (!response.ok) throw new Error('配置无效');
      if (calibrationInput.trim() !== '') {
        const calibrated = await fetch(`/api/traffic-calibration/${id}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ used_gb: Number(calibrationInput) }) });
        if (!calibrated.ok) throw new Error('校准无效');
      }
      setTrafficEditing(null); toast.add({ type: 'success', title: '流量配置已保存' }); void refresh();
    } catch { toast.add({ type: 'error', title: '保存失败', description: '请检查额度、结算日和校准值。' }); }
  }

  return <main className="min-h-screen bg-background text-foreground"><div className="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-8 md:px-8">
    <header className="flex flex-wrap items-center justify-between gap-4"><div><div className="flex items-center gap-3"><Activity aria-hidden="true" className="text-primary" /><h1 className="text-2xl font-semibold tracking-tight">Chainwatch</h1><Badge variant="secondary">网络诊断</Badge></div><p className="mt-1 text-sm text-muted-foreground">{updated ? `最近更新 ${new Date(updated).toLocaleTimeString('zh-CN')}` : '等待数据'}</p></div><div className="flex gap-2"><Button variant="outline" size="sm" onClick={() => void refresh(true)}><RefreshCw data-icon="inline-start" />刷新</Button><Button variant="ghost" size="sm" onClick={() => void logout()}>退出</Button></div></header>
    {error && <Alert variant="destructive"><ShieldAlert aria-hidden="true" /><AlertTitle>监控数据暂不可用</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
    <Tabs value={activeTab} onValueChange={setActiveTab} className="gap-4"><TabsList aria-label="监控页面"><TabsTrigger value="overview">总览</TabsTrigger><TabsTrigger value="links">链路</TabsTrigger><TabsTrigger value="mtr">路由诊断</TabsTrigger><TabsTrigger value="traffic">流量</TabsTrigger></TabsList><TabsContent value="overview" className="space-y-4">
    {!snapshot ? <Skeleton className="h-32 w-full" /> : <section className="grid grid-cols-2 gap-3 lg:grid-cols-4"><Stat icon={Server} label="在线主机" value={`${snapshot.nodes.filter((n) => isFresh(n.ts)).length}/${snapshot.nodes.length}`} note="最近 3 分钟上报" /><Stat icon={ArrowRightLeft} label="健康链路" value={`${healthy}/${links.length}`} note="按最新一次采样" /><Stat icon={Activity} label="月度额度" value={`${traffic.filter((x) => x.quota_gb > 0).length}/${traffic.length}`} note="已配置服务器 · 详情见流量" /><Stat icon={HardDrive} label="监控存储" value={bytes(snapshot.storage_bytes)} note={`可用 ${bytes(snapshot.free_bytes)}`} /></section>}

    <section className="grid gap-4 lg:grid-cols-[1.2fr_1fr]"><Card><CardHeader><CardTitle>故障定位</CardTitle><CardDescription>最近一次连续异常</CardDescription>{incident && <CardAction><Badge variant={incident.confidence === '高' ? 'destructive' : 'secondary'}>{incident.confidence}可信度</Badge></CardAction>}</CardHeader><CardContent>{incident ? <div className="flex flex-col gap-4"><div><div className="text-xl font-semibold">{incident.cause}</div><p className="text-sm text-muted-foreground">{incidentLink ? route(incidentLink, nodes).description : incident.link_name} · {when(incident.start_ts)}–{when(incident.end_ts)} · {incident.samples} 次异常</p></div><div className="grid grid-cols-2 gap-3"><Stat icon={Activity} label="故障链路·当前" value={metricValue(links.find((l) => l.node === incident.node && l.metric.link_id === incident.link)?.metric || { link_id: '', link_name: '', target: '', address: '', protocol: '' })} note="TCP 成功率 / ICMP 到达率" /><Stat icon={ArrowRightLeft} label="对照链路·当前" value={comparator ? metricValue(comparator.metric) : '—'} note={comparator ? route(comparator, nodes).description : '未配置同目标对照'} /></div><div className="flex flex-wrap gap-2">{incident.evidence.map((e, i) => <Badge key={i} variant="outline" className="max-w-full whitespace-normal text-left">{e}</Badge>)}</div>{recentMtr && <p className="text-xs text-muted-foreground">MTR 记录 {when(recentMtr.ts)} · {recentMtr.mtr.hops.length} 跳；中间跳不回应不等于转发故障。</p>}</div> : <Empty><EmptyHeader><EmptyTitle>暂无异常</EmptyTitle><EmptyDescription>故障发生后会自动合并同链路异常并展示对照数据。</EmptyDescription></EmptyHeader></Empty>}</CardContent></Card>
    <Card><CardHeader><CardTitle>主机与版本</CardTitle><CardDescription>采样状态及软件版本</CardDescription></CardHeader><CardContent><Table><TableHeader><TableRow><TableHead>主机</TableHead><TableHead>状态</TableHead><TableHead>版本</TableHead><TableHead>负载</TableHead></TableRow></TableHeader><TableBody>{snapshot?.nodes.map((n) => <TableRow key={n.id}><TableCell><div className="font-mono font-medium">{nodes.find((x) => x.id === n.id)?.public_ip || nodes.find((x) => x.id === n.id)?.tailscale_ip || n.id}</div><div className="text-xs text-muted-foreground">{name(n.id)} · {nodes.find((x) => x.id === n.id)?.note || "暂无备注"}</div><Button variant="ghost" size="xs" onClick={() => { const node = nodes.find((x) => x.id === n.id); if (node) editNode(node); }}><Pencil data-icon="inline-start" />编辑</Button><div className="text-xs text-muted-foreground">{when(n.ts)}</div></TableCell><TableCell><Badge variant={isFresh(n.ts) ? 'secondary' : 'destructive'}>{isFresh(n.ts) ? '在线' : '中断'}</Badge></TableCell><TableCell className="font-mono text-xs">{version(n.system.version)}</TableCell><TableCell>{fmt(n.system.load_1m)}</TableCell></TableRow>)}</TableBody></Table><Separator className="my-4" /><div className="grid grid-cols-2 gap-3 text-sm"><div><span className="text-muted-foreground">前端</span><div className="font-mono">{version(versions?.frontend)}</div></div><div><span className="text-muted-foreground">构建</span><div className="truncate font-mono" title={versions?.build}>{version(versions?.build)}</div></div></div></CardContent></Card></section>

    </TabsContent><TabsContent value="links" className="space-y-4">
    <Card><CardHeader><CardTitle>链路波动</CardTitle><CardDescription>最近 6 小时逐次采样；成功率低于 100% 即有失败</CardDescription><CardAction><NativeSelect size="sm" aria-label="选择链路" value={activeLink} onChange={(e) => setSelected(e.target.value)}>{links.map((l) => <NativeSelectOption key={`${l.node}/${l.metric.link_id}`} value={`${l.node}/${l.metric.link_id}`}>{route(l, nodes).description} · {route(l, nodes).type}</NativeSelectOption>)}</NativeSelect></CardAction></CardHeader><CardContent>{chart.length ? <ChartContainer className="h-64 w-full aspect-auto" config={{ success: { label: '成功率', color: 'var(--chart-1)' }, rtt: { label: 'RTT', color: 'var(--chart-2)' } }}><LineChart data={chart}><CartesianGrid vertical={false} /><XAxis dataKey="time" tickLine={false} axisLine={false} minTickGap={28} /><YAxis domain={[0, 100]} unit="%" /><ChartTooltip content={<ChartTooltipContent />} /><Line dataKey="success" name="成功率" stroke="var(--color-success)" dot={false} strokeWidth={2} type="monotone" /></LineChart></ChartContainer> : <Empty><EmptyHeader><EmptyTitle>暂无采样</EmptyTitle></EmptyHeader></Empty>}<Separator className="my-4" /><div className="flex flex-wrap gap-4 text-sm"><span>当前 RTT <strong>{fmt(active?.metric.ping?.avg_ms ?? active?.metric.tcp?.avg_ms, ' ms')}</strong></span><span>当前丢包 <strong>{fmt(active?.metric.ping?.loss_pct, '%')}</strong></span><span>链路 <strong>{active ? route(active, nodes).description : "—"}</strong>{active?.metric.tailscale && ` · Tailscale ${active.metric.tailscale.path === "direct" ? "直连" : active.metric.tailscale.path}`}</span><span>30 天链路流量 <strong>{bytes(selectedTraffic)}</strong></span></div></CardContent></Card>

    <Card><CardHeader><CardTitle>所有有向链路</CardTitle><CardDescription>每行是一台主机发起的一次探测；路径按来源 IP → 目标 IP:端口展示。公网归属取自 RIPEstat，AS 归属不代表实际转发经过的所有网络。</CardDescription></CardHeader><CardContent><Table><TableHeader><TableRow><TableHead>探测路径</TableHead><TableHead>类型 / 实际连接</TableHead><TableHead>状态</TableHead><TableHead>TCP 成功率</TableHead><TableHead>ICMP 丢包</TableHead><TableHead>RTT</TableHead></TableRow></TableHeader><TableBody>{links.map((l) => { const r = route(l, nodes); return <TableRow key={`${l.node}/${l.metric.link_id}`}><TableCell className="min-w-64"><div className="font-mono text-sm font-medium">{r.description}</div><div className="text-xs text-muted-foreground">{name(l.node)} → {r.destination}</div>{publicIP(r.from) && <div className="text-xs text-muted-foreground">来源归属：{ipSummary(ipInfo[r.from])}</div>}{publicIP(l.metric.address) && <div className="text-xs text-muted-foreground">目标归属：{ipSummary(ipInfo[l.metric.address])}</div>}</TableCell><TableCell><Badge variant="outline">{r.type}</Badge><div className="mt-1 text-xs text-muted-foreground">{r.type === '本机端口' ? '仅验证本机 Xray 的 TCP 入口，不代表代理链全程可用' : r.type === 'Tailscale' ? `节点间覆盖网络 · ${l.metric.tailscale?.path === 'direct' ? '点对点直连' : l.metric.tailscale?.path || '路径待确认'}${l.metric.tailscale?.endpoint ? ` · 对端公网端点 ${l.metric.tailscale.endpoint}` : ''}` : '直接探测目标公网 IP，不经 Tailscale'}</div></TableCell><TableCell><Badge variant={linkQuality(l) === '正常' ? 'secondary' : 'destructive'}>{linkQuality(l)}</Badge></TableCell><TableCell className="tabular-nums">{l.metric.tcp ? `${l.metric.tcp.success_pct}%` : '—'}</TableCell><TableCell className="tabular-nums">{fmt(l.metric.ping?.loss_pct, '%')}</TableCell><TableCell className="tabular-nums">{fmt(l.metric.ping?.avg_ms ?? l.metric.tcp?.avg_ms, ' ms')}</TableCell></TableRow>; })}</TableBody></Table><p className="mt-3 text-xs text-muted-foreground">Xray 是两台服务器上的代理程序。“本机 Xray”只检查 127.0.0.1 的监听端口；Tailscale 的 direct 表示两机点对点连接，不表示客户端也走了 Tailscale。表中是监控探测方向，不是客户端代理流量的逐跳验证；来源公网 IP 是配置地址，可能与 NAT 后实际出口不同。</p></CardContent></Card>

    </TabsContent><TabsContent value="mtr" className="space-y-4">
    {recentMtr && <Card><CardHeader><CardTitle>故障时 MTR</CardTitle><CardDescription>{when(recentMtr.ts)} · 目标 {recentMtr.mtr.target} · 末端与连续多跳变化更有参考价值</CardDescription></CardHeader><CardContent><Table><TableHeader><TableRow><TableHead>跳</TableHead><TableHead>逐跳地址与归属</TableHead><TableHead>丢包</TableHead><TableHead>平均 RTT</TableHead></TableRow></TableHeader><TableBody>{recentMtr.mtr.hops.map((hop, i) => <TableRow key={`${hop.count}-${i}`}><TableCell>{hop.count}</TableCell><TableCell><div className="font-mono text-xs">{hop.host}</div><div className="text-xs text-muted-foreground">{publicIP(hop.host) ? ipSummary(ipInfo[hop.host]) : "私有 / 未应答地址"}</div></TableCell><TableCell>{fmt(hop['Loss%'], '%')}</TableCell><TableCell>{fmt(hop.Avg, ' ms')}</TableCell></TableRow>)}</TableBody></Table></CardContent></Card>}
    {!recentMtr && <Card><CardHeader><CardTitle>路由诊断</CardTitle><CardDescription>异常时的逐跳 MTR 会在这里展示。</CardDescription></CardHeader><CardContent><Empty><EmptyHeader><EmptyTitle>暂无故障 MTR</EmptyTitle><EmptyDescription>链路恢复或暂无异常时，可在“链路”页查看当前双向探测。</EmptyDescription></EmptyHeader></Empty></CardContent></Card>}
    </TabsContent><TabsContent value="traffic" className="space-y-4">
    {traffic.length ? traffic.map((item) => <TrafficCard key={item.id} item={item} node={nodes.find((n) => n.id === item.id)} onEdit={editTraffic} />) : <Skeleton className="h-48 w-full" />}
    </TabsContent></Tabs>

    <Dialog open={Boolean(editing)} onOpenChange={(open) => { if (!open) setEditing(null); }}><DialogContent><DialogHeader><DialogTitle>编辑节点</DialogTitle><DialogDescription>IP 是默认标识；名称和备注仅用于看板展示。</DialogDescription></DialogHeader><label className="space-y-1 text-sm">名称<Input value={editName} maxLength={60} onChange={(e) => setEditName(e.target.value)} /></label><label className="space-y-1 text-sm">备注<Textarea value={editNote} maxLength={240} onChange={(e) => setEditNote(e.target.value)} /></label><Button onClick={() => void saveNode()}>保存</Button></DialogContent></Dialog>
    <Dialog open={Boolean(trafficEditing)} onOpenChange={(open) => { if (!open) setTrafficEditing(null); }}><DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>流量配置与校准</DialogTitle><DialogDescription>在主服务器保存本节点的月额度；校准值填服务商面板显示的本月已用量。</DialogDescription></DialogHeader><div className="grid grid-cols-2 gap-3"><label className="space-y-1 text-sm">月额度（GB）<Input type="number" min="0" max="10000000" step="1" value={quotaInput} onChange={(e) => setQuotaInput(e.target.value)} /></label><label className="space-y-1 text-sm">每月结算日（UTC）<Input type="number" min="1" max="31" step="1" value={resetInput} onChange={(e) => setResetInput(e.target.value)} /></label></div><label className="space-y-1 text-sm">计费口径<NativeSelect value={modeInput} onChange={(e) => setModeInput(e.target.value as TrafficStatus['billing_mode'])}><NativeSelectOption value="sum">上传 + 下载</NativeSelectOption><NativeSelectOption value="max">上传/下载较大值</NativeSelectOption><NativeSelectOption value="tx">仅上传</NativeSelectOption><NativeSelectOption value="rx">仅下载</NativeSelectOption></NativeSelect></label><label className="space-y-1 text-sm">校准本月已用（GB，可留空）<Input type="number" min="0" max="10000000" step="0.01" placeholder={trafficEditing ? gb(trafficEditing.used_bytes) : ''} value={calibrationInput} onChange={(e) => setCalibrationInput(e.target.value)} /></label><p className="text-xs text-muted-foreground">留空会保留本周期已有校准；改变结算日或计费口径后需要重新校准。不会读取或补算启用前的数据。</p><Button onClick={() => void saveTraffic()}>保存配置</Button></DialogContent></Dialog>

    <footer className="flex flex-wrap justify-between gap-2 text-xs text-muted-foreground"><span>故障定位为证据推断；三次 TCP / MTR 和五次 ICMP 采样不能单独证明某个运营商节点故障。</span><span>主机 {version(snapshot?.nodes.find((n) => n.id === nodes[0]?.id)?.system.version)} · 从机 {version(snapshot?.nodes.find((n) => n.id !== nodes[0]?.id)?.system.version)} · 前端 {version(versions?.frontend)} · 构建 {version(versions?.build).slice(0, 12)}{frontendBuild !== 'dev' && versions?.build !== frontendBuild ? '（前后端版本不一致）' : ''}</span></footer>
  </div></main>;
}
