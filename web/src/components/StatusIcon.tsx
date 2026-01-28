export function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case 'success': return <span className="status success">✓</span>
    case 'failed':
    case 'failure': return <span className="status failure">✗</span>
    case 'running': return <span className="status running">◐</span>
    case 'pending':
    case 'queued': return <span className="status pending">◷</span>
    case 'error': return <span className="status error">!</span>
    default: return <span className="status">{status}</span>
  }
}
