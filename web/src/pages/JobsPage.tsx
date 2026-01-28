import { useState, useEffect } from 'react'
import { ErrorState } from '../components/ErrorState'
import { StatusIcon } from '../components/StatusIcon'
import { formatDuration, relativeTime } from '../utils/format'
import type { Job } from '../types'

interface Props {
  onSelectJob: (id: string) => void
}

export function JobsPage({ onSelectJob }: Props) {
  const [jobs, setJobs] = useState<Job[]>([])
  const [allJobs, setAllJobs] = useState<Job[]>([])
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
                <td>
                  {job.pr_number ? (
                    <span title={`${job.branch} → ${job.pr_base_branch}`}>
                      PR #{job.pr_number}
                    </span>
                  ) : job.branch}
                </td>
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
