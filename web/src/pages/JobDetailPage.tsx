import { useState, useEffect, useRef } from 'react'
import { ErrorState } from '../components/ErrorState'
import { StatusIcon } from '../components/StatusIcon'
import { relativeTime, renderAnsi } from '../utils/format'
import type { Job, LogEntry } from '../types'

interface Props {
  jobId: string
  onBack: () => void
}

export function JobDetailPage({ jobId, onBack }: Props) {
  const [job, setJob] = useState<Job | null>(null)
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [status, setStatus] = useState<string>('')
  const [error, setError] = useState<string | null>(null)
  const [wsError, setWsError] = useState<string | null>(null)
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

  useEffect(() => {
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
            <span>{job.branch}</span>
            <span className="mono">{job.commit?.slice(0, 7)}</span>
            <span className="text-muted">{relativeTime(job.created_at)}</span>
          </div>
        )}
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
