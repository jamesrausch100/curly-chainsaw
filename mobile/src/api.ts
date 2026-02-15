// Curly Chainsaw — Mobile API Client
//
// Shared API layer that talks to the Go backend. Used by every screen.
// In production, API_BASE comes from env or config.

const API_BASE = __DEV__
  ? 'http://localhost:8080'
  : 'https://api.curlychainsaw.com';

interface Job {
  id: string;
  platform: string;
  company: string;
  title: string;
  description: string;
  location: string;
  remote: boolean;
  url: string;
  salary?: { min: number; max: number; currency: string; period: string };
  tags?: string[];
}

interface MatchResult {
  job: Job;
  score: number;
  reason: string;
}

interface Stats {
  jobs_discovered: number;
  matches_found: number;
  applications_sent: number;
  total_users: number;
}

interface Profile {
  id: string;
  name: string;
  email: string;
  phone?: string;
  location: string;
  remote_only: boolean;
  skills: string[];
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`);
  if (!res.ok) throw new Error(`GET ${path}: ${res.status}`);
  return res.json();
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`POST ${path}: ${res.status}`);
  return res.json();
}

export const api = {
  health:     ()       => get<{ status: string }>('/api/health'),
  stats:      ()       => get<Stats>('/api/stats'),
  profile:    ()       => get<Profile>('/api/profile'),
  matches:    ()       => get<MatchResult[]>('/api/jobs/matches'),
  search:     (q: string) => get<Job[]>(`/api/jobs/search?q=${encodeURIComponent(q)}`),
  discover:   ()       => post<{ matches: MatchResult[] }>('/api/jobs/discover', {}),
  apply:      (ids: string[]) => post<{ applied: number }>('/api/apply', { job_ids: ids }),
  saveProfile: (p: Profile) => post<Profile>('/api/profile', p),
};

export type { Job, MatchResult, Stats, Profile };
