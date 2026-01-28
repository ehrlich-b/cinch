import githubLogo from '../assets/github.svg'
import gitlabLogo from '../assets/gitlab.svg'
import giteaLogo from '../assets/gitea.svg'
import forgejoLogo from '../assets/forgejo.svg'
import codebergLogo from '../assets/codeberg.svg'

interface Props {
  forge: string
  cloneUrl?: string
  domain?: string
}

export function ForgeIcon({ forge, cloneUrl, domain }: Props) {
  // Determine the host - prefer explicit domain, then extract from cloneUrl
  const host = domain || (cloneUrl ? getDomainFromCloneURL(cloneUrl) : null)

  // Check if this is Codeberg (forgejo at codeberg.org)
  if (forge === 'forgejo' && host === 'codeberg.org') {
    return <img src={codebergLogo} alt="Codeberg" className="forge-icon" />
  }

  switch (forge) {
    case 'github':
      return <img src={githubLogo} alt="GitHub" className="forge-icon github" />
    case 'gitlab':
      return <img src={gitlabLogo} alt="GitLab" className="forge-icon" />
    case 'gitea':
      return <img src={giteaLogo} alt="Gitea" className="forge-icon" />
    case 'forgejo':
      return <img src={forgejoLogo} alt="Forgejo" className="forge-icon" />
    default:
      return <span className="forge-text">{forge}</span>
  }
}

function getDomainFromCloneURL(url: string): string | null {
  const httpsMatch = url.match(/https?:\/\/([^/]+)/)
  if (httpsMatch) return httpsMatch[1].toLowerCase()

  const sshMatch = url.match(/git@([^:]+):/)
  if (sshMatch) return sshMatch[1].toLowerCase()

  return null
}
