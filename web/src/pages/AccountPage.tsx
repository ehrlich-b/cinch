import { useState, useEffect } from 'react'
import { ErrorState } from '../components/ErrorState'
import { ForgeIcon } from '../components/ForgeIcon'
import { relativeTime } from '../utils/format'
import type { UserInfo } from '../types'

interface Props {
  onLogout: () => void
}

export function AccountPage({ onLogout }: Props) {
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
