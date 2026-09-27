'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Badge, Box, Button, Card, Container, Divider, Group, Progress,
  Select, SimpleGrid, Skeleton, Stack, Table, Text, Title,
} from '@mantine/core';
import {
  CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip as ChartTooltip,
  XAxis, YAxis,
} from 'recharts';

type Node = { id: string; name: string };
type Ping = { loss_pct: number; avg_ms: number; jitter_ms: number };
type TCP = { success_pct: number; avg_ms: number };
type Tail = { path: string; endpoint?: string; online: boolean; rx_bytes: number; tx_bytes: number };
type Metric = {
  link_id: string; link_name: string; target: string; address: string; protocol: string;
  ping?: Ping; tcp?: TCP; tailscale?: Tail; rx_delta?: number; tx_delta?: number;
};
type System = { load_1m: number; memory_pct: number; disk_pct: number; nic_rx_bytes: number; nic_tx_bytes: number };
type LiveNode = { id: string; name: string; ts: number; system: System };
type LiveLink = { node: string; ts: number; metric: Metric; bad: boolean };
type Snapshot = { nodes: LiveNode[]; links: LiveLink[]; storage_bytes: number; free_bytes: number };
type Hourly = { bucket: number; node: string; link: string; count: number; avg_ms: number; max_ms: number; max_loss: number; bad_count: number; rx_bytes: number; tx_bytes: number };
type EventRow = { node: string; ts: number; bad: boolean; metric: Metric };
type MTR = { node: string; link: string; ts: number; mtr: { target: string; reason: string; hops: Array<{ count: number; host: string; 'Loss%': number; Avg: number; Wrst: number }> } };

const colors = ['#38d9a9', '#748ffc', '#f59f00', '#f06595', '#66d9e8'];
const fmt = (v: number | undefined, suffix = '') => v === undefined || !Number.isFinite(v) ? '—' : `${v.toFixed(1)}${suffix}`;
const bytes = (v: number) => v >= 1073741824 ? `${(v / 1073741824).toFixed(2)} GiB` : v >= 1048576 ? `${(v / 1048576).toFixed(1)} MiB` : `${(v / 1024).toFixed(0)} KiB`;
const when = (ts: number) => new Date(ts * 1000).toLocaleString('zh-CN', { hour12: false });
const status = (link: LiveLink) => Date.now() / 1000 - link.ts > 180 ? '采样中断' : link.bad ? '异常' : '正常';

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return <Card radius="lg" padding="xl" withBorder bg="#101b2b" style={{ borderColor: '#26374e' }}><Stack gap="md"><Title order={3} size="h4">{title}</Title>{children}</Stack></Card>;
}

export default function Dashboard() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [hourly, setHourly] = useState<Hourly[]>([]);
  const [events, setEvents] = useState<EventRow[]>([]);
  const [mtrs, setMtrs] = useState<MTR[]>([]);
  const [days, setDays] = useState('1');
  const [selected, setSelected] = useState<string | null>(null);
  const [error, setError] = useState('');
  const [updated, setUpdated] = useState(0);

  const refresh = useCallback(async () => {
    try {
      const paths = ['/api/topology', '/api/snapshot', '/api/hourly?days=30', '/api/events', '/api/mtr'];
      const responses = await Promise.all(paths.map((path) => fetch(path, { cache: 'no-store' })));
      if (responses.some((r) => !r.ok)) throw new Error('服务返回错误');
      const [n, s, h, e, m] = await Promise.all(responses.map((r) => r.json()));
      setNodes(n); setSnapshot(s); setHourly(h); setEvents(e); setMtrs(m); setError(''); setUpdated(Date.now());
    } catch (e) { setError(e instanceof Error ? e.message : '无法连接到监控中心'); }
  }, []);
  useEffect(() => { void refresh(); const timer = setInterval(() => { void refresh(); }, 30000); return () => clearInterval(timer); }, [refresh]);

  const linkChoices = useMemo(() => snapshot?.links.map((l) => ({ value: `${l.node}/${l.metric.link_id}`, label: `${nodes.find((n) => n.id === l.node)?.name || l.node} → ${l.metric.link_name}` })) || [], [snapshot, nodes]);
  const activeLink = selected || linkChoices[0]?.value || '';
  const selectedName = linkChoices.find((l) => l.value === activeLink)?.label || '链路';
  const since = Date.now() / 1000 - Number(days) * 86400;
  const chart = useMemo(() => hourly.filter((h) => `${h.node}/${h.link}` === activeLink && h.bucket >= since).sort((a, b) => a.bucket - b.bucket).map((h) => ({ label: new Date(h.bucket * 1000).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit' }), rtt: Number(h.avg_ms.toFixed(1)), loss: h.max_loss, failures: h.bad_count })), [hourly, activeLink, since]);
  const totals = useMemo(() => hourly.filter((h) => `${h.node}/${h.link}` === activeLink && h.bucket >= since).reduce((a, h) => ({ rx: a.rx + h.rx_bytes, tx: a.tx + h.tx_bytes, bad: a.bad + h.bad_count, count: a.count + h.count }), { rx: 0, tx: 0, bad: 0, count: 0 }), [hourly, activeLink, since]);
  const alive = snapshot?.nodes.filter((n) => Date.now() / 1000 - n.ts <= 180).length || 0;
  const healthy = snapshot?.links.filter((l) => status(l) === '正常').length || 0;

  return <main className="hero">
    <Container size="xl" py={{ base: 24, sm: 38 }}>
      <Stack gap="xl">
        <Group justify="space-between" align="start">
          <Box><Text size="sm" fw={700} c="teal.3" tt="uppercase" lts={2}>Chainwatch</Text><Title order={1} mt={4}>网络运行态势</Title><Text c="dimmed" mt={7}>主机、服务与有向链路的集中监测</Text></Box>
          <Stack gap={6} align="end"><Badge size="lg" color={error ? 'red' : 'teal'} variant="light">{error || (updated ? '正在监控' : '加载中')}</Badge><Text size="xs" c="dimmed">{updated ? `更新于 ${new Date(updated).toLocaleTimeString('zh-CN')}` : '等待首次上报'}</Text><Button size="xs" variant="subtle" onClick={() => void refresh()}>刷新</Button></Stack>
        </Group>

        {!snapshot ? <Skeleton height={140} radius="lg" /> : <SimpleGrid cols={{ base: 2, md: 4 }} spacing="md">
          <Stat label="在线主机" value={`${alive} / ${snapshot.nodes.length}`} detail="最近 3 分钟内上报" />
          <Stat label="健康链路" value={`${healthy} / ${snapshot.links.length}`} detail="按最新一次采样" />
          <Stat label="监控存储" value={bytes(snapshot.storage_bytes)} detail={`剩余磁盘 ${bytes(snapshot.free_bytes)}`} />
          <Stat label="所选链路流量" value={bytes(totals.rx + totals.tx)} detail={`${days} 天双向 Tailscale 数据`} />
        </SimpleGrid>}

        <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="md">
          <Panel title="主机"><Stack gap="sm">{snapshot?.nodes.length ? snapshot.nodes.map((n) => <Card key={n.id} radius="md" p="md" bg="#162437"><Group justify="space-between"><Box><Text fw={650}>{n.name}</Text><Text size="xs" c="dimmed">{n.id} · {when(n.ts)}</Text></Box><Badge color={Date.now() / 1000 - n.ts <= 180 ? 'teal' : 'red'} variant="light">{Date.now() / 1000 - n.ts <= 180 ? '在线' : '离线'}</Badge></Group><Group gap="lg" mt="sm"><Text size="sm" c="dimmed">负载 <Text component="span" c="gray.1">{fmt(n.system.load_1m)}</Text></Text><Text size="sm" c="dimmed">内存 <Text component="span" c="gray.1">{fmt(n.system.memory_pct, '%')}</Text></Text><Text size="sm" c="dimmed">磁盘 <Text component="span" c="gray.1">{fmt(n.system.disk_pct, '%')}</Text></Text></Group></Card>) : <Text c="dimmed">尚无主机上报。</Text>}</Stack></Panel>
          <Panel title="有向链路"><Stack gap="sm">{snapshot?.links.length ? snapshot.links.map((l) => <Card key={`${l.node}/${l.metric.link_id}`} radius="md" p="md" bg="#162437"><Group justify="space-between" align="start"><Box><Text fw={650}>{nodes.find((n) => n.id === l.node)?.name || l.node} → {l.metric.link_name}</Text><Text size="xs" c="dimmed">{l.metric.target} · {l.metric.address}</Text></Box><Badge color={status(l) === '正常' ? 'teal' : status(l) === '异常' ? 'orange' : 'red'} variant="light">{status(l)}</Badge></Group><Group gap="lg" mt="sm"><Text size="sm" c="dimmed">RTT <Text component="span" c="gray.1">{fmt(l.metric.ping?.avg_ms ?? l.metric.tcp?.avg_ms, ' ms')}</Text></Text><Text size="sm" c="dimmed">丢包 <Text component="span" c="gray.1">{fmt(l.metric.ping?.loss_pct, '%')}</Text></Text><Text size="sm" c="dimmed">Tailscale <Text component="span" c="gray.1">{l.metric.tailscale?.path || '—'}</Text></Text></Group></Card>) : <Text c="dimmed">尚无链路采样。</Text>}</Stack></Panel>
        </SimpleGrid>

        <Panel title="链路趋势与流量"><Group justify="space-between"><Text c="dimmed" size="sm">选择一条有向链路查看小时汇总、异常与实际流量增量。</Text><Group><Select size="sm" w={300} data={linkChoices} value={activeLink || null} onChange={setSelected} placeholder="选择链路" /><Select size="sm" w={110} data={[{ value: '1', label: '24 小时' }, { value: '7', label: '7 天' }, { value: '30', label: '30 天' }]} value={days} onChange={(v) => setDays(v || '1')} /></Group></Group><Divider color="#26374e" /><Text fw={600}>{selectedName}</Text><Box className="chart-wrap">{chart.length ? <ResponsiveContainer width="100%" height={270}><LineChart data={chart} margin={{ top: 12, right: 16, bottom: 6, left: -18 }}><CartesianGrid stroke="#24344a" strokeDasharray="3 3" /><XAxis dataKey="label" tick={{ fill: '#91a4ba', fontSize: 11 }} minTickGap={32} /><YAxis tick={{ fill: '#91a4ba', fontSize: 11 }} /><ChartTooltip contentStyle={{ background: '#152337', border: '1px solid #354961', borderRadius: 10 }} /><Line type="monotone" dataKey="rtt" name="平均 RTT (ms)" stroke={colors[0]} dot={false} strokeWidth={2.5} connectNulls /><Line type="monotone" dataKey="loss" name="最高丢包 (%)" stroke={colors[2]} dot={false} strokeWidth={1.5} /></LineChart></ResponsiveContainer> : <Text c="dimmed" ta="center" py={80}>所选时段暂无历史数据</Text>}</Box><SimpleGrid cols={{ base: 2, md: 4 }}><Tiny label="接收流量" value={bytes(totals.rx)} /><Tiny label="发送流量" value={bytes(totals.tx)} /><Tiny label="异常采样" value={String(totals.bad)} /><Tiny label="总采样" value={String(totals.count)} /></SimpleGrid><Text size="xs" c="dimmed">Tailscale 单对节点字节数不等于运营商计费流量；网卡总流量在主机指标中单独采集。</Text></Panel>

        <SimpleGrid cols={{ base: 1, lg: 2 }} spacing="md">
          <Panel title="最近异常"><Stack gap="xs">{events.length ? events.slice(0, 20).map((e, i) => <Group key={`${e.node}-${e.ts}-${i}`} justify="space-between" py={5} style={{ borderBottom: '1px solid #24344a' }}><Box><Text size="sm" fw={550}>{e.node} · {e.metric.link_name}</Text><Text size="xs" c="dimmed">{when(e.ts)}</Text></Box><Badge color="orange" variant="light">{e.metric.ping?.loss_pct ? `丢包 ${e.metric.ping.loss_pct}%` : e.metric.tcp?.success_pct !== undefined ? `TCP ${e.metric.tcp.success_pct}%` : e.metric.tailscale?.path || '异常'}</Badge></Group>) : <Text c="dimmed">最近 30 天没有异常记录。</Text>}</Stack></Panel>
          <Panel title="MTR 路由取证"><Stack gap="sm">{mtrs.length ? mtrs.slice(0, 10).map((m, i) => <Box key={`${m.node}-${m.link}-${m.ts}-${i}`}><Group justify="space-between"><Text size="sm" fw={550}>{m.node} → {m.mtr.target}</Text><Badge size="sm" variant="light" color={m.mtr.reason === 'anomaly' ? 'orange' : 'blue'}>{m.mtr.reason === 'anomaly' ? '异常触发' : '定时基线'}</Badge></Group><Text size="xs" c="dimmed">{when(m.ts)} · {m.mtr.hops.length} 跳</Text><details><summary style={{ cursor: 'pointer', color: '#63dfc1', fontSize: 12, marginTop: 4 }}>查看逐跳数据</summary><Table striped highlightOnHover mt="xs" fz="xs"><Table.Thead><Table.Tr><Table.Th>跳数</Table.Th><Table.Th>地址</Table.Th><Table.Th>丢包</Table.Th><Table.Th>平均 RTT</Table.Th></Table.Tr></Table.Thead><Table.Tbody>{m.mtr.hops.map((h) => <Table.Tr key={`${h.count}-${h.host}`}><Table.Td>{h.count}</Table.Td><Table.Td>{h.host}</Table.Td><Table.Td>{h['Loss%']}%</Table.Td><Table.Td>{h.Avg} ms</Table.Td></Table.Tr>)}</Table.Tbody></Table></details></Box>) : <Text c="dimmed">尚无 MTR 报告。</Text>}</Stack></Panel>
        </SimpleGrid>

        <Text size="xs" c="dimmed" ta="center" pb="md">Chainwatch · 正常原始数据 24 小时 · 小时汇总与异常 30 天 · 所有时间按浏览器时区显示</Text>
      </Stack>
    </Container>
  </main>;
}

function Stat({ label, value, detail }: { label: string; value: string; detail: string }) { return <Card radius="lg" padding="lg" withBorder bg="#101b2b" style={{ borderColor: '#26374e' }}><Text size="sm" c="dimmed">{label}</Text><Title order={2} className="metric-value" mt={7}>{value}</Title><Text size="xs" c="dimmed" mt={3}>{detail}</Text></Card>; }
function Tiny({ label, value }: { label: string; value: string }) { return <Box><Text size="xs" c="dimmed">{label}</Text><Text fw={650} size="lg" className="metric-value">{value}</Text><Progress value={0} size={2} color="teal" mt={5} /></Box>; }
