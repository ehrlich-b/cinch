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

  return (
    <div className="landing">
      <header className="landing-header">
        <div className="landing-header-inner">
          <span className="landing-logo">cinch</span>
          <nav className="landing-nav">
            <a href="#features">Features</a>
            <a href="#quickstart">Quick Start</a>
            <a href="#pricing">Pricing</a>
            <a href="https://github.com/ehrlich-b/cinch">Code</a>
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
          <h1>CI that's a <span>cinch</span></h1>
          <p className="tagline">The exact <code>make build</code> you run locally. That's your CI.</p>

          <div className="config-showcase">
            <div className="config-file">
              <div className="config-filename">.cinch.yaml</div>
              <pre className="config-content">build: make build{'\n'}release: make release</pre>
            </div>
            <div className="config-file">
              <div className="config-filename">Makefile</div>
              <pre className="config-content">build:{'\n'}    go build -o bin/app{'\n'}{'\n'}release:{'\n'}    cinch release dist/*</pre>
            </div>
          </div>

          <p className="hero-subtext">Your Makefile already works. We just run it on push.</p>

          <div className="install-row">
            <div className="install-box">
              <code className="install-cmd">curl -sSL https://cinch.sh/install.sh | sh</code>
              <button className="copy-btn" onClick={() => navigator.clipboard.writeText('curl -sSL https://cinch.sh/install.sh | sh')}>
                Copy
              </button>
            </div>
          </div>
          <p className="install-note">macOS and Linux. Windows via WSL.</p>
        </section>
      </div>

      <section className="features-section" id="features">
        <div className="container">
          <h2>Why cinch?</h2>
          <p className="section-subtitle">Your Makefile is the pipeline. We just invoke it.</p>
          <div className="features-grid-landing">
            <div className="feature-card">
              <h3>Multi-Forge</h3>
              <p>GitHub, GitLab, Codeberg. One worker, any forge. Self-hosted Forgejo and GitLab too.</p>
            </div>
            <div className="feature-card">
              <h3>Your Hardware</h3>
              <p>Run builds on your Mac, your VM, your Raspberry Pi. No per-minute charges. No waiting in shared queues.</p>
            </div>
            <div className="feature-card">
              <h3>Dead Simple</h3>
              <p>One command in .cinch.yaml. No multi-step pipelines, no DAGs, no plugins. Just make ci.</p>
            </div>
          </div>
        </div>
      </section>

      <div className="container">
        <section className="quickstart" id="quickstart">
          <h2>Quick Start</h2>
          <div className="steps">
            <div className="step">
              <div className="step-number">1</div>
              <h3>Install & login</h3>
              <p><code>curl -sSL https://cinch.sh/install.sh | sh</code> then <code>cinch login</code></p>
            </div>
            <div className="step">
              <div className="step-number">2</div>
              <h3>Start a worker</h3>
              <p><code>cinch worker</code> — runs on your Mac, Linux box, or Raspberry Pi.</p>
            </div>
            <div className="step">
              <div className="step-number">3</div>
              <h3>Push code</h3>
              <p>Add <code>.cinch.yaml</code> with <code>build: make build</code> and push.</p>
            </div>
          </div>
        </section>
      </div>

      <section className="pricing-section" id="pricing">
        <div className="container">
          <h2>Pricing</h2>
          <p className="pricing-subtitle">Free while in beta. MIT licensed. Self-host anytime.</p>
          <div className="pricing-grid-landing">
            <div className="plan-card">
              <div className="plan-name">Public Repos</div>
              <div className="plan-price">$0</div>
              <div className="plan-note">Free forever</div>
              <ul className="plan-features-list">
                <li>Unlimited builds</li>
                <li>Unlimited workers</li>
                <li>All forges supported</li>
                <li>Community support</li>
              </ul>
              <div className="plan-cta"></div>
            </div>
            <div className="plan-card featured">
              <div className="plan-name">Pro</div>
              <div className="plan-price"><s className="old-price">$5</s> $0<span className="period">/seat/mo</span></div>
              <div className="plan-note">Free during beta</div>
              <ul className="plan-features-list">
                <li>Everything in Free</li>
                <li>Private repositories</li>
                <li>Priority support</li>
                <li>Badge customization</li>
              </ul>
              <div className="plan-cta">
                {auth.isPro ? (
                  <div className="pro-status">You have Pro</div>
                ) : auth.authenticated ? (
                  <button className="btn-pro" onClick={handleGivePro} disabled={givingPro}>
                    {givingPro ? 'Activating...' : 'Give me Pro'}
                  </button>
                ) : (
                  <a href="/auth/login" className="btn-pro" style={{ display: 'block', textAlign: 'center', textDecoration: 'none' }}>
                    Login to get Pro
                  </a>
                )}
              </div>
            </div>
            <div className="plan-card">
              <div className="plan-name">Enterprise</div>
              <div className="plan-price">Custom</div>
              <div className="plan-note">For teams that need support</div>
              <ul className="plan-features-list">
                <li>Dedicated support</li>
                <li>SLA guarantees</li>
                <li>Custom integrations</li>
                <li>Managed hosting option</li>
              </ul>
              <div className="plan-cta"></div>
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
