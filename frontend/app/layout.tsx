import type { Metadata } from 'next';
import { ThemeProvider } from '@/components/theme-provider';
import { LocaleProvider } from '@/components/locale-provider';
import './globals.css';
export const metadata: Metadata = { title: 'GoAdmin Kit | System Status', description: 'GoAdmin Kit foundation service status' };
export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="zh-CN" suppressHydrationWarning><body><ThemeProvider><LocaleProvider>{children}</LocaleProvider></ThemeProvider></body></html>;
}
