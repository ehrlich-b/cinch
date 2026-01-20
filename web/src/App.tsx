import { useState, useEffect } from 'react'

type Page = 'jobs' | 'workers' | 'settings'

export function App() {
  const [page, setPage] = useState<Page>('jobs')

  return (
    <div className="app">
      <header>
        <h1>Cinch</h1>
        <nav>
          <button
            className={page === 'jobs' ? 'active' : ''}
            onClick={() => setPage('jobs')}
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
        {page === 'jobs' && <JobsPage />}
        {page === 'workers' && <WorkersPage />}
        {page === 'settings' && <SettingsPage />}
      </main>
    </div>
  )
}

function JobsPage() {
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
            <tr key={job.id}>
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
    case 'failure': return <span className="status failure">✗</span>
    case 'running': return <span className="status running">◐</span>
    case 'pending': return <span className="status pending">◷</span>
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
