import { useState, useEffect } from 'react'
import type { ForgejoRepo } from '../../types'

interface Props {
  onComplete: () => void
  onCancel: () => void
}

export function ForgejoOnboardPage({ onComplete, onCancel }: Props) {
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
          return
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
