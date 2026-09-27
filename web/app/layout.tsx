import './globals.css';
import type { Metadata } from 'next';
import { Toaster } from '@/components/ui/toast';

export const metadata: Metadata = {
  title: 'Chainwatch | 网络链路监控',
  description: '分布式主机、服务与链路监控',
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN" className="dark">
      <body>{children}<Toaster /></body>
    </html>
  );
}
