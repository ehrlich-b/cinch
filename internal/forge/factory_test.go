package forge

import "testing"

func TestNewGitHub(t *testing.T) {
	f := New(ForgeConfig{Type: TypeGitHub, Token: "gh_token", BaseURL: "https://ghe.example.com"})
	if f == nil {
		t.Fatal("New(TypeGitHub) returned nil")
	}
	gh, ok := f.(*GitHub)
	if !ok {
		t.Fatalf("New(TypeGitHub) returned type %T, want *GitHub", f)
	}
	if gh.Token != "gh_token" {
		t.Errorf("Token = %q, want %q", gh.Token, "gh_token")
	}
}

func TestNewGitLab(t *testing.T) {
	f := New(ForgeConfig{Type: TypeGitLab, Token: "gl_token", BaseURL: "https://gitlab.example.com"})
	if f == nil {
		t.Fatal("New(TypeGitLab) returned nil")
	}
	gl, ok := f.(*GitLab)
	if !ok {
		t.Fatalf("New(TypeGitLab) returned type %T, want *GitLab", f)
	}
	if gl.Token != "gl_token" {
		t.Errorf("Token = %q, want %q", gl.Token, "gl_token")
	}
	if gl.BaseURL != "https://gitlab.example.com" {
		t.Errorf("BaseURL = %q, want %q", gl.BaseURL, "https://gitlab.example.com")
	}
}

func TestNewForgejo(t *testing.T) {
	f := New(ForgeConfig{Type: TypeForgejo, Token: "fj_token", BaseURL: "https://codeberg.example.com"})
	if f == nil {
		t.Fatal("New(TypeForgejo) returned nil")
	}
	fj, ok := f.(*Forgejo)
	if !ok {
		t.Fatalf("New(TypeForgejo) returned type %T, want *Forgejo", f)
	}
	if fj.Token != "fj_token" {
		t.Errorf("Token = %q, want %q", fj.Token, "fj_token")
	}
	if fj.BaseURL != "https://codeberg.example.com" {
		t.Errorf("BaseURL = %q, want %q", fj.BaseURL, "https://codeberg.example.com")
	}
	if fj.IsGitea {
		t.Error("IsGitea = true, want false for TypeForgejo")
	}
	if name := fj.Name(); name != "forgejo" {
		t.Errorf("Name() = %q, want %q", name, "forgejo")
	}
}

func TestNewGitea(t *testing.T) {
	f := New(ForgeConfig{Type: TypeGitea, Token: "gt_token", BaseURL: "https://gitea.example.com"})
	if f == nil {
		t.Fatal("New(TypeGitea) returned nil")
	}
	g, ok := f.(*Forgejo)
	if !ok {
		t.Fatalf("New(TypeGitea) returned type %T, want *Forgejo", f)
	}
	if g.Token != "gt_token" {
		t.Errorf("Token = %q, want %q", g.Token, "gt_token")
	}
	if g.BaseURL != "https://gitea.example.com" {
		t.Errorf("BaseURL = %q, want %q", g.BaseURL, "https://gitea.example.com")
	}
	if !g.IsGitea {
		t.Error("IsGitea = false, want true for TypeGitea")
	}
	if name := g.Name(); name != "gitea" {
		t.Errorf("Name() = %q, want %q", name, "gitea")
	}
}

func TestNewUnknownTypes(t *testing.T) {
	cases := []struct {
		name string
		typ  string
	}{
		{"unrecognized", "bitbucket"},
		{"empty", ""},
		{"caseMismatch", "GitHub"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if f := New(ForgeConfig{Type: tc.typ, Token: "tok", BaseURL: "https://example.com"}); f != nil {
				t.Errorf("New(%q) returned %T, want nil", tc.typ, f)
			}
		})
	}
}

func TestRepoFullNameDirect(t *testing.T) {
	cases := []struct {
		name  string
		owner string
		repo  string
		want  string
	}{
		{"normal", "owner", "repo", "owner/repo"},
		{"emptyOwner", "", "repo", "/repo"},
		{"emptyName", "owner", "", "owner/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Repo{Owner: tc.owner, Name: tc.repo}
			if got := r.FullName(); got != tc.want {
				t.Errorf("FullName() = %q, want %q", got, tc.want)
			}
		})
	}
}
