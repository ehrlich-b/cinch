import { useState, useEffect, useRef, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { ErrorState } from '../components/ErrorState'
import type { Worker, WorkerEvent } from '../types'

export function WorkersPage() {
  const [workers, setWorkers] = useState<Map<string, Worker>>(new Map())
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionLoading, setActionLoading] = useState<string | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  const fetchWorkers = useCallback(() => {
    setLoading(true)
    setError(null)
    fetch('/api/workers')
      .then(r => {
        if (!r.ok) throw new Error(`Failed to load workers (${r.status})`)
        return r.json()
      })
      .then(data => {
        const workerMap = new Map<string, Worker>()
        for (const w of (data.workers || [])) {
          workerMap.set(w.id, w)
        }
        setWorkers(workerMap)
        setLoading(false)
      })
      .catch(e => {
        setError(e.message || 'Failed to load workers')
        setLoading(false)
      })
  }, [])

  // WebSocket connection for live updates
  useEffect(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws/workers`)
    wsRef.current = ws

    ws.onopen = () => {
      console.log('Worker stream connected')
    }

    ws.onmessage = (e) => {
      try {
        const event: WorkerEvent = JSON.parse(e.data)
        handleEvent(event)
      } catch (err) {
        console.error('Failed to parse worker event:', err)
      }
    }

    ws.onerror = (err) => {
      console.error('Worker stream error:', err)
    }

    ws.onclose = () => {
      console.log('Worker stream disconnected')
    }

    return () => {
      ws.close()
    }
  }, [])

  const handleEvent = (event: WorkerEvent) => {
    setWorkers(prev => {
      const next = new Map(prev)

      switch (event.type) {
        case 'connected':
          if (event.worker) {
            next.set(event.worker_id, event.worker)
          }
          break

        case 'disconnected':
          if (next.has(event.worker_id)) {
            const worker = next.get(event.worker_id)!
            next.set(event.worker_id, { ...worker, connected: false })
          }
          break

        case 'job_started':
          if (next.has(event.worker_id) && event.job_id) {
            const worker = next.get(event.worker_id)!
            const activeJobs = [...(worker.active_jobs || []), event.job_id]
            next.set(event.worker_id, {
              ...worker,
              active_jobs: activeJobs,
              currentJob: activeJobs[0]
            })
          }
          break

        case 'job_finished':
          if (next.has(event.worker_id) && event.job_id) {
            const worker = next.get(event.worker_id)!
            const activeJobs = (worker.active_jobs || []).filter(id => id !== event.job_id)
            next.set(event.worker_id, {
              ...worker,
              active_jobs: activeJobs,
              currentJob: activeJobs[0] || undefined
            })
          }
          break
      }

      return next
    })
  }

  // Initial fetch (WebSocket will send current state too, but this is a backup)
  useEffect(() => {
    fetchWorkers()
  }, [fetchWorkers])

  const handleDrain = async (workerId: string) => {
    if (!confirm('Gracefully shut down this worker? It will finish current jobs first.')) {
      return
    }

    setActionLoading(workerId)
    try {
      const resp = await fetch(`/api/workers/${workerId}/drain`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ timeout: 300 })
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(text || `Failed (${resp.status})`)
      }
    } catch (e: any) {
      alert(`Failed to drain worker: ${e.message}`)
    } finally {
      setActionLoading(null)
    }
  }

  const handleDisconnect = async (workerId: string) => {
    if (!confirm('Force disconnect this worker? Running jobs will be cancelled.')) {
      return
    }

    setActionLoading(workerId)
    try {
      const resp = await fetch(`/api/workers/${workerId}/disconnect`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({})
      })
      if (!resp.ok) {
        const text = await resp.text()
        throw new Error(text || `Failed (${resp.status})`)
      }
    } catch (e: any) {
      alert(`Failed to disconnect worker: ${e.message}`)
    } finally {
      setActionLoading(null)
    }
  }

  if (loading && workers.size === 0) return <div className="loading">Loading...</div>
  if (error) return <ErrorState message={error} onRetry={fetchWorkers} />
  if (workers.size === 0) return (
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

  const workerList = Array.from(workers.values())

  return (
    <div className="workers">
      <table>
        <thead>
          <tr>
            <th>Status</th>
            <th>Name</th>
            <th>Mode</th>
            <th>Owner</th>
            <th>Labels</th>
            <th>Current Job</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {workerList.map(worker => (
            <tr key={worker.id}>
              <td>
                <span className={`status-dot ${worker.connected ? 'connected' : 'disconnected'}`}
                      title={worker.connected ? 'Connected' : 'Disconnected'} />
              </td>
              <td>
                <span className="worker-name">{worker.name || worker.hostname || worker.id}</span>
                {worker.version && (
                  <span className="worker-version">v{worker.version}</span>
                )}
              </td>
              <td>
                <span className={`badge mode-${worker.mode}`}>
                  {worker.mode}
                </span>
              </td>
              <td>{worker.owner_name || '-'}</td>
              <td>{worker.labels?.join(', ') || '-'}</td>
              <td>
                {worker.currentJob ? (
                  <Link to={`/jobs/${worker.currentJob}`} className="job-link">
                    {worker.currentJob}
                  </Link>
                ) : (
                  <span className="idle">idle</span>
                )}
              </td>
              <td>
                {worker.connected && worker.mode === 'shared' && (
                  <div className="worker-actions">
                    <button
                      className="btn btn-small"
                      onClick={() => handleDrain(worker.id)}
                      disabled={actionLoading === worker.id}
                      title="Graceful shutdown - finish current jobs first"
                    >
                      {actionLoading === worker.id ? '...' : 'Shutdown'}
                    </button>
                    <button
                      className="btn btn-small btn-danger"
                      onClick={() => handleDisconnect(worker.id)}
                      disabled={actionLoading === worker.id}
                      title="Force disconnect - cancel running jobs"
                    >
                      {actionLoading === worker.id ? '...' : 'Disconnect'}
                    </button>
                  </div>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
