import { useState, useEffect } from 'react'
import { ErrorState } from '../components/ErrorState'
import { StatusIcon } from '../components/StatusIcon'
import { ForgeIcon } from '../components/ForgeIcon'
import { relativeTime } from '../utils/format'
import { forgeToDomain } from '../utils/url'
import type { Repo, RepoPath } from '../types'

interface Props {
  onAddGitLab: () => void
  onAddForgejo: () => void
  onSelectRepo?: (repoPath: RepoPath) => void
}

export function ReposPage({ onAddGitLab, onAddForgejo, onSelectRepo }: Props) {
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
