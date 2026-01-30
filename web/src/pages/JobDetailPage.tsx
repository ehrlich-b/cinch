import { useState, useEffect, useRef } from 'react'
import { ErrorState } from '../components/ErrorState'
import { StatusIcon } from '../components/StatusIcon'
import { relativeTime, renderAnsi } from '../utils/format'
import type { Job, JobAttempt, LogEntry } from '../types'

interface Props {
  jobId: string
  onBack: () => void
  onSelectJob?: (jobId: string) => void
}

function AttemptsDropdown({ currentJob, attempts, onSelect }: {
  currentJob: { id: string, status: string, created_at?: string }
  attempts: JobAttempt[]
  onSelect: (id: string) => void
}) {
  const [open, setOpen] = useState(false)

  if (attempts.length === 0) return null

  // Combine current job with siblings and sort by creation time (oldest first)
  const allAttempts = [
    { id: currentJob.id, status: currentJob.status, created_at: currentJob.created_at || '' },
    ...attempts
  ].sort((a, b) => a.created_at.localeCompare(b.created_at))

  // Find which attempt number the current job is
  const currentAttemptNum = allAttempts.findIndex(a => a.id === currentJob.id) + 1

  return (
    <div className="attempts-dropdown">
      <button onClick={() => setOpen(!open)} className="attempts-btn">
        Attempt {currentAttemptNum} of {allAttempts.length} ▾
      </button>
      {open && (
        <div className="attempts-menu">
          {allAttempts.map((a, i) => {
            const isCurrent = a.id === currentJob.id
            return (
              <div
                key={a.id}
                className={`attempt-item ${isCurrent ? 'current' : 'clickable'}`}
                onClick={() => { if (!isCurrent) { onSelect(a.id); setOpen(false) }}}
              >
                <StatusIcon status={a.status} />
                <span>Attempt {i + 1}</span>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

export function JobDetailPage({ jobId, onBack, onSelectJob }: Props) {
  const [job, setJob] = useState<Job | null>(null)
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [status, setStatus] = useState<string>('')
  const [error, setError] = useState<string | null>(null)
  const [wsError, setWsError] = useState<string | null>(null)
  const [runLoading, setRunLoading] = useState(false)
  const [runError, setRunError] = useState<string | null>(null)
  const logsEndRef = useRef<HTMLDivElement>(null)
  const wsRef = useRef<WebSocket | null>(null)

  const fetchJob = () => {
    setError(null)
    fetch(`/api/jobs/${jobId}`)
      .then(r => {
        if (!r.ok) throw new Error(`Failed to load job (${r.status})`)
        return r.json()
      })
      .then(data => setJob(data))
      .catch(e => setError(e.message || 'Failed to load job'))
  }

  const handleRun = async () => {
    setRunLoading(true)
    setRunError(null)
    try {
      const response = await fetch(`/api/jobs/${jobId}/run`, { method: 'POST' })
      if (!response.ok) {
        const text = await response.text()
        throw new Error(text || `Failed to run job (${response.status})`)
      }
      const data = await response.json()
      // Navigate to the new job (or same job if it was pending_contributor)
      if (onSelectJob && data.job_id) {
        onSelectJob(data.job_id)
      }
    } catch (e: unknown) {
      setRunError(e instanceof Error ? e.message : 'Failed to run job')
    } finally {
      setRunLoading(false)
    }
  }

  // Determine if job can be run/retried
  const canRun = job && ['failed', 'success', 'error', 'cancelled', 'pending_contributor'].includes(status || job.status)
  const runButtonLabel = (status || job?.status) === 'pending_contributor' ? 'Run' : 'Retry'

  // Reset state when jobId changes
  useEffect(() => {
    setLogs([])
    setStatus('')
    setJob(null)
    setError(null)
    setWsError(null)
    setRunError(null)
    fetchJob()
  }, [jobId])

  useEffect(() => {
    setWsError(null)
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

    ws.onerror = () => setWsError('Lost connection to log stream')
    ws.onclose = () => {}

    return () => {
      ws.close()
    }
  }, [jobId])

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

  if (error) return (
    <div className="job-detail">
      <div className="job-header">
        <button onClick={onBack} className="back-btn">← Back</button>
        <h2>Job {jobId.slice(0, 12)}</h2>
      </div>
      <ErrorState message={error} onRetry={fetchJob} />
    </div>
  )

  return (
    <div className="job-detail">
      <div className="job-header">
        <button onClick={onBack} className="back-btn">← Back</button>
        <h2>Job {jobId.slice(0, 12)}</h2>
        {job && (
          <div className="job-meta">
            <span><StatusIcon status={status || job.status} /></span>
            <span>{job.repo}</span>
            <span>
              {job.pr_number ? (
                <span title={`${job.branch} → ${job.pr_base_branch}`}>
                  PR #{job.pr_number}
                </span>
              ) : job.branch}
            </span>
            <span className="mono">{job.commit?.slice(0, 7)}</span>
            <span className="text-muted">{relativeTime(job.created_at)}</span>
            {job.attempts && job.attempts.length > 0 && onSelectJob && (
              <AttemptsDropdown
                currentJob={{ id: job.id, status: status || job.status, created_at: job.created_at }}
                attempts={job.attempts}
                onSelect={onSelectJob}
              />
            )}
            {canRun && (
              <button
                onClick={handleRun}
                disabled={runLoading}
                className="run-btn"
                title={runButtonLabel === 'Run' ? 'Approve and run on shared worker' : 'Retry this job'}
              >
                {runLoading ? '...' : runButtonLabel}
              </button>
            )}
          </div>
        )}
        {runError && <div className="run-error">{runError}</div>}
      </div>
      {wsError && <div className="ws-error">{wsError}</div>}
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
