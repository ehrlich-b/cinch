import { useState, useEffect } from 'react'
import { ErrorState } from '../components/ErrorState'
import type { Worker } from '../types'

export function WorkersPage() {
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
