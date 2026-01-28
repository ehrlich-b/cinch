import { useState } from 'react'

export function BadgesPage() {
  const [copied, setCopied] = useState(false)

  const exampleForge = 'github.com'
  const exampleOwner = 'owner'
  const exampleRepo = 'repo'
  const badgeUrl = `https://cinch.sh/badge/${exampleForge}/${exampleOwner}/${exampleRepo}.svg`
  const jobsUrl = `https://cinch.sh/jobs/${exampleForge}/${exampleOwner}/${exampleRepo}`
  const markdownSnippet = `[![build](${badgeUrl})](${jobsUrl})`

  const copyToClipboard = () => {
    navigator.clipboard.writeText(markdownSnippet)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className="badges-page">
      <div className="badges-hero">
        <h2>Build Badges</h2>
        <p className="badges-subtitle">Add your build status to any README</p>
      </div>

      <div className="badges-preview-section">
        <div className="badge-preview-large">
          <img
            src="https://img.shields.io/badge/build-passing-brightgreen"
            alt="build passing"
          />
        </div>
      </div>

      <div className="badge-usage">
        <h3>Add to your README</h3>
        <div className="code-block">
          <code>{markdownSnippet}</code>
          <button onClick={copyToClipboard} className="copy-btn">
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
        <p className="usage-note">
          Replace <code>github.com/owner/repo</code> with your repository. Badge links to your build history.
        </p>
      </div>
    </div>
  )
}
