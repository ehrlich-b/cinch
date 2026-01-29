import { useState, useEffect } from 'react'
import type { Page, AuthState, RepoPath } from './types'

// Pages
import { LandingPage } from './pages/LandingPage'
import { JobsPage } from './pages/JobsPage'
import { JobDetailPage } from './pages/JobDetailPage'
import { RepoJobsPage } from './pages/RepoJobsPage'
import { WorkersPage } from './pages/WorkersPage'
import { ReposPage } from './pages/ReposPage'
import { BadgesPage } from './pages/BadgesPage'
import { AccountPage } from './pages/AccountPage'
import { SuccessPage } from './pages/SuccessPage'
import { GitLabOnboardPage } from './pages/onboard/GitLabOnboardPage'
import { ForgejoOnboardPage } from './pages/onboard/ForgejoOnboardPage'

function getPageFromPath(): { page: Page; jobId: string | null; repoPath: RepoPath | null } {
  const path = window.location.pathname

  if (path.startsWith('/jobs/')) {
    const rest = path.slice(6)
    const parts = rest.split('/')
    if (parts.length >= 3 && parts[0].includes('.')) {
      return {
        page: 'repo-jobs',
        jobId: null,
        repoPath: { forge: parts[0], owner: parts[1], repo: parts[2] }
      }
    }
    return { page: 'jobs', jobId: rest, repoPath: null }
  }
  if (path === '/jobs') return { page: 'jobs', jobId: null, repoPath: null }
  if (path === '/workers') return { page: 'workers', jobId: null, repoPath: null }
  if (path === '/repos') return { page: 'repos', jobId: null, repoPath: null }
  if (path === '/badges') return { page: 'badges', jobId: null, repoPath: null }
  if (path === '/account') return { page: 'account', jobId: null, repoPath: null }
  if (path === '/gitlab/onboard') return { page: 'gitlab-onboard', jobId: null, repoPath: null }
  if (path === '/forgejo/onboard') return { page: 'forgejo-onboard', jobId: null, repoPath: null }
  if (path === '/success') return { page: 'success', jobId: null, repoPath: null }
  return { page: 'home', jobId: null, repoPath: null }
}

export function App() {
  const initial = getPageFromPath()
  const [page, setPage] = useState<Page>(initial.page)
  const [selectedJob, setSelectedJob] = useState<string | null>(initial.jobId)
  const [selectedRepoPath, setSelectedRepoPath] = useState<RepoPath | null>(initial.repoPath)
  const [auth, setAuth] = useState<AuthState>({ authenticated: false, loading: true })

  useEffect(() => {
    const handlePopState = () => {
      const { page, jobId, repoPath } = getPageFromPath()
      setPage(page)
      setSelectedJob(jobId)
      setSelectedRepoPath(repoPath)
    }
    window.addEventListener('popstate', handlePopState)
    return () => window.removeEventListener('popstate', handlePopState)
  }, [])

  const navigate = (newPage: Page, jobId: string | null = null, repoPath: RepoPath | null = null) => {
    let path = '/'
    if (newPage === 'jobs' && jobId) path = `/jobs/${jobId}`
    else if (newPage === 'jobs') path = '/jobs'
    else if (newPage === 'repo-jobs' && repoPath) path = `/jobs/${repoPath.forge}/${repoPath.owner}/${repoPath.repo}`
    else if (newPage === 'workers') path = '/workers'
    else if (newPage === 'repos') path = '/repos'
    else if (newPage === 'badges') path = '/badges'
    else if (newPage === 'account') path = '/account'
    else if (newPage === 'gitlab-onboard') path = '/gitlab/onboard'
    else if (newPage === 'forgejo-onboard') path = '/forgejo/onboard'
    else if (newPage === 'success') path = '/success'

    history.pushState({}, '', path)
    setPage(newPage)
    setSelectedJob(jobId)
    setSelectedRepoPath(repoPath)
  }

  useEffect(() => {
    fetch('/auth/me')
      .then(r => r.json())
      .then(data => setAuth({ ...data, loading: false }))
      .catch(() => setAuth({ authenticated: false, loading: false }))
  }, [])

  // Landing page for unauthenticated users or home page
  if (!auth.loading && (!auth.authenticated || page === 'home') &&
      page !== 'gitlab-onboard' && page !== 'forgejo-onboard' &&
      page !== 'success' && page !== 'repo-jobs') {
    return <LandingPage auth={auth} setAuth={setAuth} onNavigate={navigate} />
  }

  // GitLab onboard
  if (page === 'gitlab-onboard') {
    if (!auth.authenticated && !auth.loading) {
      window.location.href = '/auth/gitlab'
      return <div className="loading">Redirecting to GitLab...</div>
    }
    return <GitLabOnboardPage onComplete={() => navigate('success')} onCancel={() => navigate('repos')} />
  }

  // Forgejo/Codeberg onboard
  if (page === 'forgejo-onboard') {
    if (!auth.authenticated && !auth.loading) {
      window.location.href = '/auth/forgejo'
      return <div className="loading">Redirecting to Codeberg...</div>
    }
    return <ForgejoOnboardPage onComplete={() => navigate('success')} onCancel={() => navigate('repos')} />
  }

  // Success page
  if (page === 'success') {
    return <SuccessPage onContinue={() => navigate('jobs')} />
  }

  // Public repo jobs page (accessible without login)
  if (page === 'repo-jobs' && !auth.authenticated && selectedRepoPath) {
    return (
      <div className="app">
        <header>
          <h1 onClick={() => navigate('home')} style={{ cursor: 'pointer' }}>cinch</h1>
          <nav></nav>
          <div className="auth">
            <a href="/auth/login" className="login">Login</a>
          </div>
        </header>
        <main>
          <RepoJobsPage
            repoPath={selectedRepoPath}
            onSelectJob={(id) => navigate('jobs', id)}
            onBack={() => navigate('home')}
          />
        </main>
      </div>
    )
  }

  // Main authenticated app
  return (
    <div className="app">
      <header>
        <h1 onClick={() => navigate('home')} style={{ cursor: 'pointer' }}>cinch</h1>
        <nav>
          <button className={page === 'jobs' ? 'active' : ''} onClick={() => navigate('jobs')}>Jobs</button>
          <button className={page === 'workers' ? 'active' : ''} onClick={() => navigate('workers')}>Workers</button>
          <button className={page === 'repos' ? 'active' : ''} onClick={() => navigate('repos')}>Repos</button>
          <button className={page === 'badges' ? 'active' : ''} onClick={() => navigate('badges')}>Badges</button>
        </nav>
        <div className="auth">
          {auth.loading ? null : auth.authenticated ? (
            <>
              <button className="user-btn" onClick={() => navigate('account')}>{auth.user}</button>
              <a href="/auth/logout" className="logout">Logout</a>
            </>
          ) : (
            <a href="/auth/login" className="login">Login</a>
          )}
        </div>
      </header>
      <main>
        {page === 'jobs' && !selectedJob && <JobsPage onSelectJob={(id) => navigate('jobs', id)} />}
        {page === 'jobs' && selectedJob && <JobDetailPage jobId={selectedJob} onBack={() => navigate('jobs')} onSelectJob={(id) => navigate('jobs', id)} />}
        {page === 'repo-jobs' && selectedRepoPath && (
          <RepoJobsPage
            repoPath={selectedRepoPath}
            onSelectJob={(id) => navigate('jobs', id)}
            onBack={() => navigate('jobs')}
          />
        )}
        {page === 'workers' && <WorkersPage />}
        {page === 'repos' && (
          <ReposPage
            onAddGitLab={() => window.location.href = '/auth/gitlab'}
            onAddForgejo={() => window.location.href = '/auth/forgejo'}
            onSelectRepo={(repoPath) => navigate('repo-jobs', null, repoPath)}
          />
        )}
        {page === 'badges' && <BadgesPage />}
        {page === 'account' && <AccountPage onLogout={() => window.location.href = '/auth/logout'} />}
      </main>
    </div>
  )
}
