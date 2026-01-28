import { useState, useEffect } from 'react'
import type { GitLabProject } from '../../types'

interface Props {
  onComplete: () => void
  onCancel: () => void
}

export function GitLabOnboardPage({ onComplete, onCancel }: Props) {
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
          return
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
