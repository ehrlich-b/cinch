export function getDomainFromURL(url: string): string | null {
  const httpsMatch = url.match(/https?:\/\/([^/]+)/)
  if (httpsMatch) return httpsMatch[1].toLowerCase()

  const sshMatch = url.match(/git@([^:]+):/)
  if (sshMatch) return sshMatch[1].toLowerCase()

  return null
}

export function forgeToDomain(forgeType: string, cloneUrl?: string): string {
  if (cloneUrl) {
    const domain = getDomainFromURL(cloneUrl)
    if (domain) return domain
  }

  switch (forgeType) {
    case 'github': return 'github.com'
    case 'gitlab': return 'gitlab.com'
    case 'forgejo': return 'codeberg.org'
    case 'gitea': return 'gitea.com'
    default: return forgeType
  }
}
