'use client';

import { createContext, useContext, useMemo, useSyncExternalStore, type ReactNode } from 'react';

export type Locale = 'zh-CN' | 'en-US';

const messages: Record<Locale, Record<string, string>> = {
  'zh-CN': {
    '插件管理': '插件管理', '插件': '插件', '已安装': '已安装', '本地可信包': '本地可信包', '事件记录': '事件记录',
    '刷新插件': '刷新插件', '安装插件': '安装插件', '启用插件': '启用插件', '停用插件': '停用插件', '卸载插件': '卸载插件',
    '升级插件': '升级插件', '配置': '配置', '版本': '版本', '作者': '作者', '状态': '状态', '权限': '权限', '路由': '路由',
    '依赖': '依赖', '描述': '描述', '签名密钥': '签名密钥', '安装时间': '安装时间', '事件': '事件', '结果': '结果',
    '没有已安装的插件。': '没有已安装的插件。', '可信包目录中没有可验证的插件包。': '可信包目录中没有可验证的插件包。',
    '插件数据加载失败，请重试。': '插件数据加载失败，请重试。', '插件操作失败，请检查依赖和签名包。': '插件操作失败，请检查依赖和签名包。',
    '插件已安装。': '插件已安装。', '插件已更新。': '插件已更新。', '插件操作已完成。': '插件操作已完成。', '配置已保存。': '配置已保存。',
    '可信本地包：服务端仅列出签名有效且文件校验通过的版本。不会执行上传的二进制、Shell 或 JavaScript。': '可信本地包：服务端仅列出签名有效且文件校验通过的版本。不会执行上传的二进制、Shell 或 JavaScript。',
    '插件路由': '插件路由', '此插件未提供可用的编译期 Provider。': '此插件未提供可用的编译期 Provider。', '返回插件管理': '返回插件管理',
    '暂无插件事件。': '暂无插件事件。', '插件状态异常': '插件状态异常', '配置未设置': '配置未设置', '已配置（值已隐藏）': '已配置（值已隐藏）',
    '敏感配置不回显，留空表示保持原值。': '敏感配置不回显，留空表示保持原值。', '确认卸载该插件？其插件数据迁移不会自动回滚，事件记录会保留。': '确认卸载该插件？其插件数据迁移不会自动回滚，事件记录会保留。',
    '插件路由标识无效。': '插件路由标识无效。', '插件路由请求失败。': '插件路由请求失败。', '请求内容必须是有效 JSON。': '请求内容必须是有效 JSON。',
    '请求方法': '请求方法', '发送请求': '发送请求', '请求内容 JSON': '请求内容 JSON', '成功': '成功', '失败': '失败',
  },
  'en-US': {
    '管理概览': 'Dashboard', 'Dashboard': 'Dashboard', '管理员管理': 'Administrators', '用户管理': 'Administrators',
    '角色管理': 'Roles', '权限管理': 'Permissions', '菜单管理': 'Menus', '个人资料': 'Profile',
    '系统设置': 'System settings', '媒体库': 'Media library', '审计日志': 'Audit logs', '登录日志': 'Login logs',
    '站内通知': 'Notifications', 'API 密钥': 'API keys', '后台任务': 'Background jobs', '文章管理': 'Articles',
    '文章分类': 'Article categories', '管理模块': 'Workspace', '侧边导航': 'Main navigation', '管理导航': 'Main navigation',
    '工作区': 'Workspace', '打开导航': 'Open navigation', 'GoAdmin Kit 管理控制台': 'GOADMIN KIT ADMIN CONSOLE',
    '管理员用户': 'Administrator users', '权限目录': 'Permission catalog', '文章': 'Articles', '操作审计': 'Audit trail',
    '重试': 'Retry', '正在加载菜单…': 'Loading navigation…', '当前账号没有已分配的菜单。': 'No navigation items are assigned to this account.',
    '暂时无法加载管理权限，请刷新重试。': 'Could not load access settings. Refresh and try again.',
    '无法加载菜单或权限，请重试。': 'Could not load navigation or permissions. Please retry.',
    '正在验证登录状态…': 'Checking your session…', '没有访问此页面的权限。': 'You do not have access to this page.',
    '请使用已授权的导航项目，或联系管理员调整角色权限。': 'Choose an authorized page or ask an administrator to update your role.',
    '重试加载': 'Retry loading', '搜索': 'Search', '刷新': 'Refresh', '操作': 'Actions', '创建': 'Create',
    '保存': 'Save', '取消': 'Cancel', '删除': 'Delete', '编辑': 'Edit', '查看': 'View', '关闭': 'Close',
    '详情': 'Details', '新建用户': 'Create administrator', '新建菜单': 'Create menu', '新建分类': 'Create category',
    '新建文章': 'Create article', '编辑分类': 'Edit category', '保存分类': 'Save category', '保存菜单': 'Save menu',
    '保存角色': 'Save role', '保存资料': 'Save profile', '保存更改': 'Save changes', '更新密码': 'Update password',
    '上一页': 'Previous', '下一页': 'Next', '加载中…': 'Loading…', '正在加载…': 'Loading…', '暂无数据。': 'No data yet.',
    '暂无通知。': 'No notifications yet.', '没有符合条件的记录。': 'No results match your filters.', '全部': 'All',
    '状态': 'Status', '名称': 'Name', '描述': 'Description', '代码': 'Code', '类型': 'Type', '更新时间': 'Updated',
    '邮箱': 'Email', '姓名': 'Display name', '当前密码': 'Current password', '新密码': 'New password', '确认新密码': 'Confirm new password',
    '默认分页条数': 'Default page size', '站点名称': 'Site name', '站点描述': 'Site description', '结束日期 (UTC)': 'End date (UTC)',
    '访问权限': 'Access permission', '登录结果': 'Sign-in result', '筛选': 'Filter', '结果': 'Result', '对象': 'Object',
    '目录': 'Directory', '页面': 'Page', '外链': 'External link', '显示': 'Visible', '排序': 'Order', '父级': 'Parent',
    '启用': 'Active', '停用': 'Disabled', '系统': 'System', '自定义': 'Custom', '重置': 'Reset',
    '未读': 'Unread', '标记已读': 'Mark as read', '发送': 'Send', '发送中…': 'Sending…', '文件名': 'File name',
    '上传文件': 'Upload file', '选择文件': 'Choose file', '预览': 'Preview', '下载': 'Download', '上传': 'Upload',
    '文件已保存。': 'File saved.', '文件已删除。': 'File deleted.', '登录管理后台': 'Sign in to GoAdmin Kit',
    '管理员访问': 'ADMINISTRATOR ACCESS', '使用已初始化的管理员账号继续。': 'Sign in with your initialized administrator account.',
    '用户名': 'Username', '密码': 'Password', '登录': 'Sign in', '正在登录': 'Signing in…', '显示密码': 'Show password',
    '隐藏密码': 'Hide password', 'GoAdmin Kit': 'GoAdmin Kit', '快速导航': 'Quick access', '最近操作': 'Recent activity',
    '安全事件趋势': 'Security event trend', '管理员': 'Administrators', '角色': 'Roles', '安全事件': 'Security events',
    '刷新数据': 'Refresh data', '正在加载真实统计数据…': 'Loading live statistics…', '无法获取仪表盘数据，请重试。': 'Could not load dashboard data. Please retry.',
    '当前会话 · 概览': 'CURRENT SESSION · OVERVIEW', '已连接管理员账号 · 数据来自当前数据库': 'Administrator session active · Live database data',
    '你好，': 'Welcome, ',
    '服务状态': 'Service status', '正常': 'Operational', '暂不可用': 'Unavailable', '快捷导航': 'Quick access',
    '依据当前角色和菜单授权': 'Based on your role and menu permissions', '当前没有可访问的快捷入口。': 'No permitted shortcuts are available.',
    '暂无操作记录。': 'No recent activity.', '时间均为 UTC': 'Times shown in UTC', '退出登录': 'Sign out',
    '退出所有设备': 'Sign out all sessions', '第': 'Page', '已发布': 'Published', '草稿': 'Draft', '分类': 'Category',
    '未分类': 'Uncategorized', '标题': 'Title', 'Slug': 'Slug', '没有符合条件的文章。': 'No articles match your filters.',
    '文章列表': 'Article list', '新建角色': 'New role', '新建管理员': 'New administrator',
    '保存中…': 'Saving…', '正在加载文章…': 'Loading articles…', '正在加载媒体文件…': 'Loading media…',
    '正在读取通知…': 'Loading notifications…', '正在加载角色…': 'Loading roles…', '暂无角色。': 'No roles yet.',
    '共': 'Total: ', '个文件': ' files', '篇文章': ' articles', '条': ' items', '第 {page} 页': 'Page {page}',
    '图片': 'Images', '附件': 'Attachments', '清除': 'Clear', '内容': 'Content', '正文': 'Body',
    '收件人 UUID': 'Recipient UUID', '管理员 UUID': 'Administrator UUID', '发送通知': 'Send notification',
    '我的通知 · 未读': 'My notifications · Unread', '刷新通知': 'Refresh notifications', '上传失败。': 'Upload failed.',
    '无法预览。': 'Preview unavailable.', '无法下载。': 'Download failed.', '无法读取媒体文件。': 'Could not load media.',
    '删除失败。': 'Delete failed.', '分类加载失败。': 'Could not load categories.', '操作失败。': 'Action failed.',
    '登录失败，请稍后重试。': 'Sign-in failed. Please try again.', '请输入用户名': 'Enter a username', '请输入密码': 'Enter a password',
    '正在检查登录状态': 'Checking sign-in status', '登录失败': 'Sign-in failed', '搜索文章': 'Search articles',
    '搜索用户名': 'Search usernames', '按管理员筛选': 'Filter by administrator', '登录记录': 'Sign-in records',
    '操作记录': 'Operation records', '记录详情': 'Record details', '日志筛选': 'Log filters', '日志详情': 'Log details',
    '有效会话': 'Active sessions', '当前及近期活动且未过期的登录会话。': 'Current and recent unexpired sign-in sessions.',
    '撤销其他会话': 'Revoke other sessions', '撤销当前会话后将立即退出，继续吗？': 'Revoking this session signs you out immediately. Continue?',
    '会话已撤销。': 'Session revoked.', '其他会话已撤销。': 'Other sessions revoked.', '刷新会话': 'Refresh sessions',
    '刷新日志': 'Refresh logs', '刷新密钥': 'Refresh keys', '刷新任务': 'Refresh jobs',
    '执行队列 ·': 'Execution queue ·', '我的密钥 ·': 'My keys ·',
    '执行记录加载失败。': 'Could not load execution history.', '任务列表加载失败。': 'Could not load jobs.',
    '通知加载失败，请重试。': 'Could not load notifications. Please retry.', '无法加载设置。': 'Could not load settings.',
    '无法读取权限。': 'Could not load permissions.', '无法读取用户。': 'Could not load users.', '无法读取菜单。': 'Could not load menus.',
    '无法读取角色。': 'Could not load roles.', '无法读取日志，请重试。': 'Could not load logs. Please retry.',
    '设置数据不可用': 'Settings are unavailable', '请求失败，请重试。': 'Request failed. Please retry.',
    '保存失败，请重试。': 'Save failed. Please retry.',
    '处理中…': 'Processing…', '正在更新…': 'Updating…', '执行中': 'Running', '等待执行': 'Queued', '已完成': 'Completed',
    '已失败': 'Failed', '已撤销': 'Revoked', '已过期': 'Expired', '成功': 'Success', '失败': 'Failed',
    '未分配角色': 'No role assigned', '无关联用户': 'No associated users', '无描述': 'No description', '无权限': 'No permissions',
    '父级只能选择目录。': 'Only directories can be selected as a parent.', '目录不能设置路由、组件或权限。': 'Directories cannot have a route, component, or permission.',
    '页面和外部链接必须选择访问权限。': 'Pages and external links require an access permission.',
    '创建失败，请检查名称、有效期和权限。': 'Creation failed. Check the name, expiration, and permissions.',
    '两次输入的新密码不一致。': 'The new passwords do not match.', '重试失败，任务可能不处于失败状态或已重试。': 'Retry failed. The job may no longer be failed or may already have been retried.',
    '撤销失败，请重试。': 'Revoke failed. Please retry.', '发送失败，请检查收件人、权限和内容。': 'Send failed. Check the recipient, permissions, and content.',
    '密码已重置，原会话已撤销。': 'Password reset. Existing sessions were revoked.', '用户已创建。': 'Administrator created.',
    '用户已删除。': 'Administrator deleted.', '用户状态已更新。': 'Administrator status updated.', '用户角色已更新。': 'Administrator roles updated.',
    '用户资料已更新。': 'Administrator profile updated.', '角色已创建。': 'Role created.', '角色已删除。': 'Role deleted.',
    '角色已更新。': 'Role updated.', '角色权限已更新。': 'Role permissions updated.', '分类已创建。': 'Category created.',
    '分类已保存。': 'Category saved.', '分类已删除。': 'Category deleted.', '菜单已创建。': 'Menu created.', '菜单已删除。': 'Menu deleted.',
    '菜单已更新。': 'Menu updated.', '菜单顺序已保存。': 'Menu order saved.', '菜单角色授权已更新。': 'Menu role assignments updated.',
    '系统设置已保存。': 'System settings saved.', '标记已读失败，请重试。': 'Could not mark as read. Please retry.',
    '最近操作 · 共': 'Recent activity · Total', '过去': 'Past', '天': 'days', '7 天': '7 days', '30 天': '30 days',
    '数据库及迁移就绪检查': 'Database and migration readiness', '已记录操作': 'Recorded operations', '累计认证成功': 'Successful sign-ins',
    '含限流拒绝': 'Includes rate-limited attempts', '查看每日数据': 'View daily data',
    '按 UTC 日期汇总，包含今天': 'Grouped by UTC date, including today', '趋势时间范围': 'Trend range',
    '所选时间内暂无安全事件。': 'No security events in this range.',
    '会话操作': 'Session actions',
    '退出失败，请重试。': 'Sign out failed. Please retry.', '正在加载权限': 'Loading permissions',
    '插件管理': 'Plugins', '插件': 'Plugin', '已安装': 'Installed', '本地可信包': 'Trusted local packages', '事件记录': 'Event history',
    '刷新插件': 'Refresh plugins', '安装插件': 'Install plugin', '启用插件': 'Enable', '停用插件': 'Disable', '卸载插件': 'Uninstall',
    '升级插件': 'Upgrade', '版本': 'Version', '作者': 'Author', '权限': 'Permissions', '路由': 'Routes',
    '依赖': 'Dependencies', '签名密钥': 'Signing key', '安装时间': 'Installed', '事件': 'Event',
    '没有已安装的插件。': 'No plugins installed.', '可信包目录中没有可验证的插件包。': 'No verifiable packages in the trusted package directory.',
    '插件数据加载失败，请重试。': 'Could not load plugin data. Please retry.', '插件操作失败，请检查依赖和签名包。': 'Plugin operation failed. Check its dependencies and signed package.',
    '插件已安装。': 'Plugin installed.', '插件已更新。': 'Plugin upgraded.', '插件操作已完成。': 'Plugin operation completed.', '配置已保存。': 'Configuration saved.',
    '可信本地包：服务端仅列出签名有效且文件校验通过的版本。不会执行上传的二进制、Shell 或 JavaScript。': 'Trusted local packages only: the server lists versions with valid signatures and file hashes. Uploaded binaries, shell commands, and JavaScript are never executed.',
    '插件路由': 'Plugin route', '此插件未提供可用的编译期 Provider。': 'This plugin has no available build-time Provider.', '返回插件管理': 'Back to plugin management',
    '暂无插件事件。': 'No plugin events yet.', '插件状态异常': 'Plugin failure', '配置未设置': 'Not configured', '已配置（值已隐藏）': 'Configured (value hidden)',
    '敏感配置不回显，留空表示保持原值。': 'Secret values are never shown again. Leave blank to keep the current value.', '确认卸载该插件？其插件数据迁移不会自动回滚，事件记录会保留。': 'Uninstall this plugin? Its migrations are not automatically rolled back; event history is retained.',
    '插件路由标识无效。': 'Invalid plugin route identifier.', '插件路由请求失败。': 'Plugin route request failed.', '请求内容必须是有效 JSON。': 'Request body must be valid JSON.',
    '请求方法': 'Request method', '发送请求': 'Send request', '请求内容 JSON': 'Request JSON',
    install: 'Install', enable: 'Enable', disable: 'Disable', uninstall: 'Uninstall', upgrade: 'Upgrade', configure: 'Configure', error: 'Error',
  },
};

type LocaleContextValue = { locale: Locale; setLocale: (locale: Locale) => void; t: (value: string) => string };
const LocaleContext = createContext<LocaleContextValue>({ locale: 'zh-CN', setLocale: () => undefined, t: value => value });
const localeListeners = new Set<() => void>();
const subscribeLocale = (listener: () => void) => {
  localeListeners.add(listener);
  return () => localeListeners.delete(listener);
};
const getLocaleSnapshot = (): Locale => window.localStorage.getItem('goadmin.locale') === 'en-US' ? 'en-US' : 'zh-CN';
const getServerLocaleSnapshot = (): Locale => 'zh-CN';

export function LocaleProvider({ children }: { children: ReactNode }) {
  const locale = useSyncExternalStore(subscribeLocale, getLocaleSnapshot, getServerLocaleSnapshot);
  const setLocale = (value: Locale) => {
    window.localStorage.setItem('goadmin.locale', value);
    document.documentElement.lang = value;
    localeListeners.forEach(listener => listener());
  };
  const value = useMemo<LocaleContextValue>(() => ({
    locale,
    setLocale,
    t: text => messages[locale][text] ?? text,
  }), [locale]);
  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>;
}

export function useLocale() {
  return useContext(LocaleContext);
}
