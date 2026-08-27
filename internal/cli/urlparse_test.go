package cli

import "testing"

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		want      *RepoInfo
		wantErr   bool
	}{
		{
			name:      "https github with .git suffix",
			remoteURL: "https://github.com/owner/repo.git",
			want:      &RepoInfo{Forge: "github.com", Owner: "owner", Name: "repo"},
		},
		{
			name:      "https github without .git suffix",
			remoteURL: "https://github.com/owner/repo",
			want:      &RepoInfo{Forge: "github.com", Owner: "owner", Name: "repo"},
		},
		{
			name:      "ssh with .git suffix",
			remoteURL: "git@github.com:owner/repo.git",
			want:      &RepoInfo{Forge: "github.com", Owner: "owner", Name: "repo"},
		},
		{
			name:      "ssh without .git suffix",
			remoteURL: "git@github.com:owner/repo",
			want:      &RepoInfo{Forge: "github.com", Owner: "owner", Name: "repo"},
		},
		{
			name:      "ssh malformed missing owner/repo",
			remoteURL: "git@github.com:onlyowner",
			wantErr:   true,
		},
		{
			name:      "https too few path segments",
			remoteURL: "https://github.com/onlyowner",
			wantErr:   true,
		},
		{
			name:      "https self-hosted host passed through unchanged",
			remoteURL: "https://git.example.com/owner/repo",
			want:      &RepoInfo{Forge: "git.example.com", Owner: "owner", Name: "repo"},
		},
		{
			// GitLab subgroup URL: parseRemoteURL only looks at the first
			// two path segments, so Owner="group" and Name="sub" (the
			// trailing "/repo" segment is dropped). Subgroups are
			// misparsed, but this is what the code actually does.
			name:      "https gitlab subgroup truncates to first two segments",
			remoteURL: "https://gitlab.com/group/sub/repo.git",
			want:      &RepoInfo{Forge: "gitlab.com", Owner: "group", Name: "sub"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRemoteURL(tt.remoteURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseRemoteURL(%q) expected error, got %+v", tt.remoteURL, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRemoteURL(%q) unexpected error: %v", tt.remoteURL, err)
			}
			if *got != *tt.want {
				t.Errorf("parseRemoteURL(%q) = %+v, want %+v", tt.remoteURL, got, tt.want)
			}
		})
	}
}

func TestHostToForgeDomain(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"github.com", "github.com"},
		{"gitlab.com", "gitlab.com"},
		{"codeberg.org", "codeberg.org"},
		{"git.example.com", "git.example.com"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := hostToForgeDomain(tt.host); got != tt.want {
				t.Errorf("hostToForgeDomain(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestParseRepoFromURL(t *testing.T) {
	tests := []struct {
		name     string
		cloneURL string
		want     string
	}{
		{
			name:     "https github with .git suffix",
			cloneURL: "https://github.com/owner/repo.git",
			want:     "owner/repo",
		},
		{
			name:     "https github without .git suffix",
			cloneURL: "https://github.com/owner/repo",
			want:     "owner/repo",
		},
		{
			name:     "https with extra path segments returns full trimmed path",
			cloneURL: "https://gitlab.com/group/sub/repo.git",
			want:     "group/sub/repo",
		},
		{
			name:     "invalid url returns empty string",
			cloneURL: "://not-a-url",
			want:     "",
		},
		{
			// This is the same SSH-style remote that parseRemoteURL
			// handles correctly: parseRemoteURL special-cases the "git@"
			// prefix, but parseRepoFromURL just calls url.Parse, which
			// rejects "git@github.com:owner/repo.git" ("first path
			// segment in URL cannot contain colon"). So parseRepoFromURL
			// silently returns "" and cinch release cannot resolve the
			// repo for SSH remotes while cinch status can.
			name:     "ssh remote returns empty string",
			cloneURL: "git@github.com:owner/repo.git",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRepoFromURL(tt.cloneURL); got != tt.want {
				t.Errorf("parseRepoFromURL(%q) = %q, want %q", tt.cloneURL, got, tt.want)
			}
		})
	}
}
