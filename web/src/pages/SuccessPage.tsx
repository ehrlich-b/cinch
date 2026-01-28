interface Props {
  onContinue: () => void
}

export function SuccessPage({ onContinue }: Props) {
  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text)
  }

  return (
    <div className="success-page">
      <div className="success-container">
        <div className="success-header">
          <div className="success-icon">✓</div>
          <h1>You're all set!</h1>
          <p>Your repositories are connected. Now set up a worker to start building.</p>
        </div>

        <div className="setup-steps">
          <div className="setup-step">
            <div className="step-number">1</div>
            <div className="step-content">
              <h3>Install Cinch</h3>
              <div className="code-block">
                <code>curl -sSL https://cinch.sh/install.sh | sh</code>
                <button className="copy-btn" onClick={() => copyToClipboard('curl -sSL https://cinch.sh/install.sh | sh')}>
                  Copy
                </button>
              </div>
            </div>
          </div>

          <div className="setup-step">
            <div className="step-number">2</div>
            <div className="step-content">
              <h3>Login & Start Worker</h3>
              <div className="code-block">
                <code>cinch login && cinch worker --all</code>
                <button className="copy-btn" onClick={() => copyToClipboard('cinch login && cinch worker --all')}>
                  Copy
                </button>
              </div>
              <p className="step-note">
                The <code>--all</code> flag builds all your connected repos. Leave it running!
              </p>
            </div>
          </div>

          <div className="setup-step">
            <div className="step-number">3</div>
            <div className="step-content">
              <h3>Push Code</h3>
              <p>
                Add <code>.cinch.yaml</code> to your repo with your build command:
              </p>
              <div className="code-block yaml-block">
                <code>build: make build</code>
              </div>
              <p className="step-note">Push and watch the build run!</p>
            </div>
          </div>
        </div>

        <div className="success-actions">
          <button className="btn-primary" onClick={onContinue}>
            Go to Dashboard
          </button>
        </div>
      </div>
    </div>
  )
}
