import { useState, useEffect, useRef } from 'react'

type Page = 'jobs' | 'workers' | 'settings' | 'badges'

interface AuthState {
  authenticated: boolean
  user?: string
  loading: boolean
}

export function App() {
  const [page, setPage] = useState<Page>('jobs')
  const [selectedJob, setSelectedJob] = useState<string | null>(null)
  const [auth, setAuth] = useState<AuthState>({ authenticated: false, loading: true })

  // Check auth status on load
  useEffect(() => {
    fetch('/auth/me')
      .then(r => r.json())
      .then(data => setAuth({ ...data, loading: false }))
      .catch(() => setAuth({ authenticated: false, loading: false }))
  }, [])

  return (
    <div className="app">
      <header>
        <h1>Cinch</h1>
        <nav>
          <button
            className={page === 'jobs' ? 'active' : ''}
            onClick={() => { setPage('jobs'); setSelectedJob(null) }}
          >
            Jobs
          </button>
          <button
            className={page === 'workers' ? 'active' : ''}
            onClick={() => setPage('workers')}
          >
            Workers
          </button>
          <button
            className={page === 'settings' ? 'active' : ''}
            onClick={() => setPage('settings')}
          >
            Settings
          </button>
          <button
            className={page === 'badges' ? 'active' : ''}
            onClick={() => setPage('badges')}
          >
            Badges
          </button>
        </nav>
        <div className="auth">
          {auth.loading ? null : auth.authenticated ? (
            <>
              <span className="user">{auth.user}</span>
              <a href="/auth/logout" className="logout">Logout</a>
            </>
          ) : (
            <a href="/auth/login" className="login">Login</a>
          )}
        </div>
      </header>
      <main>
        {page === 'jobs' && !selectedJob && <JobsPage onSelectJob={setSelectedJob} />}
        {page === 'jobs' && selectedJob && (
          <JobDetailPage jobId={selectedJob} onBack={() => setSelectedJob(null)} />
        )}
        {page === 'workers' && <WorkersPage />}
        {page === 'settings' && <SettingsPage />}
        {page === 'badges' && <BadgesPage />}
      </main>
    </div>
  )
}

function JobsPage({ onSelectJob }: { onSelectJob: (id: string) => void }) {
  const [jobs, setJobs] = useState<Job[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/jobs')
      .then(r => r.json())
      .then(data => {
        setJobs(data.jobs || [])
        setLoading(false)
      })
      .catch(() => setLoading(false))
  }, [])

  if (loading) return <div className="loading">Loading...</div>
  if (jobs.length === 0) return <div className="empty">No jobs yet</div>

  return (
    <div className="jobs">
      <table>
        <thead>
          <tr>
            <th>Status</th>
            <th>Repo</th>
            <th>Branch</th>
            <th>Commit</th>
            <th>Duration</th>
          </tr>
        </thead>
        <tbody>
          {jobs.map(job => (
            <tr key={job.id} onClick={() => onSelectJob(job.id)} className="clickable">
              <td><StatusIcon status={job.status} /></td>
              <td>{job.repo}</td>
              <td>{job.branch}</td>
              <td className="mono">{job.commit?.slice(0, 7)}</td>
              <td>{formatDuration(job.duration)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function JobDetailPage({ jobId, onBack }: { jobId: string; onBack: () => void }) {
  const [job, setJob] = useState<Job | null>(null)
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [status, setStatus] = useState<string>('')
  const logsEndRef = useRef<HTMLDivElement>(null)
  const wsRef = useRef<WebSocket | null>(null)

  // Fetch job details
  useEffect(() => {
    fetch(`/api/jobs/${jobId}`)
      .then(r => r.json())
      .then(data => setJob(data))
      .catch(console.error)
  }, [jobId])

  // Connect to log stream
  useEffect(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws/logs/${jobId}`)
    wsRef.current = ws

    ws.onmessage = (event) => {
      const msg = JSON.parse(event.data)
      if (msg.type === 'log') {
        setLogs(prev => [...prev, { stream: msg.stream, data: msg.data, time: msg.time }])
      } else if (msg.type === 'status') {
        setStatus(msg.status)
      }
    }

    ws.onerror = (e) => console.error('WebSocket error:', e)
    ws.onclose = () => console.log('WebSocket closed')

    return () => {
      ws.close()
    }
  }, [jobId])

  // Auto-scroll to bottom
  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  return (
    <div className="job-detail">
      <div className="job-header">
        <button onClick={onBack} className="back-btn">← Back</button>
        <h2>Job {jobId.slice(0, 12)}</h2>
        {job && (
          <div className="job-meta">
            <span><StatusIcon status={status || job.status} /></span>
            <span>{job.repo}</span>
            <span>{job.branch}</span>
            <span className="mono">{job.commit?.slice(0, 7)}</span>
          </div>
        )}
      </div>
      <div className="log-viewer">
        <pre>
          {logs.map((log, i) => (
            <span key={i} className={`log-line ${log.stream}`}>
              {renderAnsi(log.data)}
            </span>
          ))}
        </pre>
        <div ref={logsEndRef} />
      </div>
    </div>
  )
}

function WorkersPage() {
  const [workers, setWorkers] = useState<Worker[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/workers')
      .then(r => r.json())
      .then(data => {
        setWorkers(data.workers || [])
        setLoading(false)
      })
      .catch(() => setLoading(false))
  }, [])

  if (loading) return <div className="loading">Loading...</div>
  if (workers.length === 0) return <div className="empty">No workers connected</div>

  return (
    <div className="workers">
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Labels</th>
            <th>Status</th>
            <th>Current Job</th>
          </tr>
        </thead>
        <tbody>
          {workers.map(worker => (
            <tr key={worker.id}>
              <td>{worker.name}</td>
              <td>{worker.labels?.join(', ')}</td>
              <td>{worker.status}</td>
              <td>{worker.currentJob || '-'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function SettingsPage() {
  return (
    <div className="settings">
      <h2>Settings</h2>
      <p>Settings page coming soon...</p>
    </div>
  )
}

// Badge style definitions
const BADGE_STYLES = {
  // Shields.io tribute
  shields: { name: 'Shields.io Style', desc: 'The OG. Props to shields.io', link: 'https://shields.io' },
  // CLI/dev aesthetic
  terminal: { name: 'Terminal', desc: 'Lives in the command line' },
  // Modern styles
  neon: { name: 'Neon', desc: 'Glow effects' },
  electric: { name: 'Electric', desc: 'Bold uppercase' },
  // Clean styles
  modern: { name: 'Modern', desc: 'Clean dark minimal' },
  flat: { name: 'Flat', desc: 'Simple and readable' },
  minimal: { name: 'Minimal', desc: 'Just the essentials' },
  outlined: { name: 'Outlined', desc: 'Works on any background' },
  // Fun styles
  holographic: { name: 'Holographic', desc: 'Iridescent border' },
  pixel: { name: 'Pixel', desc: 'Retro 8-bit' },
  brutalist: { name: 'Brutalist', desc: 'Maximum impact' },
  gradient: { name: 'Gradient', desc: 'Smooth transitions' },
} as const

type BadgeStyle = keyof typeof BADGE_STYLES
type BadgeStatus = 'passing' | 'failing' | 'running' | 'unknown'

function BadgesPage() {
  const [selectedStyle, setSelectedStyle] = useState<BadgeStyle>('neon')
  const [selectedStatus, setSelectedStatus] = useState<BadgeStatus>('passing')
  const [copied, setCopied] = useState(false)

  const exampleOwner = 'yourname'
  const exampleRepo = 'yourrepo'
  const badgeUrl = `https://cinch.sh/badge/${exampleOwner}/${exampleRepo}.svg?style=${selectedStyle}`
  const markdownSnippet = `[![Build Status](${badgeUrl})](https://cinch.sh/${exampleOwner}/${exampleRepo})`

  const copyToClipboard = () => {
    navigator.clipboard.writeText(markdownSnippet)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="badges-page">
      <div className="badges-hero">
        <h2>Build Badges</h2>
        <p className="badges-subtitle">Show off your build status with style</p>
      </div>

      <div className="badges-preview-section">
        <div className="badge-preview-large">
          <BadgeSVG style={selectedStyle} status={selectedStatus} />
        </div>
        <div className="status-toggles">
          {(['passing', 'failing', 'running', 'unknown'] as const).map(status => (
            <button
              key={status}
              className={`status-toggle ${selectedStatus === status ? 'active' : ''}`}
              onClick={() => setSelectedStatus(status)}
            >
              {status}
            </button>
          ))}
        </div>
      </div>

      <div className="badges-grid">
        {(Object.entries(BADGE_STYLES) as [BadgeStyle, typeof BADGE_STYLES[BadgeStyle]][]).map(([key, style]) => (
          <div
            key={key}
            className={`badge-card ${selectedStyle === key ? 'selected' : ''}`}
            onClick={() => setSelectedStyle(key)}
          >
            <div className="badge-card-preview">
              <BadgeSVG style={key} status={selectedStatus} />
            </div>
            <div className="badge-card-info">
              <span className="badge-card-name">{style.name}</span>
              <span className="badge-card-desc">
                {style.desc}
                {'link' in style && (
                  <a href={style.link} target="_blank" rel="noopener noreferrer" onClick={e => e.stopPropagation()}> ↗</a>
                )}
              </span>
            </div>
          </div>
        ))}
      </div>

      <div className="badge-usage">
        <h3>Add to your README</h3>
        <div className="code-block">
          <code>{markdownSnippet}</code>
          <button onClick={copyToClipboard} className="copy-btn">
            {copied ? '✓ Copied' : 'Copy'}
          </button>
        </div>
        <p className="usage-note">
          Replace <code>yourname/yourrepo</code> with your repository path.
          Add <code>?branch=main</code> to show status for a specific branch.
        </p>
      </div>
    </div>
  )
}

// SVG Badge component - renders inline SVG for each style
function BadgeSVG({ style, status }: { style: BadgeStyle; status: BadgeStatus }) {
  const colors = {
    passing: { main: '#22c55e', glow: '#4ade80', text: 'passing' },
    failing: { main: '#ef4444', glow: '#f87171', text: 'failing' },
    running: { main: '#eab308', glow: '#facc15', text: 'running' },
    unknown: { main: '#6b7280', glow: '#9ca3af', text: 'unknown' },
  }
  const c = colors[status]

  switch (style) {
    case 'shields':
      // Faithful shields.io style
      return (
        <svg width="108" height="20" xmlns="http://www.w3.org/2000/svg">
          <linearGradient id="sh-b" x2="0" y2="100%">
            <stop offset="0" stopColor="#bbb" stopOpacity=".1"/>
            <stop offset="1" stopOpacity=".1"/>
          </linearGradient>
          <clipPath id="sh-a"><rect width="108" height="20" rx="3"/></clipPath>
          <g clipPath="url(#sh-a)">
            <path fill="#555" d="M0 0h49v20H0z"/>
            <path fill={c.main} d="M49 0h59v20H49z"/>
            <path fill="url(#sh-b)" d="M0 0h108v20H0z"/>
          </g>
          <g fill="#fff" textAnchor="middle" fontFamily="Verdana,Geneva,DejaVu Sans,sans-serif" fontSize="11">
            <text x="24.5" y="15" fill="#010101" fillOpacity=".3">cinch</text>
            <text x="24.5" y="14">cinch</text>
            <text x="77.5" y="15" fill="#010101" fillOpacity=".3">{c.text}</text>
            <text x="77.5" y="14">{c.text}</text>
          </g>
        </svg>
      )

    case 'flat':
      return (
        <svg width="108" height="20" xmlns="http://www.w3.org/2000/svg">
          <clipPath id="flat-a"><rect width="108" height="20" rx="3"/></clipPath>
          <g clipPath="url(#flat-a)">
            <path fill="#555" d="M0 0h49v20H0z"/>
            <path fill={c.main} d="M49 0h59v20H49z"/>
          </g>
          <g fill="#fff" textAnchor="middle" fontFamily="Verdana,Geneva,sans-serif" fontSize="11">
            <text x="24.5" y="14">cinch</text>
            <text x="77.5" y="14">{c.text}</text>
          </g>
        </svg>
      )

    case 'modern':
      return (
        <svg width="116" height="26" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id="mod-bg" x1="0%" y1="0%" x2="0%" y2="100%">
              <stop offset="0%" stopColor="#27272a"/>
              <stop offset="100%" stopColor="#18181b"/>
            </linearGradient>
          </defs>
          <rect width="116" height="26" rx="6" fill="url(#mod-bg)"/>
          <circle cx="16" cy="13" r="5" fill={c.main}/>
          <text x="28" y="17" fontFamily="-apple-system,sans-serif" fontSize="11" fontWeight="600" fill="#71717a">cinch</text>
          <text x="66" y="17" fontFamily="-apple-system,sans-serif" fontSize="11" fontWeight="500" fill="#a1a1aa">{c.text}</text>
        </svg>
      )

    case 'neon':
      return (
        <svg width="130" height="32" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <filter id={`neon-glow-${status}`} x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="3" result="blur"/>
              <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
            </filter>
            <linearGradient id={`neon-grad-${status}`} x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor={c.main}/>
              <stop offset="100%" stopColor={c.glow}/>
            </linearGradient>
          </defs>
          <rect width="130" height="32" rx="6" fill="#0a0a0a"/>
          <rect x="1" y="1" width="128" height="30" rx="5" fill="none" stroke={`url(#neon-grad-${status})`} strokeWidth="1.5" filter={`url(#neon-glow-${status})`} opacity="0.6"/>
          <text x="14" y="21" fontFamily="JetBrains Mono,monospace" fontSize="13" fontWeight="700" fill="#888">cinch</text>
          <line x1="60" y1="8" x2="60" y2="24" stroke="#333" strokeWidth="1"/>
          <text x="70" y="21" fontFamily="JetBrains Mono,monospace" fontSize="13" fontWeight="600" fill={`url(#neon-grad-${status})`} filter={`url(#neon-glow-${status})`}>{c.text}</text>
        </svg>
      )

    case 'electric':
      return (
        <svg width="140" height="28" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id={`elec-${status}`} x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor={c.main}/>
              <stop offset="100%" stopColor={c.glow}/>
            </linearGradient>
            <filter id={`elec-glow-${status}`} x="-100%" y="-100%" width="300%" height="300%">
              <feGaussianBlur stdDeviation="2" result="blur"/>
              <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
            </filter>
          </defs>
          <rect width="140" height="28" rx="4" fill="#0c0c0c"/>
          <rect x="0" y="0" width="140" height="1" fill={`url(#elec-${status})`} opacity="0.3"/>
          <text x="16" y="18" fontFamily="Inter,sans-serif" fontSize="12" fontWeight="800" fill="#666" letterSpacing="1">CINCH</text>
          <rect x="68" y="6" width="1" height="16" fill="#333"/>
          <text x="80" y="18" fontFamily="Inter,sans-serif" fontSize="12" fontWeight="600" fill={`url(#elec-${status})`} filter={`url(#elec-glow-${status})`}>{c.text.toUpperCase()}</text>
        </svg>
      )

    case 'terminal':
      return (
        <svg width="145" height="24" xmlns="http://www.w3.org/2000/svg">
          <rect width="145" height="24" rx="4" fill="#0d1117"/>
          <rect x="1" y="1" width="143" height="22" rx="3" fill="none" stroke="#30363d" strokeWidth="1"/>
          <text x="8" y="16" fontFamily="SF Mono,monospace" fontSize="11" fill={c.main}>$</text>
          <text x="20" y="16" fontFamily="SF Mono,monospace" fontSize="11" fill="#c9d1d9">cinch</text>
          <text x="58" y="16" fontFamily="SF Mono,monospace" fontSize="11" fill="#484f58">[</text>
          <text x="64" y="16" fontFamily="SF Mono,monospace" fontSize="11" fill={c.main} fontWeight="bold">{c.text.toUpperCase()}</text>
          <text x="122" y="16" fontFamily="SF Mono,monospace" fontSize="11" fill="#484f58">]</text>
          <rect x="132" y="6" width="7" height="12" fill="#58a6ff" opacity="0.8"/>
        </svg>
      )

    case 'brutalist':
      return (
        <svg width="100" height="28" xmlns="http://www.w3.org/2000/svg">
          <rect width="100" height="28" fill="#000"/>
          <rect x="2" y="2" width="96" height="24" fill={c.main}/>
          <text x="50" y="19" fontFamily="Arial Black,sans-serif" fontSize="11" fontWeight="900" fill="#000" textAnchor="middle">CINCH {c.text.toUpperCase().slice(0,4)}</text>
        </svg>
      )

    case 'gradient':
      return (
        <svg width="116" height="26" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id={`grad-bg-${status}`} x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor="#3b82f6"/>
              <stop offset="100%" stopColor={c.main}/>
            </linearGradient>
          </defs>
          <rect width="116" height="26" rx="6" fill={`url(#grad-bg-${status})`}/>
          <text x="14" y="17" fontFamily="-apple-system,sans-serif" fontSize="11" fontWeight="700" fill="#fff">cinch</text>
          <text x="58" y="17" fontFamily="-apple-system,sans-serif" fontSize="11" fontWeight="500" fill="#fff">{c.text}</text>
          <circle cx="102" cy="13" r="4" fill="#fff" fillOpacity="0.9"/>
        </svg>
      )

    case 'holographic':
      return (
        <svg width="120" height="28" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id="holo-border" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor="#ff00ff"/>
              <stop offset="33%" stopColor="#00ffff"/>
              <stop offset="66%" stopColor="#ffff00"/>
              <stop offset="100%" stopColor="#ff00ff"/>
            </linearGradient>
          </defs>
          <rect width="120" height="28" rx="6" fill="#0a0a0a"/>
          <rect x="1" y="1" width="118" height="26" rx="5" fill="none" stroke="url(#holo-border)" strokeWidth="1"/>
          <text x="16" y="18" fontFamily="Inter,sans-serif" fontSize="11" fontWeight="700" fill="#888">cinch</text>
          <text x="60" y="18" fontFamily="Inter,sans-serif" fontSize="11" fontWeight="600" fill={c.glow}>{c.text}</text>
          <circle cx="106" cy="14" r="4" fill={c.main}/>
        </svg>
      )

    case 'pixel':
      return (
        <svg width="112" height="24" xmlns="http://www.w3.org/2000/svg">
          <rect width="112" height="24" fill="#1a1a2e"/>
          <rect x="0" y="0" width="112" height="2" fill={c.main}/>
          <rect x="0" y="22" width="112" height="2" fill={c.main}/>
          <rect x="0" y="0" width="2" height="24" fill={c.main}/>
          <rect x="110" y="0" width="2" height="24" fill={c.main}/>
          <text x="12" y="16" fontFamily="Monaco,monospace" fontSize="10" fill="#eee" letterSpacing="1">CINCH</text>
          <text x="58" y="16" fontFamily="Monaco,monospace" fontSize="10" fill={c.main} letterSpacing="1">{c.text.toUpperCase().slice(0,4)}</text>
        </svg>
      )

    case 'minimal':
      return (
        <svg width="72" height="24" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id={`min-${status}`} x1="0%" y1="0%" x2="0%" y2="100%">
              <stop offset="0%" stopColor={c.main}/>
              <stop offset="100%" stopColor={c.glow}/>
            </linearGradient>
          </defs>
          <rect width="72" height="24" rx="12" fill={`url(#min-${status})`}/>
          <path d="M16 10 L20 14 L28 6" stroke="#fff" strokeWidth="2.5" fill="none" strokeLinecap="round" strokeLinejoin="round"/>
          <text x="38" y="16" fontFamily="-apple-system,sans-serif" fontSize="11" fontWeight="600" fill="#fff">cinch</text>
        </svg>
      )

    case 'outlined':
      return (
        <svg width="108" height="24" xmlns="http://www.w3.org/2000/svg">
          <rect x="1" y="1" width="106" height="22" rx="4" fill="none" stroke={c.main} strokeWidth="1.5"/>
          <line x1="50" y1="5" x2="50" y2="19" stroke={c.main} strokeWidth="1" opacity="0.5"/>
          <text x="25" y="16" fontFamily="-apple-system,sans-serif" fontSize="11" fontWeight="600" fill={c.main} textAnchor="middle">cinch</text>
          <text x="78" y="16" fontFamily="-apple-system,sans-serif" fontSize="11" fontWeight="500" fill={c.main} textAnchor="middle">{c.text}</text>
        </svg>
      )
  }
}

function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case 'success': return <span className="status success">✓</span>
    case 'failed':
    case 'failure': return <span className="status failure">✗</span>
    case 'running': return <span className="status running">◐</span>
    case 'pending':
    case 'queued': return <span className="status pending">◷</span>
    case 'error': return <span className="status error">!</span>
    default: return <span className="status">{status}</span>
  }
}

function formatDuration(ms?: number): string {
  if (!ms) return '-'
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${seconds % 60}s`
}

// Basic ANSI escape code renderer
function renderAnsi(text: string): string {
  // Strip ANSI codes for now - basic implementation
  // eslint-disable-next-line no-control-regex
  return text.replace(/\x1b\[[0-9;]*m/g, '')
}

interface Job {
  id: string
  repo: string
  branch: string
  commit: string
  status: string
  duration?: number
}

interface Worker {
  id: string
  name: string
  labels: string[]
  status: string
  currentJob?: string
}

interface LogEntry {
  stream: string
  data: string
  time: string
}
