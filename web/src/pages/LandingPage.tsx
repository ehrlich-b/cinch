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

export function LandingPage({ auth, onNavigate }: Props) {
  const [showForgeSelector, setShowForgeSelector] = useState(false)
  const [copied, setCopied] = useState(false)
  const [yearly, setYearly] = useState(false)

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
          <h1>CI that's a <span>cinch</span></h1>
          <p className="tagline">Run CI on your own hardware. Free for public repos. Self-host the whole thing.</p>

          <div className="install-row">
            <div className="install-box">
              <code className="install-cmd">curl -sSL https://cinch.sh/install.sh | sh</code>
              <button className="copy-btn" onClick={handleCopy}>
                {copied ? 'Copied!' : 'Copy'}
              </button>
            </div>
          </div>

          <div className="forge-logos">
            <img src={githubLogo} alt="GitHub" className="forge-logo github" />
            <img src={gitlabLogo} alt="GitLab" className="forge-logo gitlab" />
            <img src={forgejoLogo} alt="Codeberg" className="forge-logo" />
            <span className="forge-label">+ self-hosted forges</span>
          </div>
        </section>
      </div>

      <section className="why-section" id="why">
        <div className="container">
          <h2>Why Cinch?</h2>
          <p className="section-subtitle">Same Makefile locally and in CI. No translation layer.</p>
          <div className="why-grid">
            <div className="why-card">
              <div className="why-icon">🏠</div>
              <h3>Your Hardware</h3>
              <p>Run builds on your Mac, your server, your Raspberry Pi. You own it.</p>
            </div>
            <div className="why-card">
              <div className="why-icon">⚡</div>
              <h3>Instant Builds</h3>
              <p>No waiting in queues. Your hardware is always available.</p>
            </div>
            <div className="why-card">
              <div className="why-icon">🔧</div>
              <h3>Dead Simple</h3>
              <p>One command to build, one to release. Cinch handles the rest.</p>
            </div>
            <div className="why-card">
              <div className="why-icon">🔀</div>
              <h3>Any Forge</h3>
              <p>GitHub, GitLab, Codeberg, self-hosted. One worker handles them all.</p>
            </div>
          </div>
          <div className="config-example">
            <div className="config-file">
              <div className="config-filename">.cinch.yaml</div>
              <pre className="config-content">build: make build{'\n'}release: make release</pre>
            </div>
            <p className="config-caption">That's it. The same <code>make build</code> you run locally.</p>
          </div>
        </div>
      </section>

      <section className="social-proof-section">
        <div className="container">
          <p className="social-proof-text">Cinch builds itself with Cinch</p>
          <div className="social-proof-badges">
            <img src="https://cinch.sh/badge/github/ehrlich-b/cinch" alt="GitHub build" />
            <img src="https://cinch.sh/badge/gitlab/ehrlich-b/cinch" alt="GitLab build" />
            <img src="https://cinch.sh/badge/forgejo/ehrlich-b/cinch" alt="Codeberg build" />
          </div>
        </div>
      </section>

      <section className="how-it-works-section" id="how-it-works">
        <div className="container">
          <h2>How It Works</h2>
          <div className="steps-row">
            <div className="step-card">
              <div className="step-icon">1</div>
              <h3>Install</h3>
              <code>curl cinch.sh | sh</code>
              <p>One command</p>
            </div>
            <div className="step-arrow">→</div>
            <div className="step-card">
              <div className="step-icon">2</div>
              <h3>Login</h3>
              <code>cinch login</code>
              <p>Opens browser</p>
            </div>
            <div className="step-arrow">→</div>
            <div className="step-card">
              <div className="step-icon">3</div>
              <h3>Run</h3>
              <code>cinch worker -s</code>
              <p>Builds run locally</p>
            </div>
          </div>
        </div>
      </section>

      <section className="pricing-section" id="pricing">
        <div className="container">
          <h2>Pricing</h2>
          <div className="pricing-toggle">
            <span className={!yearly ? 'active' : ''}>Monthly</span>
            <button className="toggle-switch" onClick={() => setYearly(!yearly)}>
              <span className={`toggle-knob ${yearly ? 'yearly' : ''}`} />
            </button>
            <span className={yearly ? 'active' : ''}>Yearly <span className="save-badge">Save 20%</span></span>
          </div>
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
              <div className="plan-cta">
                <button className="btn-pro" onClick={() => setShowForgeSelector(true)}>
                  Get Started
                </button>
              </div>
            </div>
            <div className="plan-card featured">
              <div className="plan-name">Pro</div>
              <div className="plan-price">${yearly ? '4' : '5'}<span className="period">/seat/mo</span></div>
              <div className="plan-note">Private repos + more</div>
              <ul className="plan-features-list">
                <li>Private repositories</li>
                <li>1000 workers</li>
                <li>10 GB storage per seat</li>
                <li>90-day retention</li>
              </ul>
              <div className="plan-cta">
                <button className="btn-pro" onClick={() => setShowForgeSelector(true)}>
                  Get Started
                </button>
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
