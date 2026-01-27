import { useState, useEffect, useRef } from 'react'
import githubLogo from './assets/github.svg'
import gitlabLogo from './assets/gitlab.svg'
import giteaLogo from './assets/gitea.svg'
import forgejoLogo from './assets/forgejo.svg'
import codebergLogo from './assets/codeberg.svg'

type Page = 'home' | 'jobs' | 'repo-jobs' | 'workers' | 'repos' | 'badges' | 'account' | 'gitlab-onboard' | 'forgejo-onboard' | 'success'

interface AuthState {
  authenticated: boolean
  user?: string
  isPro?: boolean
  loading: boolean
}

interface RepoPath {
  forge: string
  owner: string
  repo: string
}

// Simple URL routing
function getPageFromPath(): { page: Page; jobId: string | null; repoPath: RepoPath | null } {
  const path = window.location.pathname

  if (path.startsWith('/jobs/')) {
    const rest = path.slice(6) // after /jobs/
    // Check if this is a repo path like github.com/owner/repo
    const parts = rest.split('/')
    if (parts.length >= 3 && parts[0].includes('.')) {
      return {
        page: 'repo-jobs',
        jobId: null,
        repoPath: { forge: parts[0], owner: parts[1], repo: parts[2] }
      }
    }
    // Otherwise it's a job ID
    return { page: 'jobs', jobId: rest, repoPath: null }
  }
  if (path === '/jobs') return { page: 'jobs', jobId: null, repoPath: null }
  if (path === '/workers') return { page: 'workers', jobId: null, repoPath: null }
  if (path === '/repos') return { page: 'repos', jobId: null, repoPath: null }
  if (path === '/badges') return { page: 'badges', jobId: null, repoPath: null }
  if (path === '/account') return { page: 'account', jobId: null, repoPath: null }
  if (path === '/gitlab/onboard') return { page: 'gitlab-onboard', jobId: null, repoPath: null }
  if (path === '/forgejo/onboard') return { page: 'forgejo-onboard', jobId: null, repoPath: null }
  if (path === '/success') return { page: 'success', jobId: null, repoPath: null }
  return { page: 'home', jobId: null, repoPath: null }
}

export function App() {
  const initial = getPageFromPath()
  const [page, setPage] = useState<Page>(initial.page)
  const [selectedJob, setSelectedJob] = useState<string | null>(initial.jobId)
  const [selectedRepoPath, setSelectedRepoPath] = useState<RepoPath | null>(initial.repoPath)
  const [auth, setAuth] = useState<AuthState>({ authenticated: false, loading: true })
  const [gitlabModal, setGitlabModal] = useState<'select-project' | 'token-choice' | null>(null)

  // Handle browser back/forward
  useEffect(() => {
    const handlePopState = () => {
      const { page, jobId, repoPath } = getPageFromPath()
      setPage(page)
      setSelectedJob(jobId)
      setSelectedRepoPath(repoPath)
    }
    window.addEventListener('popstate', handlePopState)
    return () => window.removeEventListener('popstate', handlePopState)
  }, [])

  // Navigate with history
  const navigate = (newPage: Page, jobId: string | null = null, repoPath: RepoPath | null = null) => {
    let path = '/'
    if (newPage === 'jobs' && jobId) path = `/jobs/${jobId}`
    else if (newPage === 'jobs') path = '/jobs'
    else if (newPage === 'repo-jobs' && repoPath) path = `/jobs/${repoPath.forge}/${repoPath.owner}/${repoPath.repo}`
    else if (newPage === 'workers') path = '/workers'
    else if (newPage === 'repos') path = '/repos'
    else if (newPage === 'badges') path = '/badges'
    else if (newPage === 'account') path = '/account'
    else if (newPage === 'gitlab-onboard') path = '/gitlab/onboard'
    else if (newPage === 'forgejo-onboard') path = '/forgejo/onboard'
    else if (newPage === 'success') path = '/success'

    history.pushState({}, '', path)
    setPage(newPage)
    setSelectedJob(jobId)
    setSelectedRepoPath(repoPath)
  }

  // Check auth status on load
  useEffect(() => {
    fetch('/auth/me')
      .then(r => r.json())
      .then(data => setAuth({ ...data, loading: false }))
      .catch(() => setAuth({ authenticated: false, loading: false }))
  }, [])

  // Show landing page for unauthenticated users or when on home
  // Exceptions: onboard pages, success page, and repo-jobs (public repos viewable without login)
  if (!auth.loading && (!auth.authenticated || page === 'home') && page !== 'gitlab-onboard' && page !== 'forgejo-onboard' && page !== 'success' && page !== 'repo-jobs') {
    return <LandingPage auth={auth} setAuth={setAuth} onNavigate={(p) => navigate(p)} />
  }

  // GitLab onboard page - show full-page onboarding
  if (page === 'gitlab-onboard') {
    if (!auth.authenticated && !auth.loading) {
      // Not logged in - redirect to GitLab OAuth
      window.location.href = '/auth/gitlab'
      return <div className="loading">Redirecting to GitLab...</div>
    }
    return (
      <GitLabOnboardPage
        onComplete={() => navigate('success')}
        onCancel={() => navigate('repos')}
      />
    )
  }

  // Forgejo/Codeberg onboard page
  if (page === 'forgejo-onboard') {
    if (!auth.authenticated && !auth.loading) {
      window.location.href = '/auth/forgejo'
      return <div className="loading">Redirecting to Codeberg...</div>
    }
    return (
      <ForgejoOnboardPage
        onComplete={() => navigate('success')}
        onCancel={() => navigate('repos')}
      />
    )
  }

  // Success page after onboarding
  if (page === 'success') {
    return <SuccessPage onContinue={() => navigate('jobs')} />
  }

  // Public repo jobs page (accessible without login)
  if (page === 'repo-jobs' && !auth.authenticated && selectedRepoPath) {
    return (
      <div className="app">
        <header>
          <h1 onClick={() => navigate('home')} style={{ cursor: 'pointer' }}>cinch</h1>
          <nav></nav>
          <div className="auth">
            <a href="/auth/login" className="login">Login</a>
          </div>
        </header>
        <main>
          <RepoJobsPage
            repoPath={selectedRepoPath}
            onSelectJob={(id) => navigate('jobs', id)}
            onBack={() => navigate('home')}
          />
        </main>
      </div>
    )
  }

  return (
    <div className="app">
      <header>
        <h1 onClick={() => navigate('home')} style={{ cursor: 'pointer' }}>cinch</h1>
        <nav>
          <button
            className={page === 'jobs' ? 'active' : ''}
            onClick={() => navigate('jobs')}
          >
            Jobs
          </button>
          <button
            className={page === 'workers' ? 'active' : ''}
            onClick={() => navigate('workers')}
          >
            Workers
          </button>
          <button
            className={page === 'repos' ? 'active' : ''}
            onClick={() => navigate('repos')}
          >
            Repos
          </button>
          <button
            className={page === 'badges' ? 'active' : ''}
            onClick={() => navigate('badges')}
          >
            Badges
          </button>
        </nav>
        <div className="auth">
          {auth.loading ? null : auth.authenticated ? (
            <>
              <button className="user-btn" onClick={() => navigate('account')}>{auth.user}</button>
              <a href="/auth/logout" className="logout">Logout</a>
            </>
          ) : (
            <a href="/auth/login" className="login">Login</a>
          )}
        </div>
      </header>
      <main>
        {page === 'jobs' && !selectedJob && <JobsPage onSelectJob={(id) => navigate('jobs', id)} />}
        {page === 'jobs' && selectedJob && (
          <JobDetailPage jobId={selectedJob} onBack={() => navigate('jobs')} />
        )}
        {page === 'repo-jobs' && selectedRepoPath && (
          <RepoJobsPage
            repoPath={selectedRepoPath}
            onSelectJob={(id) => navigate('jobs', id)}
            onBack={() => navigate('jobs')}
          />
        )}
        {page === 'workers' && <WorkersPage />}
        {page === 'repos' && (
          <ReposPage
            onAddGitLab={() => window.location.href = '/auth/gitlab'}
            onAddForgejo={() => window.location.href = '/auth/forgejo'}
            onSelectRepo={(repoPath) => navigate('repo-jobs', null, repoPath)}
          />
        )}
        {page === 'badges' && <BadgesPage />}
        {page === 'account' && <AccountPage onLogout={() => window.location.href = '/auth/logout'} />}
      </main>
      {gitlabModal && (
        <GitLabSetupModal
          mode={gitlabModal}
          onClose={() => {
            setGitlabModal(null)
            // Clear URL params
            history.replaceState({}, '', window.location.pathname)
          }}
          onComplete={() => {
            setGitlabModal(null)
            history.replaceState({}, '', '/repos')
            setPage('repos')
          }}
          onNeedToken={() => setGitlabModal('token-choice')}
        />
      )}
    </div>
  )
}

function LandingPage({ auth, setAuth, onNavigate }: {
  auth: AuthState,
  setAuth: (auth: AuthState) => void,
  onNavigate: (page: Page) => void
}) {
  const [givingPro, setGivingPro] = useState(false)
  const [showForgeSelector, setShowForgeSelector] = useState(false)

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
          <span className="landing-logo">cinch</span>
          <nav className="landing-nav">
            <a href="#features">Features</a>
            <a href="#quickstart">Quick Start</a>
            <a href="#pricing">Pricing</a>
            <a href="https://github.com/ehrlich-b/cinch">Code</a>
            {auth.authenticated ? (
              <button className="landing-btn" onClick={() => onNavigate('jobs')}>Dashboard</button>
            ) : (
              <button className="landing-btn" onClick={() => setShowForgeSelector(true)}>Get Started</button>
            )}
          </nav>
        </div>
      </header>

      <div className="container">
        <section className="hero">
          <h1>CI that's a <span>cinch</span></h1>
          <p className="tagline">The exact <code>make build</code> you run locally. That's your CI.</p>

          <div className="config-showcase">
            <div className="config-file">
              <div className="config-filename">.cinch.yaml</div>
              <pre className="config-content">build: make build{'\n'}release: make release</pre>
            </div>
            <div className="config-file">
              <div className="config-filename">Makefile</div>
              <pre className="config-content">build:{'\n'}    go build -o bin/app{'\n'}{'\n'}release:{'\n'}    cinch release dist/*</pre>
            </div>
          </div>

          <p className="hero-subtext">Your Makefile already works. We just run it on push.</p>

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
          <h2>Why cinch?</h2>
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
              <h3>Install & login</h3>
              <p><code>curl -sSL https://cinch.sh/install.sh | sh</code> then <code>cinch login</code></p>
            </div>
            <div className="step">
              <div className="step-number">2</div>
              <h3>Start a worker</h3>
              <p><code>cinch worker</code> — runs on your Mac, Linux box, or Raspberry Pi.</p>
            </div>
            <div className="step">
              <div className="step-number">3</div>
              <h3>Push code</h3>
              <p>Add <code>.cinch.yaml</code> with <code>build: make build</code> and push.</p>
            </div>
          </div>
        </section>
      </div>

      <section className="pricing-section" id="pricing">
        <div className="container">
          <h2>Pricing</h2>
          <p className="pricing-subtitle">Free while in beta. MIT licensed. Self-host anytime.</p>
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
              <div className="plan-cta"></div>
            </div>
            <div className="plan-card featured">
              <div className="plan-name">Pro</div>
              <div className="plan-price"><s className="old-price">$5</s> $0<span className="period">/seat/mo</span></div>
              <div className="plan-note">Free during beta</div>
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
              <div className="plan-name">Enterprise</div>
              <div className="plan-price">Custom</div>
              <div className="plan-note">For teams that need support</div>
              <ul className="plan-features-list">
                <li>Dedicated support</li>
                <li>SLA guarantees</li>
                <li>Custom integrations</li>
                <li>Managed hosting option</li>
              </ul>
              <div className="plan-cta"></div>
            </div>
          </div>
        </div>
      </section>

      <footer className="landing-footer">
        <div className="footer-inner">
          <div className="footer-brand">cinch</div>
          <div className="footer-links">
            <a href="https://github.com/ehrlich-b/cinch">GitHub</a>
            <a href="https://github.com/ehrlich-b/cinch/issues">Issues</a>
            <a href="mailto:bryan@ehrlich.dev">Contact</a>
          </div>
        </div>
        <div className="footer-copy">
          MIT License. Built by <a href="https://github.com/ehrlich-b" style={{ color: 'inherit' }}>Bryan Ehrlich</a>.
        </div>
      </footer>

      {showForgeSelector && (
        <div className="modal-overlay" onClick={() => setShowForgeSelector(false)}>
          <div className="modal forge-selector-modal" onClick={e => e.stopPropagation()}>
            <button className="modal-close" onClick={() => setShowForgeSelector(false)}>×</button>
            <h2>Get Started</h2>
            <p className="modal-subtitle">Connect your forge to start building</p>
            <div className="forge-options">
              <a href="/auth/github" className="forge-option">
                <img src={githubLogo} alt="GitHub" className="forge-option-icon github" />
                <span>GitHub</span>
              </a>
              <a href="/auth/gitlab" className="forge-option">
                <img src={gitlabLogo} alt="GitLab" className="forge-option-icon" />
                <span>GitLab</span>
              </a>
              <a href="/auth/forgejo" className="forge-option">
                <img src={forgejoLogo} alt="Codeberg" className="forge-option-icon" />
                <span>Codeberg</span>
              </a>
            </div>
            <p className="forge-note">Already have an account? This will log you in.</p>
          </div>
        </div>
      )}
    </div>
  )
}

function JobsPage({ onSelectJob }: { onSelectJob: (id: string) => void }) {
  const [jobs, setJobs] = useState<Job[]>([])
  const [allJobs, setAllJobs] = useState<Job[]>([]) // For extracting filter options
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [statusFilter, setStatusFilter] = useState('')
  const [branchFilter, setBranchFilter] = useState('')

  const fetchJobs = () => {
    setLoading(true)
    setError(null)
    const params = new URLSearchParams()
    if (statusFilter) params.set('status', statusFilter)
    if (branchFilter) params.set('branch', branchFilter)
    const url = '/api/jobs' + (params.toString() ? '?' + params.toString() : '')

    fetch(url)
      .then(r => {
        if (!r.ok) throw new Error(`Failed to load jobs (${r.status})`)
        return r.json()
      })
      .then(data => {
        const jobsList = data.jobs || []
        setJobs(jobsList)
        // If no filters, update allJobs for filter options
        if (!statusFilter && !branchFilter) {
          setAllJobs(jobsList)
        }
        setLoading(false)
      })
      .catch(e => {
        setError(e.message || 'Failed to load jobs')
        setLoading(false)
      })
  }

  useEffect(() => {
    fetchJobs()
  }, [statusFilter, branchFilter])

  // Get unique branches for filter
  const branches = Array.from(new Set(allJobs.map(j => j.branch).filter(Boolean)))

  if (loading && allJobs.length === 0) return <div className="loading">Loading...</div>
  if (error) return <ErrorState message={error} onRetry={fetchJobs} />
  if (allJobs.length === 0 && !loading) return (
    <div className="empty-state">
      <h2>No builds yet</h2>
      <p>Push to a connected repo to trigger your first build.</p>
      <div className="empty-steps">
        <div className="empty-step">
          <strong>1. Start a worker</strong>
          <code>cinch login && cinch worker</code>
        </div>
        <div className="empty-step">
          <strong>2. Add .cinch.yaml to your repo</strong>
          <code>build: make build</code>
        </div>
        <div className="empty-step">
          <strong>3. Push!</strong>
        </div>
      </div>
    </div>
  )

  return (
    <div className="jobs">
      <div className="jobs-filters">
        <select value={statusFilter} onChange={e => setStatusFilter(e.target.value)}>
          <option value="">All statuses</option>
          <option value="success">Success</option>
          <option value="failed">Failed</option>
          <option value="running">Running</option>
          <option value="pending">Pending</option>
        </select>
        <select value={branchFilter} onChange={e => setBranchFilter(e.target.value)}>
          <option value="">All branches</option>
          {branches.map(b => (
            <option key={b} value={b}>{b}</option>
          ))}
        </select>
      </div>
      {loading ? (
        <div className="loading">Loading...</div>
      ) : jobs.length === 0 ? (
        <div className="empty-state">
          <h3>No builds match filters</h3>
          <p>Try adjusting your filters.</p>
        </div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Status</th>
              <th>Repo</th>
              <th>Branch</th>
              <th>Commit</th>
              <th>Duration</th>
              <th>When</th>
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
                <td className="text-muted">{relativeTime(job.created_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

function JobDetailPage({ jobId, onBack }: { jobId: string; onBack: () => void }) {
  const [job, setJob] = useState<Job | null>(null)
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [status, setStatus] = useState<string>('')
  const [error, setError] = useState<string | null>(null)
  const [wsError, setWsError] = useState<string | null>(null)
  const logsEndRef = useRef<HTMLDivElement>(null)
  const wsRef = useRef<WebSocket | null>(null)

  const fetchJob = () => {
    setError(null)
    fetch(`/api/jobs/${jobId}`)
      .then(r => {
        if (!r.ok) throw new Error(`Failed to load job (${r.status})`)
        return r.json()
      })
      .then(data => setJob(data))
      .catch(e => setError(e.message || 'Failed to load job'))
  }

  // Fetch job details
  useEffect(() => {
    fetchJob()
  }, [jobId])

  // Connect to log stream
  useEffect(() => {
    setWsError(null)
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

    ws.onerror = () => setWsError('Lost connection to log stream')
    ws.onclose = () => {}

    return () => {
      ws.close()
    }
  }, [jobId])

  // Auto-scroll to bottom
  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  if (error) return (
    <div className="job-detail">
      <div className="job-header">
        <button onClick={onBack} className="back-btn">← Back</button>
        <h2>Job {jobId.slice(0, 12)}</h2>
      </div>
      <ErrorState message={error} onRetry={fetchJob} />
    </div>
  )

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
            <span className="text-muted">{relativeTime(job.created_at)}</span>
          </div>
        )}
      </div>
      {wsError && <div className="ws-error">{wsError}</div>}
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

// Per-repo jobs page
function RepoJobsPage({ repoPath, onSelectJob, onBack }: {
  repoPath: RepoPath
  onSelectJob: (id: string) => void
  onBack: () => void
}) {
  const [jobs, setJobs] = useState<Job[]>([])
  const [repoInfo, setRepoInfo] = useState<{ html_url?: string; private?: boolean } | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [statusFilter, setStatusFilter] = useState('')
  const [branchFilter, setBranchFilter] = useState('')

  const apiPath = `/api/repos/${repoPath.forge}/${repoPath.owner}/${repoPath.repo}`

  const fetchData = () => {
    setLoading(true)
    setError(null)

    // Fetch repo info and jobs in parallel
    Promise.all([
      fetch(apiPath).then(r => {
        if (r.status === 401) throw new Error('Login required to view this repo')
        if (r.status === 403) throw new Error('You don\'t have access to this private repo')
        if (r.status === 404) throw new Error('Repository not found')
        if (!r.ok) throw new Error(`Failed to load repo (${r.status})`)
        return r.json()
      }),
      fetch(`${apiPath}/jobs${statusFilter || branchFilter ? '?' : ''}${statusFilter ? `status=${statusFilter}` : ''}${statusFilter && branchFilter ? '&' : ''}${branchFilter ? `branch=${branchFilter}` : ''}`).then(r => {
        if (!r.ok) throw new Error(`Failed to load jobs (${r.status})`)
        return r.json()
      })
    ])
      .then(([repo, jobsData]) => {
        setRepoInfo(repo)
        setJobs(jobsData.jobs || [])
        setLoading(false)
      })
      .catch(e => {
        setError(e.message || 'Failed to load data')
        setLoading(false)
      })
  }

  useEffect(() => {
    fetchData()
  }, [repoPath.forge, repoPath.owner, repoPath.repo, statusFilter, branchFilter])

  // Get unique branches for filter dropdown
  const branches = Array.from(new Set(jobs.map(j => j.branch).filter(Boolean)))

  if (loading) return <div className="loading">Loading...</div>
  if (error) return (
    <div className="repo-jobs">
      <div className="repo-jobs-header">
        <button onClick={onBack} className="back-btn">← Back</button>
        <h2>{repoPath.owner}/{repoPath.repo}</h2>
      </div>
      <ErrorState message={error} onRetry={fetchData} />
    </div>
  )

  return (
    <div className="repo-jobs">
      <div className="repo-jobs-header">
        <button onClick={onBack} className="back-btn">← All Jobs</button>
        <div className="repo-info">
          <ForgeIcon forge={repoPath.forge.split('.')[0]} domain={repoPath.forge} />
          <h2>{repoPath.owner}/{repoPath.repo}</h2>
          {repoInfo?.private && <span className="private-badge">private</span>}
          {repoInfo?.html_url && (
            <a href={repoInfo.html_url} target="_blank" rel="noopener noreferrer" className="forge-link">
              View on {repoPath.forge}
            </a>
          )}
        </div>
      </div>

      <div className="jobs-filters">
        <select value={statusFilter} onChange={e => setStatusFilter(e.target.value)}>
          <option value="">All statuses</option>
          <option value="success">Success</option>
          <option value="failed">Failed</option>
          <option value="running">Running</option>
          <option value="pending">Pending</option>
        </select>
        <select value={branchFilter} onChange={e => setBranchFilter(e.target.value)}>
          <option value="">All branches</option>
          {branches.map(b => (
            <option key={b} value={b}>{b}</option>
          ))}
        </select>
      </div>

      {jobs.length === 0 ? (
        <div className="empty-state">
          <h3>No builds yet</h3>
          <p>Push to this repo to trigger your first build.</p>
        </div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Status</th>
              <th>Branch</th>
              <th>Commit</th>
              <th>Duration</th>
              <th>When</th>
            </tr>
          </thead>
          <tbody>
            {jobs.map(job => (
              <tr key={job.id} onClick={() => onSelectJob(job.id)} className="clickable">
                <td><StatusIcon status={job.status} /></td>
                <td>{job.branch}</td>
                <td className="mono">{job.commit?.slice(0, 7)}</td>
                <td>{formatDuration(job.duration)}</td>
                <td className="text-muted">{relativeTime(job.created_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

function WorkersPage() {
  const [workers, setWorkers] = useState<Worker[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchWorkers = () => {
    setLoading(true)
    setError(null)
    fetch('/api/workers')
      .then(r => {
        if (!r.ok) throw new Error(`Failed to load workers (${r.status})`)
        return r.json()
      })
      .then(data => {
        setWorkers(data.workers || [])
        setLoading(false)
      })
      .catch(e => {
        setError(e.message || 'Failed to load workers')
        setLoading(false)
      })
  }

  useEffect(() => {
    fetchWorkers()
  }, [])

  if (loading) return <div className="loading">Loading...</div>
  if (error) return <ErrorState message={error} onRetry={fetchWorkers} />
  if (workers.length === 0) return (
    <div className="empty-state">
      <h2>No workers connected</h2>
      <p>Workers run your builds on your hardware.</p>
      <div className="empty-steps">
        <div className="empty-step">
          <strong>Install</strong>
          <code>curl -sSL https://cinch.sh/install.sh | sh</code>
        </div>
        <div className="empty-step">
          <strong>Login & Run</strong>
          <code>cinch login && cinch worker</code>
        </div>
      </div>
    </div>
  )

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

// Badge page
function BadgesPage() {
  const [copied, setCopied] = useState(false)

  const exampleForge = 'github.com'
  const exampleOwner = 'owner'
  const exampleRepo = 'repo'
  const badgeUrl = `https://cinch.sh/badge/${exampleForge}/${exampleOwner}/${exampleRepo}.svg`
  const jobsUrl = `https://cinch.sh/jobs/${exampleForge}/${exampleOwner}/${exampleRepo}`
  const markdownSnippet = `[![build](${badgeUrl})](${jobsUrl})`

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
            src="https://img.shields.io/badge/build-passing-brightgreen"
            alt="build passing"
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
          Replace <code>github.com/owner/repo</code> with your repository. Badge links to your build history.
        </p>
      </div>
    </div>
  )
}

// Connected forge info from API
interface ConnectedForge {
  type: string
  username?: string
  connected_at?: string
}

interface UserInfo {
  id: string
  name: string
  email?: string
  connected_forges: ConnectedForge[]
  created_at: string
}

function AccountPage({ onLogout }: { onLogout: () => void }) {
  const [user, setUser] = useState<UserInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [disconnecting, setDisconnecting] = useState<string | null>(null)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const fetchUser = () => {
    setLoading(true)
    setError(null)
    fetch('/api/user')
      .then(r => {
        if (!r.ok) throw new Error(`Failed to load account (${r.status})`)
        return r.json()
      })
      .then(data => {
        setUser(data)
        setLoading(false)
      })
      .catch(e => {
        setError(e.message || 'Failed to load account')
        setLoading(false)
      })
  }

  useEffect(() => {
    fetchUser()
  }, [])

  const handleDisconnect = async (forgeType: string) => {
    const forgeCount = user?.connected_forges.length || 0

    // Warn if this is the last forge
    if (forgeCount === 1) {
      if (!confirm("This is your only login method. Disconnecting it will lock you out of your account. Are you sure?")) {
        return
      }
    } else {
      if (!confirm(`Disconnect ${forgeType}? You can reconnect later.`)) {
        return
      }
    }

    setDisconnecting(forgeType)
    try {
      const res = await fetch(`/api/user/forges/${forgeType}`, { method: 'DELETE' })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        if (data.error === 'last_login_method') {
          alert(data.message)
          return
        }
        throw new Error(data.message || `Failed to disconnect ${forgeType}`)
      }
      // Refresh user data
      fetchUser()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to disconnect')
    } finally {
      setDisconnecting(null)
    }
  }

  const handleDeleteAccount = async () => {
    setDeleting(true)
    try {
      const res = await fetch('/api/user', { method: 'DELETE' })
      if (!res.ok) {
        throw new Error('Failed to delete account')
      }
      // Redirect to logout to clear session
      onLogout()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to delete account')
      setDeleting(false)
    }
  }

  if (loading) return <div className="loading">Loading...</div>
  if (error) return <ErrorState message={error} onRetry={fetchUser} />
  if (!user) return null

  const forgeLabels: Record<string, string> = {
    github: 'GitHub',
    gitlab: 'GitLab',
    forgejo: 'Codeberg',
  }

  return (
    <div className="account-page">
      <h2>Account Settings</h2>

      <section className="account-section">
        <h3>Profile</h3>
        <div className="profile-info">
          <div className="profile-row">
            <span className="profile-label">Username</span>
            <span className="profile-value">{user.name}</span>
          </div>
          {user.email && (
            <div className="profile-row">
              <span className="profile-label">Email</span>
              <span className="profile-value">{user.email}</span>
            </div>
          )}
          <div className="profile-row">
            <span className="profile-label">Member since</span>
            <span className="profile-value">{new Date(user.created_at).toLocaleDateString()}</span>
          </div>
        </div>
      </section>

      <section className="account-section">
        <h3>Connected Accounts</h3>
        <p className="section-desc">These are the forge accounts linked to your Cinch account.</p>

        <div className="connected-forges">
          {user.connected_forges.map(forge => (
            <div key={forge.type} className="forge-row">
              <div className="forge-info">
                <ForgeIcon forge={forge.type} />
                <div className="forge-details">
                  <span className="forge-name">{forgeLabels[forge.type] || forge.type}</span>
                  {forge.username && <span className="forge-username">@{forge.username}</span>}
                  {forge.connected_at && (
                    <span className="forge-connected">Connected {relativeTime(forge.connected_at)}</span>
                  )}
                </div>
              </div>
              <button
                className="btn-disconnect"
                onClick={() => handleDisconnect(forge.type)}
                disabled={disconnecting === forge.type}
              >
                {disconnecting === forge.type ? 'Disconnecting...' : 'Disconnect'}
              </button>
            </div>
          ))}
        </div>

        <div className="add-forge">
          <span>Connect another account:</span>
          <div className="add-forge-buttons">
            {!user.connected_forges.find(f => f.type === 'gitlab') && (
              <a href="/auth/gitlab" className="btn-add-forge">+ GitLab</a>
            )}
            {!user.connected_forges.find(f => f.type === 'forgejo') && (
              <a href="/auth/forgejo" className="btn-add-forge">+ Codeberg</a>
            )}
          </div>
        </div>
      </section>

      <section className="account-section danger-zone">
        <h3>Danger Zone</h3>
        {!showDeleteConfirm ? (
          <div className="danger-action">
            <div>
              <strong>Delete Account</strong>
              <p>Permanently delete your Cinch account and all associated data.</p>
            </div>
            <button className="btn-danger" onClick={() => setShowDeleteConfirm(true)}>
              Delete Account
            </button>
          </div>
        ) : (
          <div className="delete-confirm">
            <p><strong>Are you absolutely sure?</strong></p>
            <p>This action cannot be undone. All your data will be permanently deleted.</p>
            <div className="confirm-actions">
              <button onClick={() => setShowDeleteConfirm(false)}>Cancel</button>
              <button
                className="btn-danger"
                onClick={handleDeleteAccount}
                disabled={deleting}
              >
                {deleting ? 'Deleting...' : 'Yes, delete my account'}
              </button>
            </div>
          </div>
        )}
      </section>
    </div>
  )
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="error-state">
      <div className="error-icon">!</div>
      <h3>Something went wrong</h3>
      <p>{message}</p>
      <button onClick={onRetry} className="retry-btn">Try again</button>
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

function ForgeIcon({ forge, cloneUrl, domain }: { forge: string; cloneUrl?: string; domain?: string }) {
  // Determine the host - prefer explicit domain, then extract from cloneUrl
  const host = domain || (cloneUrl ? getDomainFromURL(cloneUrl) : null)

  // Check if this is Codeberg (forgejo at codeberg.org)
  if (forge === 'forgejo' && host === 'codeberg.org') {
    return <img src={codebergLogo} alt="Codeberg" className="forge-icon" />
  }

  switch (forge) {
    case 'github':
      return <img src={githubLogo} alt="GitHub" className="forge-icon github" />
    case 'gitlab':
      return <img src={gitlabLogo} alt="GitLab" className="forge-icon" />
    case 'gitea':
      return <img src={giteaLogo} alt="Gitea" className="forge-icon" />
    case 'forgejo':
      return <img src={forgejoLogo} alt="Forgejo" className="forge-icon" />
    default:
      return <span className="forge-text">{forge}</span>
  }
}

function formatDuration(ms?: number): string {
  if (!ms) return '-'
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${seconds % 60}s`
}

function relativeTime(dateStr?: string): string {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffSecs = Math.floor(diffMs / 1000)

  if (diffSecs < 5) return 'just now'
  if (diffSecs < 60) return `${diffSecs}s ago`

  const diffMins = Math.floor(diffSecs / 60)
  if (diffMins < 60) return `${diffMins}m ago`

  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`

  const diffDays = Math.floor(diffHours / 24)
  if (diffDays < 7) return `${diffDays}d ago`

  // Fallback to date
  return date.toLocaleDateString()
}

// Basic ANSI escape code renderer
function renderAnsi(text: string): string {
  // Strip ANSI codes for now - basic implementation
  // eslint-disable-next-line no-control-regex
  return text.replace(/\x1b\[[0-9;]*m/g, '')
}

// Helper to extract domain from clone URL
function getDomainFromURL(url: string): string | null {
  // Handle https://github.com/owner/repo.git
  const httpsMatch = url.match(/https?:\/\/([^/]+)/)
  if (httpsMatch) return httpsMatch[1].toLowerCase()

  // Handle git@github.com:owner/repo.git
  const sshMatch = url.match(/git@([^:]+):/)
  if (sshMatch) return sshMatch[1].toLowerCase()

  return null
}

// Helper to get forge domain - prefers extracting from clone_url
function forgeToDomain(forgeType: string, cloneUrl?: string): string {
  // If we have a clone URL, extract the actual domain
  if (cloneUrl) {
    const domain = getDomainFromURL(cloneUrl)
    if (domain) return domain
  }

  // Fallback to static mapping for default hosted services
  switch (forgeType) {
    case 'github': return 'github.com'
    case 'gitlab': return 'gitlab.com'
    case 'forgejo': return 'codeberg.org'
    case 'gitea': return 'gitea.com'
    default: return forgeType
  }
}


// Repos page
function ReposPage({ onAddGitLab, onAddForgejo, onSelectRepo }: {
  onAddGitLab: () => void
  onAddForgejo: () => void
  onSelectRepo?: (repoPath: RepoPath) => void
}) {
  const [repos, setRepos] = useState<Repo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchRepos = () => {
    setLoading(true)
    setError(null)
    fetch('/api/repos?include_status=true')
      .then(r => {
        if (!r.ok) throw new Error(`Failed to load repos (${r.status})`)
        return r.json()
      })
      .then(data => {
        setRepos(data || [])
        setLoading(false)
      })
      .catch(e => {
        setError(e.message || 'Failed to load repos')
        setLoading(false)
      })
  }

  useEffect(() => {
    fetchRepos()
  }, [])

  const handleRepoClick = (repo: Repo) => {
    if (onSelectRepo) {
      onSelectRepo({
        forge: forgeToDomain(repo.forge_type, repo.clone_url),
        owner: repo.owner,
        repo: repo.name
      })
    }
  }

  if (loading) return <div className="loading">Loading...</div>
  if (error) return <ErrorState message={error} onRetry={fetchRepos} />

  return (
    <div className="repos-page">
      <div className="repos-header">
        <h2>Connected Repositories</h2>
        <div className="add-repo-buttons">
          <button className="btn-add-repo gitlab" onClick={onAddGitLab}>
            Add GitLab Repo
          </button>
          <button className="btn-add-repo forgejo" onClick={onAddForgejo}>
            Add Codeberg Repo
          </button>
        </div>
      </div>
      {repos.length === 0 ? (
        <div className="empty-state">
          <h3>No repositories connected</h3>
          <p>Connect a repository to start building.</p>
        </div>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Status</th>
              <th></th>
              <th>Repository</th>
              <th>Added</th>
            </tr>
          </thead>
          <tbody>
            {repos.map(repo => (
              <tr key={repo.id} onClick={() => handleRepoClick(repo)} className="clickable">
                <td><StatusIcon status={repo.latest_job_status || ''} /></td>
                <td className="forge-icon"><ForgeIcon forge={repo.forge_type} cloneUrl={repo.clone_url} /></td>
                <td>
                  {repo.owner}/{repo.name}
                  {repo.private && <span className="private-badge">private</span>}
                </td>
                <td className="text-muted">{relativeTime(repo.created_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

// Success page after onboarding
function SuccessPage({ onContinue }: { onContinue: () => void }) {
  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text)
  }

  return (
    <div className="success-page">
      <div className="success-container">
        <div className="success-header">
          <div className="success-icon">✓</div>
          <h1>You're all set!</h1>
          <p>Your repositories are connected. Now set up a worker to start building.</p>
        </div>

        <div className="setup-steps">
          <div className="setup-step">
            <div className="step-number">1</div>
            <div className="step-content">
              <h3>Install Cinch</h3>
              <div className="code-block">
                <code>curl -sSL https://cinch.sh/install.sh | sh</code>
                <button className="copy-btn" onClick={() => copyToClipboard('curl -sSL https://cinch.sh/install.sh | sh')}>
                  Copy
                </button>
              </div>
            </div>
          </div>

          <div className="setup-step">
            <div className="step-number">2</div>
            <div className="step-content">
              <h3>Login & Start Worker</h3>
              <div className="code-block">
                <code>cinch login && cinch worker --all</code>
                <button className="copy-btn" onClick={() => copyToClipboard('cinch login && cinch worker --all')}>
                  Copy
                </button>
              </div>
              <p className="step-note">
                The <code>--all</code> flag builds all your connected repos. Leave it running!
              </p>
            </div>
          </div>

          <div className="setup-step">
            <div className="step-number">3</div>
            <div className="step-content">
              <h3>Push Code</h3>
              <p>
                Add <code>.cinch.yaml</code> to your repo with your build command:
              </p>
              <div className="code-block yaml-block">
                <code>build: make build</code>
              </div>
              <p className="step-note">Push and watch the build run!</p>
            </div>
          </div>
        </div>

        <div className="success-actions">
          <button className="btn-primary" onClick={onContinue}>
            Go to Dashboard
          </button>
        </div>
      </div>
    </div>
  )
}

// GitLab project from API
interface GitLabProject {
  id: number
  name: string
  path_with_namespace: string
  web_url: string
  visibility: string
}

// Full-page GitLab onboarding
function GitLabOnboardPage({ onComplete, onCancel }: { onComplete: () => void; onCancel: () => void }) {
  const [projects, setProjects] = useState<GitLabProject[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedProjects, setSelectedProjects] = useState<Set<number>>(new Set())
  const [setting, setSetting] = useState(false)
  const [setupProgress, setSetupProgress] = useState({ current: 0, total: 0 })
  const [tokenChoice, setTokenChoice] = useState<'manual' | 'oauth' | null>(null)
  const [tokenInput, setTokenInput] = useState('')
  const [needsToken, setNeedsToken] = useState(false)
  const [pendingProject, setPendingProject] = useState<GitLabProject | null>(null)

  useEffect(() => {
    fetch('/api/gitlab/projects')
      .then(r => {
        if (r.status === 401) {
          // Not connected to GitLab, redirect to OAuth
          window.location.href = '/auth/gitlab'
          return []
        }
        if (!r.ok) throw new Error(`Failed to load projects (${r.status})`)
        return r.json()
      })
      .then(data => {
        setProjects(Array.isArray(data) ? data : data.projects || [])
        setLoading(false)
      })
      .catch(e => {
        setError(e.message || 'Failed to load projects')
        setLoading(false)
      })
  }, [])

  const toggleProject = (projectId: number) => {
    setSelectedProjects(prev => {
      const next = new Set(prev)
      if (next.has(projectId)) next.delete(projectId)
      else next.add(projectId)
      return next
    })
  }

  const selectAll = () => setSelectedProjects(new Set(projects.map(p => p.id)))
  const selectNone = () => setSelectedProjects(new Set())

  const handleSetup = async () => {
    const selectedList = projects.filter(p => selectedProjects.has(p.id))
    if (selectedList.length === 0) return

    setSetting(true)
    setError(null)
    setSetupProgress({ current: 0, total: selectedList.length })

    for (let i = 0; i < selectedList.length; i++) {
      const project = selectedList[i]
      setSetupProgress({ current: i + 1, total: selectedList.length })

      try {
        const res = await fetch('/api/gitlab/setup', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            project_id: project.id,
            project_path: project.path_with_namespace,
          }),
        })
        const data = await res.json()

        if (data.status === 'needs_token' && !pendingProject) {
          setPendingProject(project)
          setNeedsToken(true)
          setSetting(false)
          return // Stop here and show token prompt
        }
      } catch (e) {
        console.error(`Failed to setup ${project.path_with_namespace}:`, e)
      }
    }

    setSetting(false)
    onComplete()
  }

  const handleTokenSubmit = async () => {
    if (!pendingProject) return
    setSetting(true)

    try {
      const body: Record<string, unknown> = {
        project_id: pendingProject.id,
        project_path: pendingProject.path_with_namespace,
      }
      if (tokenChoice === 'oauth') body.use_oauth = true
      if (tokenChoice === 'manual') body.manual_token = tokenInput.trim()

      const res = await fetch('/api/gitlab/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data = await res.json()

      if (data.status === 'success') {
        onComplete()
      } else if (data.error) {
        setError(data.error)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Setup failed')
    } finally {
      setSetting(false)
    }
  }

  if (loading) {
    return (
      <div className="onboard-page">
        <div className="onboard-container">
          <h1>Loading your GitLab projects...</h1>
        </div>
      </div>
    )
  }

  if (error && !needsToken) {
    return (
      <div className="onboard-page">
        <div className="onboard-container">
          <h1>Something went wrong</h1>
          <p className="error-msg">{error}</p>
          <button onClick={onCancel}>Back to Repos</button>
        </div>
      </div>
    )
  }

  if (needsToken && pendingProject) {
    return (
      <div className="onboard-page">
        <div className="onboard-container">
          <h1>One More Step</h1>
          <p>GitLab free tier doesn't allow automated token creation for <strong>{pendingProject.path_with_namespace}</strong>.</p>
          <p>Choose how to authenticate for status updates:</p>

          <div className="token-options">
            <label className="token-option">
              <input
                type="radio"
                checked={tokenChoice === 'manual'}
                onChange={() => setTokenChoice('manual')}
              />
              <span>Create a token manually (recommended)</span>
            </label>
            <label className="token-option">
              <input
                type="radio"
                checked={tokenChoice === 'oauth'}
                onChange={() => setTokenChoice('oauth')}
              />
              <span>Use my current session</span>
            </label>
          </div>

          {tokenChoice === 'manual' && (
            <div className="manual-token-input">
              <p>
                Create a Project Access Token at:{' '}
                <a href={`${pendingProject.web_url}/-/settings/access_tokens`} target="_blank" rel="noopener noreferrer">
                  {pendingProject.path_with_namespace} settings
                </a>
              </p>
              <p className="token-instructions">Required scope: <code>api</code></p>
              <input
                type="password"
                placeholder="glpat-xxxxxxxxxxxx"
                value={tokenInput}
                onChange={e => setTokenInput(e.target.value)}
              />
            </div>
          )}

          {tokenChoice === 'oauth' && (
            <p className="oauth-warning">
              Status updates will appear as coming from you, and the token will expire periodically.
            </p>
          )}

          {error && <div className="error-msg">{error}</div>}

          <div className="onboard-actions">
            <button onClick={onCancel}>Cancel</button>
            <button
              className="primary"
              disabled={!tokenChoice || (tokenChoice === 'manual' && !tokenInput.trim()) || setting}
              onClick={handleTokenSubmit}
            >
              {setting ? 'Finishing...' : 'Finish Setup'}
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="onboard-page">
      <div className="onboard-container">
        <h1>Select GitLab Repositories</h1>
        <p>Choose which repositories to connect to Cinch</p>

        <div className="project-actions">
          <button className="btn-small" onClick={selectAll}>Select All</button>
          <button className="btn-small" onClick={selectNone}>Select None</button>
          <span className="selection-count">{selectedProjects.size} of {projects.length} selected</span>
        </div>

        <div className="project-list onboard-list">
          {projects.map(p => (
            <label key={p.id} className={`project-item checkbox ${selectedProjects.has(p.id) ? 'selected' : ''}`}>
              <input
                type="checkbox"
                checked={selectedProjects.has(p.id)}
                onChange={() => toggleProject(p.id)}
              />
              <span className="project-name">{p.path_with_namespace}</span>
              <span className="project-visibility">{p.visibility}</span>
            </label>
          ))}
        </div>

        {setting && (
          <div className="setup-progress">
            Setting up {setupProgress.current} of {setupProgress.total}...
          </div>
        )}

        <div className="onboard-actions">
          <button onClick={onCancel}>Cancel</button>
          <button
            className="primary"
            disabled={selectedProjects.size === 0 || setting}
            onClick={handleSetup}
          >
            {setting ? 'Setting up...' : `Connect ${selectedProjects.size} ${selectedProjects.size === 1 ? 'Repository' : 'Repositories'}`}
          </button>
        </div>
      </div>
    </div>
  )
}

// Forgejo/Codeberg repo
interface ForgejoRepo {
  id: number
  name: string
  full_name: string
  html_url: string
  private: boolean
  owner: { login: string }
}

// Full-page Forgejo onboarding (Codeberg)
function ForgejoOnboardPage({ onComplete, onCancel }: { onComplete: () => void; onCancel: () => void }) {
  const [repos, setRepos] = useState<ForgejoRepo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedRepos, setSelectedRepos] = useState<Set<number>>(new Set())
  const [setting, setSetting] = useState(false)
  const [setupProgress, setSetupProgress] = useState({ current: 0, total: 0, currentRepo: '' })
  const [tokenInput, setTokenInput] = useState('')
  const [needsToken, setNeedsToken] = useState(false)
  const [pendingRepo, setPendingRepo] = useState<ForgejoRepo | null>(null)
  const [tokenUrl, setTokenUrl] = useState('')

  useEffect(() => {
    fetch('/api/forgejo/repos')
      .then(r => {
        if (r.status === 401) {
          window.location.href = '/auth/forgejo'
          return []
        }
        if (!r.ok) throw new Error(`Failed to load repos (${r.status})`)
        return r.json()
      })
      .then(data => {
        setRepos(Array.isArray(data) ? data : [])
        setLoading(false)
      })
      .catch(e => {
        setError(e.message || 'Failed to load repos')
        setLoading(false)
      })
  }, [])

  const toggleRepo = (repoId: number) => {
    setSelectedRepos(prev => {
      const next = new Set(prev)
      if (next.has(repoId)) next.delete(repoId)
      else next.add(repoId)
      return next
    })
  }

  const selectAll = () => setSelectedRepos(new Set(repos.map(r => r.id)))
  const selectNone = () => setSelectedRepos(new Set())

  const handleSetup = async () => {
    const selectedList = repos.filter(r => selectedRepos.has(r.id))
    if (selectedList.length === 0) return

    setSetting(true)
    setError(null)
    setSetupProgress({ current: 0, total: selectedList.length, currentRepo: '' })

    for (let i = 0; i < selectedList.length; i++) {
      const repo = selectedList[i]
      setSetupProgress({ current: i + 1, total: selectedList.length, currentRepo: repo.full_name })

      try {
        const res = await fetch('/api/forgejo/setup', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            owner: repo.owner.login,
            name: repo.name,
          }),
        })
        const data = await res.json()

        if (data.status === 'needs_token' && !pendingRepo) {
          setPendingRepo(repo)
          setTokenUrl(data.token_url || '')
          setNeedsToken(true)
          setSetting(false)
          return // Stop and ask for token
        }
      } catch (e) {
        console.error(`Failed to setup ${repo.full_name}:`, e)
      }
    }

    setSetting(false)
    onComplete()
  }

  const handleTokenSubmit = async () => {
    if (!pendingRepo || !tokenInput.trim()) return
    setSetting(true)

    try {
      const res = await fetch('/api/forgejo/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          owner: pendingRepo.owner.login,
          name: pendingRepo.name,
          manual_token: tokenInput.trim(),
        }),
      })
      const data = await res.json()

      if (data.status === 'success') {
        onComplete()
      } else if (data.error) {
        setError(data.error)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Setup failed')
    } finally {
      setSetting(false)
    }
  }

  if (loading) {
    return (
      <div className="onboard-page">
        <div className="onboard-container">
          <h1>Loading your Codeberg repositories...</h1>
        </div>
      </div>
    )
  }

  if (error && !needsToken) {
    return (
      <div className="onboard-page">
        <div className="onboard-container">
          <h1>Something went wrong</h1>
          <p className="error-msg">{error}</p>
          <button onClick={onCancel}>Back to Repos</button>
        </div>
      </div>
    )
  }

  if (needsToken && pendingRepo) {
    return (
      <div className="onboard-page">
        <div className="onboard-container">
          <h1>One More Step</h1>
          <p>Webhook created for <strong>{pendingRepo.full_name}</strong>!</p>
          <p>Now we need a token for posting build status.</p>

          <div className="manual-token-input">
            <p>
              Create a Personal Access Token at:{' '}
              <a href={tokenUrl} target="_blank" rel="noopener noreferrer">
                Codeberg Settings → Applications
              </a>
            </p>
            <p className="token-instructions">Required scope: <code>repository</code> (read & write)</p>
            <input
              type="password"
              placeholder="Paste your token here"
              value={tokenInput}
              onChange={e => setTokenInput(e.target.value)}
            />
          </div>

          {error && <div className="error-msg">{error}</div>}

          <div className="onboard-actions">
            <button onClick={onCancel}>Cancel</button>
            <button
              className="primary"
              disabled={!tokenInput.trim() || setting}
              onClick={handleTokenSubmit}
            >
              {setting ? 'Finishing...' : 'Finish Setup'}
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="onboard-page">
      <div className="onboard-container">
        <h1>Select Codeberg Repositories</h1>
        <p>Choose which repositories to connect to Cinch</p>

        <div className="project-actions">
          <button className="btn-small" onClick={selectAll}>Select All</button>
          <button className="btn-small" onClick={selectNone}>Select None</button>
          <span className="selection-count">{selectedRepos.size} of {repos.length} selected</span>
        </div>

        <div className="project-list onboard-list">
          {repos.map(r => (
            <label key={r.id} className={`project-item checkbox ${selectedRepos.has(r.id) ? 'selected' : ''}`}>
              <input
                type="checkbox"
                checked={selectedRepos.has(r.id)}
                onChange={() => toggleRepo(r.id)}
              />
              <span className="project-name">{r.full_name}</span>
              <span className="project-visibility">{r.private ? 'private' : 'public'}</span>
            </label>
          ))}
        </div>

        {setting && (
          <div className="setup-progress">
            Setting up {setupProgress.current} of {setupProgress.total}: {setupProgress.currentRepo}
          </div>
        )}

        <div className="onboard-actions">
          <button onClick={onCancel}>Cancel</button>
          <button
            className="primary"
            disabled={selectedRepos.size === 0 || setting}
            onClick={handleSetup}
          >
            {setting ? 'Setting up...' : `Connect ${selectedRepos.size} ${selectedRepos.size === 1 ? 'Repository' : 'Repositories'}`}
          </button>
        </div>
      </div>
    </div>
  )
}

// GitLab setup modal with multi-select support
function GitLabSetupModal({
  mode,
  onClose,
  onComplete,
  onNeedToken,
}: {
  mode: 'select-project' | 'token-choice'
  onClose: () => void
  onComplete: () => void
  onNeedToken: () => void
}) {
  const [projects, setProjects] = useState<GitLabProject[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedProjects, setSelectedProjects] = useState<Set<number>>(new Set())
  const [setting, setSetting] = useState(false)
  const [setupProgress, setSetupProgress] = useState({ current: 0, total: 0 })
  const [tokenInput, setTokenInput] = useState('')
  const [tokenChoice, setTokenChoice] = useState<'manual' | 'oauth' | null>(null)
  const [setupOptions, setSetupOptions] = useState<{ id: string; label: string }[]>([])
  const [pendingProject, setPendingProject] = useState<GitLabProject | null>(null)

  // Fetch projects when in select-project mode
  useEffect(() => {
    if (mode !== 'select-project') return

    fetch('/api/gitlab/projects')
      .then(r => {
        if (!r.ok) throw new Error(`Failed to load projects (${r.status})`)
        return r.json()
      })
      .then(data => {
        // API returns array directly, not wrapped in object
        setProjects(Array.isArray(data) ? data : data.projects || [])
        setLoading(false)
      })
      .catch(e => {
        setError(e.message || 'Failed to load projects')
        setLoading(false)
      })
  }, [mode])

  const toggleProject = (projectId: number) => {
    setSelectedProjects(prev => {
      const next = new Set(prev)
      if (next.has(projectId)) {
        next.delete(projectId)
      } else {
        next.add(projectId)
      }
      return next
    })
  }

  const selectAll = () => {
    setSelectedProjects(new Set(projects.map(p => p.id)))
  }

  const selectNone = () => {
    setSelectedProjects(new Set())
  }

  const handleSetupMultiple = async () => {
    const selectedList = projects.filter(p => selectedProjects.has(p.id))
    if (selectedList.length === 0) return

    setSetting(true)
    setError(null)
    setSetupProgress({ current: 0, total: selectedList.length })

    let successCount = 0
    let needsTokenProject: GitLabProject | null = null

    for (let i = 0; i < selectedList.length; i++) {
      const project = selectedList[i]
      setSetupProgress({ current: i + 1, total: selectedList.length })

      try {
        const res = await fetch('/api/gitlab/setup', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            project_id: project.id,
            project_path: project.path_with_namespace,
          }),
        })

        const data = await res.json()

        if (data.status === 'success') {
          successCount++
        } else if (data.status === 'needs_token') {
          // PAT creation failed (free tier), remember for later
          if (!needsTokenProject) {
            needsTokenProject = project
            setSetupOptions(data.options || [])
          }
        } else if (data.error) {
          console.error(`Failed to setup ${project.path_with_namespace}:`, data.error)
        }
      } catch (e) {
        console.error(`Failed to setup ${project.path_with_namespace}:`, e)
      }
    }

    setSetting(false)

    if (needsTokenProject) {
      // Need manual token for at least one project
      setPendingProject(needsTokenProject)
      onNeedToken()
    } else if (successCount > 0) {
      onComplete()
    } else {
      setError('Failed to setup any repositories')
    }
  }

  const handleSingleSetup = async (useOAuth = false, manualToken = '') => {
    const project = pendingProject
    if (!project) return

    setSetting(true)
    setError(null)

    try {
      const body: Record<string, unknown> = {
        project_id: project.id,
        project_path: project.path_with_namespace,
      }
      if (useOAuth) body.use_oauth = true
      if (manualToken) body.manual_token = manualToken

      const res = await fetch('/api/gitlab/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })

      const data = await res.json()

      if (data.status === 'success') {
        onComplete()
      } else if (data.error) {
        setError(data.error)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Setup failed')
    } finally {
      setSetting(false)
    }
  }

  const handleTokenSubmit = () => {
    if (tokenChoice === 'oauth') {
      handleSingleSetup(true)
    } else if (tokenChoice === 'manual' && tokenInput.trim()) {
      handleSingleSetup(false, tokenInput.trim())
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal modal-large" onClick={e => e.stopPropagation()}>
        <button className="modal-close" onClick={onClose}>x</button>

        {mode === 'select-project' && (
          <>
            <h2>Select GitLab Repositories</h2>
            <p className="modal-subtitle">Choose which repositories to connect to Cinch</p>
            {loading && <div className="loading">Loading projects...</div>}
            {error && <div className="error-msg">{error}</div>}
            {!loading && !error && (
              <>
                <div className="project-actions">
                  <button className="btn-small" onClick={selectAll}>Select All</button>
                  <button className="btn-small" onClick={selectNone}>Select None</button>
                  <span className="selection-count">
                    {selectedProjects.size} of {projects.length} selected
                  </span>
                </div>
                <div className="project-list">
                  {projects.map(p => (
                    <label
                      key={p.id}
                      className={`project-item checkbox ${selectedProjects.has(p.id) ? 'selected' : ''}`}
                    >
                      <input
                        type="checkbox"
                        checked={selectedProjects.has(p.id)}
                        onChange={() => toggleProject(p.id)}
                      />
                      <span className="project-name">{p.path_with_namespace}</span>
                      <span className="project-visibility">{p.visibility}</span>
                    </label>
                  ))}
                </div>
                {setting && (
                  <div className="setup-progress">
                    Setting up {setupProgress.current} of {setupProgress.total}...
                  </div>
                )}
                <div className="modal-actions">
                  <button onClick={onClose}>Cancel</button>
                  <button
                    className="primary"
                    disabled={selectedProjects.size === 0 || setting}
                    onClick={handleSetupMultiple}
                  >
                    {setting ? `Setting up...` : `Connect ${selectedProjects.size} ${selectedProjects.size === 1 ? 'Repository' : 'Repositories'}`}
                  </button>
                </div>
              </>
            )}
          </>
        )}

        {mode === 'token-choice' && pendingProject && (
          <>
            <h2>One More Step</h2>
            <p className="modal-desc">
              GitLab free tier doesn't allow automated token creation for <strong>{pendingProject.path_with_namespace}</strong>.
              Choose how to authenticate for status updates:
            </p>

            <div className="token-options">
              {setupOptions.map(opt => (
                <label key={opt.id} className="token-option">
                  <input
                    type="radio"
                    name="token-choice"
                    checked={tokenChoice === opt.id}
                    onChange={() => setTokenChoice(opt.id as 'manual' | 'oauth')}
                  />
                  <span>{opt.label}</span>
                </label>
              ))}
            </div>

            {tokenChoice === 'manual' && (
              <div className="manual-token-input">
                <p>
                  Create a Project Access Token at:{' '}
                  <a
                    href={`${pendingProject.web_url}/-/settings/access_tokens`}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {pendingProject.path_with_namespace} settings
                  </a>
                </p>
                <p className="token-instructions">
                  Required scope: <code>api</code> (for status updates)
                </p>
                <input
                  type="password"
                  placeholder="glpat-xxxxxxxxxxxx"
                  value={tokenInput}
                  onChange={e => setTokenInput(e.target.value)}
                />
              </div>
            )}

            {tokenChoice === 'oauth' && (
              <p className="oauth-warning">
                Using your session means status updates will appear as coming from you,
                and the token will expire periodically.
              </p>
            )}

            {error && <div className="error-msg">{error}</div>}

            <div className="modal-actions">
              <button onClick={onClose}>Cancel</button>
              <button
                className="primary"
                disabled={!tokenChoice || (tokenChoice === 'manual' && !tokenInput.trim()) || setting}
                onClick={handleTokenSubmit}
              >
                {setting ? 'Finishing setup...' : 'Finish Setup'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

interface Repo {
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

interface Job {
  id: string
  repo: string
  branch: string
  commit: string
  status: string
  duration?: number
  created_at?: string
  started_at?: string
  finished_at?: string
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
