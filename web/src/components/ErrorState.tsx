interface Props {
  message: string
  onRetry: () => void
}

export function ErrorState({ message, onRetry }: Props) {
  return (
    <div className="error-state">
      <div className="error-icon">!</div>
      <h3>Something went wrong</h3>
      <p>{message}</p>
      <button onClick={onRetry} className="retry-btn">Try again</button>
    </div>
  )
}
