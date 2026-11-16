export type Health = { status: 'ok'; database: 'connected'; migration: 'current'; checked_at: string };
export type Envelope<T> = { code: number; message: string; data: T | null };
