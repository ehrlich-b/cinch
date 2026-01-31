import { useState } from 'react'
import githubLogo from '../assets/github.svg'
import gitlabLogo from '../assets/gitlab.svg'
import forgejoLogo from '../assets/forgejo.svg'
import type { AuthState, Page } from '../types'

interface Props {
  auth: AuthState
  setAuth: (auth: AuthState) => void
  onNavigate: (page: Page) => void
}

export function LandingPage({ auth, setAuth, onNavigate }: Props) {
  const [givingPro, setGivingPro] = useState(false)
  const [showForgeSelector, setShowForgeSelector] = useState(false)
  const [copied, setCopied] = useState(false)

  const handleGivePro = async () => {
    setGivingPro(true)
    try {
      const res = await fetch('/api/give-me-pro', { method: 'POST' })
      if (res.ok) {
        setAuth({ ...auth, isPro: true })
      }
    } catch (e) {
      console.error('Failed to give pro:', e)
    }
    setGivingPro(false)
  }

  const handleCopy = () => {
    navigator.clipboard.writeText('curl -sSL https://cinch.sh/install.sh | sh')
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="landing">
      <header className="landing-header">
        <div className="landing-header-inner">
          <span className="landing-logo">cinch</span>
          <nav className="landing-nav">
            <a href="#how-it-works">How It Works</a>
            <a href="#pricing">Pricing</a>
            <a href="https://github.com/ehrlich-b/cinch">GitHub</a>
            {auth.authenticated ? (
              <button className="landing-btn" onClick={() => onNavigate('jobs')}>Dashboard</button>
            ) : (
              <button className="landing-btn" onClick={() => setShowForgeSelector(true)}>Get Started</button>
            )}
          </nav>
        </div>
      </header>

      <div className="container">
        <section className="hero">
          <h1>GitHub charges $0.008/min for runners.<br /><span>Use your own hardware instead.</span></h1>
          <p className="tagline">Cinch runs CI on your machines. One config file, any git forge, zero cloud bills.</p>

          <div className="install-row">
            <div className="install-box">
              <code className="install-cmd">curl -sSL https://cinch.sh/install.sh | sh</code>
              <button className="copy-btn" onClick={handleCopy}>
                {copied ? 'Copied!' : 'Copy'}
              </button>
            </div>
            <a href="https://github.com/ehrlich-b/cinch" className="demo-link">View on GitHub</a>
          </div>

          <div className="forge-logos">
            <span className="forge-label">Works with:</span>
            <img src={githubLogo} alt="GitHub" className="forge-logo github" />
            <img src={gitlabLogo} alt="GitLab" className="forge-logo" />
            <img src={forgejoLogo} alt="Codeberg" className="forge-logo" />
            <span className="forge-label">+ self-hosted</span>
          </div>
        </section>
      </div>

      <section className="how-it-works-section" id="how-it-works">
        <div className="container">
          <h2>How It Works</h2>
          <div className="steps-row">
            <div className="step-card">
              <div className="step-icon">1</div>
              <h3>Install</h3>
              <code>curl -sSL https://cinch.sh/install.sh | sh</code>
              <p>One command. No dependencies.</p>
            </div>
            <div className="step-arrow">→</div>
            <div className="step-card">
              <div className="step-icon">2</div>
              <h3>Login</h3>
              <code>cinch login</code>
              <p>Opens browser, saves credentials.</p>
            </div>
            <div className="step-arrow">→</div>
            <div className="step-card">
              <div className="step-icon">3</div>
              <h3>Run</h3>
              <code>cinch worker</code>
              <p>Builds run on your machine.</p>
            </div>
          </div>
        </div>
      </section>

      <section className="why-section" id="why">
        <div className="container">
          <h2>Why Cinch?</h2>
          <p className="section-subtitle">Same Makefile locally and in CI. No translation layer.</p>
          <div className="why-grid">
            <div className="why-card">
              <div className="why-icon">$</div>
              <h3>Zero Cloud Bills</h3>
              <p>GitHub Actions costs add up. Your laptop is already paid for.</p>
            </div>
            <div className="why-card">
              <div className="why-icon">⚡</div>
              <h3>Instant Builds</h3>
              <p>No waiting for shared runners. Your hardware is always available.</p>
            </div>
            <div className="why-card">
              <div className="why-icon">🔧</div>
              <h3>Dead Simple</h3>
              <p>One command in config. No YAML pipelines, no plugins, no complexity.</p>
            </div>
            <div className="why-card">
              <div className="why-icon">🔀</div>
              <h3>Multi-Forge</h3>
              <p>GitHub, GitLab, Codeberg. One worker handles them all.</p>
            </div>
          </div>
          <div className="config-example">
            <div className="config-file">
              <div className="config-filename">.cinch.yaml</div>
              <pre className="config-content">build: make build</pre>
            </div>
            <p className="config-caption">That's it. The same <code>make build</code> you run locally.</p>
          </div>
        </div>
      </section>

      <section className="social-proof-section">
        <div className="container">
          <p className="social-proof-text">Cinch builds itself with Cinch</p>
          <img
            src="https://cinch.sh/badge/github/ehrlich-b/cinch"
            alt="Cinch build status"
            className="social-proof-badge"
          />
        </div>
      </section>

      <section className="pricing-section" id="pricing">
        <div className="container">
          <h2>Pricing</h2>
          <p className="pricing-subtitle">Free during beta. MIT licensed.</p>
          <div className="pricing-grid-landing">
            <div className="plan-card">
              <div className="plan-name">Free</div>
              <div className="plan-price">$0</div>
              <div className="plan-note">Public repos</div>
              <ul className="plan-features-list">
                <li>Unlimited builds</li>
                <li>10 workers</li>
                <li>100 MB log storage</li>
                <li>7-day retention</li>
              </ul>
            </div>
            <div className="plan-card featured">
              <div className="plan-name">Pro</div>
              <div className="plan-price"><s className="old-price">$5</s> $0<span className="period">/seat/mo</span></div>
              <div className="plan-note">Free during beta</div>
              <ul className="plan-features-list">
                <li>Private repositories</li>
                <li>1000 workers</li>
                <li>10 GB storage per seat</li>
                <li>90-day retention</li>
              </ul>
              <div className="plan-cta">
                {auth.isPro ? (
                  <div className="pro-status">You have Pro</div>
                ) : auth.authenticated ? (
                  <button className="btn-pro" onClick={handleGivePro} disabled={givingPro}>
                    {givingPro ? 'Activating...' : 'Get Pro Free'}
                  </button>
                ) : (
                  <button className="btn-pro" onClick={() => setShowForgeSelector(true)}>
                    Get Started
                  </button>
                )}
              </div>
            </div>
            <div className="plan-card">
              <div className="plan-name">Self-Hosted</div>
              <div className="plan-price">$0</div>
              <div className="plan-note">Your infrastructure</div>
              <ul className="plan-features-list">
                <li>Everything unlimited</li>
                <li>MIT licensed</li>
                <li>Single binary deploy</li>
                <li>Your data stays yours</li>
              </ul>
              <div className="plan-cta">
                <a href="https://github.com/ehrlich-b/cinch" className="btn-selfhost">View Source</a>
              </div>
            </div>
          </div>
        </div>
      </section>

      <footer className="landing-footer">
        <div className="footer-inner">
          <div className="footer-brand">cinch</div>
          <div className="footer-links">
            <a href="https://github.com/ehrlich-b/cinch">GitHub</a>
            <a href="https://github.com/ehrlich-b/cinch/issues">Issues</a>
            <a href="mailto:bryan@ehrlich.dev">Contact</a>
          </div>
        </div>
        <div className="footer-copy">
          MIT License. Built by <a href="https://github.com/ehrlich-b" style={{ color: 'inherit' }}>Bryan Ehrlich</a>.
        </div>
      </footer>

      {showForgeSelector && (
        <div className="modal-overlay" onClick={() => setShowForgeSelector(false)}>
          <div className="modal forge-selector-modal" onClick={e => e.stopPropagation()}>
            <button className="modal-close" onClick={() => setShowForgeSelector(false)}>×</button>
            <h2>Get Started</h2>
            <p className="modal-subtitle">Connect your forge to start building</p>
            <div className="forge-options">
              <a href="/auth/github" className="forge-option">
                <img src={githubLogo} alt="GitHub" className="forge-option-icon github" />
                <span>GitHub</span>
              </a>
              <a href="/auth/gitlab" className="forge-option">
                <img src={gitlabLogo} alt="GitLab" className="forge-option-icon" />
                <span>GitLab</span>
              </a>
              <a href="/auth/forgejo" className="forge-option">
                <img src={forgejoLogo} alt="Codeberg" className="forge-option-icon" />
                <span>Codeberg</span>
              </a>
            </div>
            <p className="forge-note">Already have an account? This will log you in.</p>
          </div>
        </div>
      )}
    </div>
  )
}
