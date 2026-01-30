import { useState, useEffect } from 'react'
import { ErrorState } from '../components/ErrorState'
import { StatusIcon } from '../components/StatusIcon'
import { ForgeIcon } from '../components/ForgeIcon'
import { formatDuration, relativeTime } from '../utils/format'
import type { Job, RepoPath } from '../types'

function BadgeSection({ repoPath }: { repoPath: RepoPath }) {
  const [copied, setCopied] = useState(false)
  const [showBadge, setShowBadge] = useState(false)

  const badgeUrl = `https://cinch.sh/badge/${repoPath.forge}/${repoPath.owner}/${repoPath.repo}.svg`
  const jobsUrl = `https://cinch.sh/jobs/${repoPath.forge}/${repoPath.owner}/${repoPath.repo}`
  const markdownSnippet = `[![build](${badgeUrl})](${jobsUrl})`

  const copyToClipboard = () => {
    navigator.clipboard.writeText(markdownSnippet)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="badge-section">
      <button className="badge-toggle" onClick={() => setShowBadge(!showBadge)}>
        {showBadge ? 'Hide' : 'Show'} README badge
      </button>
      {showBadge && (
        <div className="badge-content">
          <div className="badge-preview">
            <img src={badgeUrl} alt="build status" />
          </div>
          <div className="badge-code">
            <code>{markdownSnippet}</code>
            <button onClick={copyToClipboard} className="copy-btn">
              {copied ? 'Copied' : 'Copy'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

interface Props {
  repoPath: RepoPath
  onSelectJob: (id: string) => void
  onBack: () => void
}

export function RepoJobsPage({ repoPath, onSelectJob, onBack }: Props) {
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
        <BadgeSection repoPath={repoPath} />
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
