export type Page = 'home' | 'jobs' | 'repo-jobs' | 'workers' | 'repos' | 'account' | 'gitlab-onboard' | 'forgejo-onboard' | 'success'

export interface AuthState {
  authenticated: boolean
  user?: string
  isPro?: boolean
  loading: boolean
}

export interface RepoPath {
  forge: string
  owner: string
  repo: string
}

export interface ConnectedForge {
  type: string
  username?: string
  connected_at?: string
}

export interface UserInfo {
  id: string
  name: string
  email?: string
  connected_forges: ConnectedForge[]
  created_at: string
  tier: string // "free" or "pro"
  storage_used_bytes: number
  storage_quota_bytes: number
}

export interface GitLabProject {
  id: number
  name: string
  path_with_namespace: string
  web_url: string
  visibility: string
}

export interface ForgejoRepo {
  id: number
  name: string
  full_name: string
  html_url: string
  private: boolean
  owner: { login: string }
}

export interface Repo {
  id: string
  forge_type: string
  owner: string
  name: string
  private?: boolean
  clone_url: string
  html_url: string
  build: string
  release: string
  created_at: string
  latest_job_status?: string
}

export interface JobAttempt {
  id: string
  status: string
  created_at: string
}

export interface Job {
  id: string
  repo: string
  branch: string
  pr_number?: number
  pr_base_branch?: string
  commit: string
  status: string
  duration?: number
  created_at?: string
  started_at?: string
  finished_at?: string
  attempts?: JobAttempt[] // Other jobs for same commit
}

export interface Worker {
  id: string
  name: string
  hostname?: string
  labels: string[]
  mode: 'personal' | 'shared'
  owner_name?: string
  version?: string
  status: string
  connected: boolean
  active_jobs: string[]
  currentJob?: string
  last_seen: string
}

export interface WorkerEvent {
  type: 'connected' | 'disconnected' | 'job_started' | 'job_finished'
  worker_id: string
  worker?: Worker
  job_id?: string
}

export interface LogEntry {
  stream: string
  data: string
  time: string
}
