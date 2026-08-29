package server

import (
	"testing"

	"github.com/ehrlich-b/cinch/internal/forge"
	"github.com/ehrlich-b/cinch/internal/storage"
)

// TestDeterminePRTrustLevel covers the trust assignment for PR webhook
// events. This is the OTHER half of the worker-trust model: the trust level
// computed here is what SelectWorkerForJob/CanWorkerRunJob gate on at
// dispatch time. NOTE: determinePRTrustLevel has a *WebhookHandler receiver
// but reads nothing from it, so we call it on a zero-value handler.
func TestDeterminePRTrustLevel(t *testing.T) {
	repo := &storage.Repo{Owner: "alice"}

	tests := []struct {
		name  string
		event *forge.PullRequestEvent
		want  storage.TrustLevel
	}{
		{
			name:  "fork PR external contributor",
			event: &forge.PullRequestEvent{IsFork: true, Sender: "mallory"},
			want:  storage.TrustExternal,
		},
		{
			name: "fork PR from repo owner is still external",
			// A PR opened from the repo owner's OWN fork (e.g. testing from a
			// personal fork) is still TrustExternal: fork status wins over
			// sender identity, as the source comment says "even if from a
			// collaborator".
			event: &forge.PullRequestEvent{IsFork: true, Sender: repo.Owner},
			want:  storage.TrustExternal,
		},
		{
			name:  "same-repo PR from owner",
			event: &forge.PullRequestEvent{IsFork: false, Sender: repo.Owner},
			want:  storage.TrustOwner,
		},
		{
			name: "same-repo PR from non-owner is collaborator",
			// Trust assumption to note: determinePRTrustLevel has NO way to
			// verify forge-side collaborator status (see the TODO comment in
			// webhook.go) — ANY non-owner same-repo PR author is trusted as a
			// collaborator. That's a documented, real assumption, so we pin it
			// down here on purpose.
			event: &forge.PullRequestEvent{IsFork: false, Sender: "bob"},
			want:  storage.TrustCollaborator,
		},
	}

	h := &WebhookHandler{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.determinePRTrustLevel(repo, tt.event); got != tt.want {
				t.Errorf("determinePRTrustLevel() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDeterminePushTrustLevel covers trust assignment for push webhook
// events. Pushes have no fork concept at all, so TrustExternal is
// unreachable through this function regardless of input.
func TestDeterminePushTrustLevel(t *testing.T) {
	repo := &storage.Repo{Owner: "alice"}

	tests := []struct {
		name  string
		event *forge.PushEvent
		want  storage.TrustLevel
	}{
		{
			name:  "push from owner",
			event: &forge.PushEvent{Sender: repo.Owner},
			want:  storage.TrustOwner,
		},
		{
			name:  "push from collaborator",
			event: &forge.PushEvent{Sender: "bob"},
			want:  storage.TrustCollaborator,
		},
	}

	h := &WebhookHandler{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.determinePushTrustLevel(repo, tt.event); got != tt.want {
				t.Errorf("determinePushTrustLevel() = %q, want %q", got, tt.want)
			}
		})
	}

	// Assert that NO input can yield TrustExternal: the function only ever
	// returns TrustOwner or TrustCollaborator, because a PushEvent has no
	// fork parameter for the function to even see.
	for _, sender := range []string{"", repo.Owner, "someone-else", "sp_aced"} {
		if got := h.determinePushTrustLevel(repo, &forge.PushEvent{Sender: sender}); got == storage.TrustExternal {
			t.Errorf("determinePushTrustLevel(sender=%q) = TrustExternal, but TrustExternal is unreachable for pushes", sender)
		}
	}
}

// TestCheckPrivateRepoAccess covers the billing gate on private repos.
//
// The success path is a single narrow check: private repo AND an org_billing
// row for (ForgeType, Owner) with Status == "active". Everything else — no
// billing row (ErrNotFound), any non-"active" status, a row for a different
// org, or even a genuine storage error — falls through to the SAME generic
// "private repos require Pro" error. Notably that means a transient DB
// hiccup denies a private build exactly the way a genuinely unpaid org
// would: the storage error is never propagated or logged specially.
func TestCheckPrivateRepoAccess(t *testing.T) {
	const forgeType = storage.ForgeTypeGitHub
	const owner = "acme"

	baseRepo := func(private bool) *storage.Repo {
		return &storage.Repo{
			ForgeType: forgeType,
			Owner:     owner,
			Private:   private,
		}
	}

	h := &WebhookHandler{}

	t.Run("public repo allowed with no billing queried", func(t *testing.T) {
		// Fresh store with ZERO org_billing rows. If checkPrivateRepoAccess
		// queried billing for a public repo it would get ErrNotFound — so
		// returning nil here also proves it never touches billing at all.
		store, err := storage.NewSQLite(":memory:", "", "")
		if err != nil {
			t.Fatalf("NewSQLite failed: %v", err)
		}
		defer store.Close()

		h.storage = store
		if err := h.checkPrivateRepoAccess(t.Context(), baseRepo(false)); err != nil {
			t.Errorf("public repo should always be allowed, got error: %v", err)
		}
	})

	t.Run("private repo with no billing row denied", func(t *testing.T) {
		store, err := storage.NewSQLite(":memory:", "", "")
		if err != nil {
			t.Fatalf("NewSQLite failed: %v", err)
		}
		defer store.Close()

		h.storage = store
		err = h.checkPrivateRepoAccess(t.Context(), baseRepo(true))
		if err == nil {
			t.Fatal("private repo with no billing row should be denied")
		}
	})

	t.Run("private repo with canceled billing denied", func(t *testing.T) {
		store, err := storage.NewSQLite(":memory:", "", "")
		if err != nil {
			t.Fatalf("NewSQLite failed: %v", err)
		}
		defer store.Close()

		if err := store.CreateOrgBilling(t.Context(), &storage.OrgBilling{
			ID:        "b_1",
			ForgeType: forgeType,
			ForgeOrg:  owner,
			Status:    "canceled",
		}); err != nil {
			t.Fatalf("CreateOrgBilling failed: %v", err)
		}

		h.storage = store
		err = h.checkPrivateRepoAccess(t.Context(), baseRepo(true))
		if err == nil {
			t.Fatal("private repo with canceled billing should be denied")
		}
	})

	t.Run("private repo with active billing allowed", func(t *testing.T) {
		store, err := storage.NewSQLite(":memory:", "", "")
		if err != nil {
			t.Fatalf("NewSQLite failed: %v", err)
		}
		defer store.Close()

		if err := store.CreateOrgBilling(t.Context(), &storage.OrgBilling{
			ID:        "b_1",
			ForgeType: forgeType,
			ForgeOrg:  owner,
			Status:    "active",
		}); err != nil {
			t.Fatalf("CreateOrgBilling failed: %v", err)
		}

		h.storage = store
		if err := h.checkPrivateRepoAccess(t.Context(), baseRepo(true)); err != nil {
			t.Errorf("private repo with active billing should be allowed, got error: %v", err)
		}
	})

	t.Run("private repo with billing for a different org denied", func(t *testing.T) {
		store, err := storage.NewSQLite(":memory:", "", "")
		if err != nil {
			t.Fatalf("NewSQLite failed: %v", err)
		}
		defer store.Close()

		// Same ForgeType, different ForgeOrg: proves the lookup is keyed on
		// forge org as well, not forge type alone.
		if err := store.CreateOrgBilling(t.Context(), &storage.OrgBilling{
			ID:        "b_1",
			ForgeType: forgeType,
			ForgeOrg:  "some-other-org",
			Status:    "active",
		}); err != nil {
			t.Fatalf("CreateOrgBilling failed: %v", err)
		}

		h.storage = store
		err = h.checkPrivateRepoAccess(t.Context(), baseRepo(true))
		if err == nil {
			t.Fatal("private repo with billing for a different org should be denied")
		}
	})

	t.Run("private repo with active billing for a different forge denied", func(t *testing.T) {
		store, err := storage.NewSQLite(":memory:", "", "")
		if err != nil {
			t.Fatalf("NewSQLite failed: %v", err)
		}
		defer store.Close()

		// Same ForgeOrg, different ForgeType: proves the lookup is keyed on
		// both columns.
		if err := store.CreateOrgBilling(t.Context(), &storage.OrgBilling{
			ID:        "b_1",
			ForgeType: storage.ForgeTypeGitLab,
			ForgeOrg:  owner,
			Status:    "active",
		}); err != nil {
			t.Fatalf("CreateOrgBilling failed: %v", err)
		}

		h.storage = store
		err = h.checkPrivateRepoAccess(t.Context(), baseRepo(true))
		if err == nil {
			t.Fatal("private repo with billing for a different forge should be denied")
		}
	})

	t.Run("private repo with storage error denied like unpaid org", func(t *testing.T) {
		store, err := storage.NewSQLite(":memory:", "", "")
		if err != nil {
			t.Fatalf("NewSQLite failed: %v", err)
		}
		// Deliberately close the store so GetOrgBilling fails with a genuine
		// storage error. The generic "private repos require Pro" error is the
		// ONLY outcome — a transient DB hiccup denies a build the exact same
		// way a genuinely unpaid org would. Mentioned in comments because the
		// error path is silently merged with the no-billing path.
		store.Close()

		h.storage = store
		err = h.checkPrivateRepoAccess(t.Context(), baseRepo(true))
		if err == nil {
			t.Fatal("private repo with a storage error should be denied")
		}
	})
}
