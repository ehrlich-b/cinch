import { useState, useEffect, useRef } from 'react'

type Page = 'home' | 'jobs' | 'workers' | 'settings' | 'badges'

interface AuthState {
  authenticated: boolean
  user?: string
  isPro?: boolean
  loading: boolean
}

export function App() {
  const [page, setPage] = useState<Page>('home')
  const [selectedJob, setSelectedJob] = useState<string | null>(null)
  const [auth, setAuth] = useState<AuthState>({ authenticated: false, loading: true })

  // Check auth status on load
  useEffect(() => {
    fetch('/auth/me')
      .then(r => r.json())
      .then(data => setAuth({ ...data, loading: false }))
      .catch(() => setAuth({ authenticated: false, loading: false }))
  }, [])

  // Show landing page for unauthenticated users or when on home
  if (!auth.loading && (!auth.authenticated || page === 'home')) {
    return <LandingPage auth={auth} setAuth={setAuth} onNavigate={setPage} />
  }

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
              <span className="user">{auth.user} {auth.isPro && '(Pro)'}</span>
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

function LandingPage({ auth, setAuth, onNavigate }: {
  auth: AuthState,
  setAuth: (auth: AuthState) => void,
  onNavigate: (page: Page) => void
}) {
  const [givingPro, setGivingPro] = useState(false)

  const handleGivePro = async () => {
    setGivingPro(true)
    try {
      const res = await fetch('/api/give-me-pro', { method: 'POST' })
      if (res.ok) {
        setAuth({ ...auth, isPro: true })
      }
    } catch (e) {
      console.error('Failed to give pro:', e)
    }
    setGivingPro(false)
  }

  return (
    <div className="landing">
      <header className="landing-header">
        <div className="landing-header-inner">
          <span className="landing-logo">Cinch</span>
          <nav className="landing-nav">
            <a href="#features">Features</a>
            <a href="#quickstart">Quick Start</a>
            <a href="#pricing">Pricing</a>
            <a href="https://github.com/ehrlich-b/cinch">Code</a>
            {auth.authenticated ? (
              <button className="landing-btn" onClick={() => onNavigate('jobs')}>Dashboard</button>
            ) : (
              <a href="/auth/login" className="landing-btn">Login</a>
            )}
          </nav>
        </div>
      </header>

      <div className="container">
        <section className="hero">
          <h1>CI that's a <span>Cinch</span></h1>
          <p className="tagline">One config. Every forge. Your hardware. Always green.</p>

          <div className="command-showcase">
<pre><span className="prompt">$</span> cinch worker --token=xxx
<span className="output">Connected to cinch.sh</span>
<span className="output">Waiting for jobs...</span>
<span className="output">Running: github.com/you/repo @ main</span>
<span className="output">Build passed in 12s</span></pre>
          </div>

          <div className="install-row">
            <div className="install-box">
              <code className="install-cmd">curl -sSL https://cinch.sh/install.sh | sh</code>
              <button className="copy-btn" onClick={() => navigator.clipboard.writeText('curl -sSL https://cinch.sh/install.sh | sh')}>
                Copy
              </button>
            </div>
          </div>
          <p className="install-note">macOS and Linux. Windows via WSL.</p>
        </section>
      </div>

      <section className="features-section" id="features">
        <div className="container">
          <h2>Why Cinch?</h2>
          <p className="section-subtitle">Your Makefile is the pipeline. We just invoke it.</p>
          <div className="features-grid-landing">
            <div className="feature-card">
              <h3>Multi-Forge</h3>
              <p>GitHub, GitLab, Forgejo, Gitea, Bitbucket. One worker, any forge. Stop learning vendor-specific YAML.</p>
            </div>
            <div className="feature-card">
              <h3>Your Hardware</h3>
              <p>Run builds on your Mac, your VM, your Raspberry Pi. No per-minute charges. No waiting in shared queues.</p>
            </div>
            <div className="feature-card">
              <h3>Dead Simple</h3>
              <p>One command in .cinch.yaml. No multi-step pipelines, no DAGs, no plugins. Just make ci.</p>
            </div>
          </div>
        </div>
      </section>

      <div className="container">
        <section className="quickstart" id="quickstart">
          <h2>Quick Start</h2>
          <div className="steps">
            <div className="step">
              <div className="step-number">1</div>
              <h3>Install the worker</h3>
              <p>Download the binary and run <code>cinch worker</code> on any machine you control.</p>
            </div>
            <div className="step">
              <div className="step-number">2</div>
              <h3>Add the GitHub App</h3>
              <p>Install the Cinch GitHub App on your repos. We handle webhooks and status checks.</p>
            </div>
            <div className="step">
              <div className="step-number">3</div>
              <h3>Push code</h3>
              <p>Add <code>.cinch.yaml</code> with your build command. Push to trigger your first build.</p>
            </div>
          </div>
        </section>
      </div>

      <section className="pricing-section" id="pricing">
        <div className="container">
          <h2>Pricing</h2>
          <p className="pricing-subtitle">Free while in beta. Self-hosting always free.</p>
          <div className="pricing-grid-landing">
            <div className="plan-card">
              <div className="plan-name">Public Repos</div>
              <div className="plan-price">$0</div>
              <div className="plan-note">Free forever</div>
              <ul className="plan-features-list">
                <li>Unlimited builds</li>
                <li>Unlimited workers</li>
                <li>All forges supported</li>
                <li>Community support</li>
              </ul>
            </div>
            <div className="plan-card featured">
              <div className="plan-name">Pro</div>
              <div className="plan-price">$5<span className="period">/seat/mo</span></div>
              <div className="plan-note">For private repos</div>
              <ul className="plan-features-list">
                <li>Everything in Free</li>
                <li>Private repositories</li>
                <li>Priority support</li>
                <li>Badge customization</li>
              </ul>
              <div className="plan-cta">
                {auth.isPro ? (
                  <div className="pro-status">You have Pro</div>
                ) : auth.authenticated ? (
                  <button className="btn-pro" onClick={handleGivePro} disabled={givingPro}>
                    {givingPro ? 'Activating...' : 'Give me Pro'}
                  </button>
                ) : (
                  <a href="/auth/login" className="btn-pro" style={{ display: 'block', textAlign: 'center', textDecoration: 'none' }}>
                    Login to get Pro
                  </a>
                )}
              </div>
            </div>
            <div className="plan-card">
              <div className="plan-name">Self-Hosted</div>
              <div className="plan-price">$0</div>
              <div className="plan-note">MIT Licensed</div>
              <ul className="plan-features-list">
                <li>Run your own server</li>
                <li>Full control</li>
                <li>No limits</li>
                <li>Community support</li>
              </ul>
            </div>
          </div>
        </div>
      </section>

      <footer className="landing-footer">
        <div className="footer-inner">
          <div className="footer-brand">Cinch</div>
          <div className="footer-links">
            <a href="https://github.com/ehrlich-b/cinch">GitHub</a>
            <a href="https://github.com/ehrlich-b/cinch/issues">Issues</a>
            <a href="mailto:bryan@ehrlich.dev">Contact</a>
          </div>
        </div>
        <div className="footer-copy">
          Open source under MIT license. Built by <a href="mailto:bryan@ehrlich.dev" style={{ color: 'inherit' }}>Bryan Ehrlich</a>.
        </div>
      </footer>
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
function BadgesPage() {
  const [copied, setCopied] = useState(false)

  const badgeUrl = 'https://cinch.sh/badge/github.com/owner/repo.svg'
  const markdownSnippet = `[![CI](${badgeUrl})](https://cinch.sh/jobs)`

  const copyToClipboard = () => {
    navigator.clipboard.writeText(markdownSnippet)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="badges-page">
      <div className="badges-hero">
        <h2>Build Badges</h2>
        <p className="badges-subtitle">Add your build status to any README</p>
      </div>

      <div className="badges-preview-section">
        <div className="badge-preview-large">
          <img
            src="https://img.shields.io/badge/CI-passing-brightgreen?logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIyMCIgaGVpZ2h0PSIxNCIgdmlld0JveD0iMCAwIDIwIDE0Ij48dGV4dCB4PSIwIiB5PSIxMSIgZm9udC1mYW1pbHk9InVpLW1vbm9zcGFjZSxTRk1vbm8tUmVndWxhcixNZW5sbyxNb25hY28sQ29uc29sYXMsbW9ub3NwYWNlIiBmb250LXNpemU9IjEyIiBmb250LXdlaWdodD0iNzAwIiBmaWxsPSIjZmZmIj5DSTwvdGV4dD48L3N2Zz4="
            alt="CI passing"
          />
        </div>
      </div>

      <div className="badge-usage">
        <h3>Add to your README</h3>
        <div className="code-block">
          <code>{markdownSnippet}</code>
          <button onClick={copyToClipboard} className="copy-btn">
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
        <p className="usage-note">
          Replace <code>github.com/owner/repo</code> with your repository.
          Use <code>?branch=main</code> for a specific branch.
        </p>
        <p className="usage-note" style={{ marginTop: '0.5rem', opacity: 0.7 }}>
          Powered by <a href="https://shields.io" target="_blank" rel="noopener noreferrer">shields.io</a>
        </p>
      </div>
    </div>
  )
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
