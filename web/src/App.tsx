import { useState, useEffect, useRef } from 'react'

type Page = 'jobs' | 'workers' | 'settings'

export function App() {
  const [page, setPage] = useState<Page>('jobs')
  const [selectedJob, setSelectedJob] = useState<string | null>(null)

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
        </nav>
      </header>
      <main>
        {page === 'jobs' && !selectedJob && <JobsPage onSelectJob={setSelectedJob} />}
        {page === 'jobs' && selectedJob && (
          <JobDetailPage jobId={selectedJob} onBack={() => setSelectedJob(null)} />
        )}
        {page === 'workers' && <WorkersPage />}
        {page === 'settings' && <SettingsPage />}
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
