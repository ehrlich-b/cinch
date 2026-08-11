package forge

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

// conformanceFixture describes one forge implementation together with the
// realistic pull_request webhook payloads, the header names it uses, and the
// function that computes a valid auth header for a given body and secret.
//
// The auth schemes intentionally differ per forge:
//   - GitHub and Forgejo verify an HMAC-SHA256 signature over the body.
//   - GitLab compares a plain secret token header (X-Gitlab-Token).
//
// Every behavioural assertion in TestForgeParsePullRequestConformance runs
// against every fixture, so a future forge cannot quietly skip a case.
type conformanceFixture struct {
	name string
	// forge is the implementation under test.
	forge Forge
	// secret is the webhook secret the receiver is configured with.
	secret string

	// eventHeader/eventValue identify a pull_request webhook for this forge.
	eventHeader string
	// eventValue is the event type value for a pull_request webhook.
	eventValue string
	// pushEventValue is the event type value for a push webhook.
	pushEventValue string
	// authHeader is the header that carries the forge's signature/token.
	authHeader string
	// sign computes the auth header value for a given body and secret.
	sign func(body []byte, secret string) string

	// prBody is a realistic pull_request webhook opened from a fork.
	prBody []byte
	// want is the PullRequestEvent prBody must parse to.
	want PullRequestEvent
	// sameRepoBody is a pull_request webhook for a branch on the same repo.
	sameRepoBody []byte
	// pushBody is a realistic push webhook for the same repository. It is
	// used to prove ParsePullRequest rejects a push event.
	pushBody []byte
}

// hmacSHA256Hex returns the hex-encoded HMAC-SHA256 of body keyed by secret.
func hmacSHA256Hex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// eventRequest builds a webhook request carrying the given body and forging
// the given event-type header.
func eventRequest(t *testing.T, eventHeader, eventValue string, body []byte) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set(eventHeader, eventValue)
	return req
}

// signedRequest builds a pull_request webhook request whose auth header is
// computed with sign(body, secret), i.e. a request a genuine webhook from the
// forge would look like.
func (fx conformanceFixture) signedRequest(t *testing.T, body []byte, secret string) *http.Request {
	t.Helper()
	req := eventRequest(t, fx.eventHeader, fx.eventValue, body)
	req.Header.Set(fx.authHeader, fx.sign(body, secret))
	return req
}

// comparePullRequestEvents asserts every conformance-relevant field of ev.
func comparePullRequestEvents(t *testing.T, ev *PullRequestEvent, want PullRequestEvent) {
	t.Helper()
	if ev == nil {
		t.Fatal("ParsePullRequest returned a nil event")
	}
	if ev.Number != want.Number {
		t.Errorf("Number = %d, want %d", ev.Number, want.Number)
	}
	if ev.Action != want.Action {
		t.Errorf("Action = %q, want %q", ev.Action, want.Action)
	}
	if ev.Commit != want.Commit {
		t.Errorf("Commit = %q, want %q", ev.Commit, want.Commit)
	}
	if ev.HeadBranch != want.HeadBranch {
		t.Errorf("HeadBranch = %q, want %q", ev.HeadBranch, want.HeadBranch)
	}
	if ev.BaseBranch != want.BaseBranch {
		t.Errorf("BaseBranch = %q, want %q", ev.BaseBranch, want.BaseBranch)
	}
	if ev.Title != want.Title {
		t.Errorf("Title = %q, want %q", ev.Title, want.Title)
	}
	if ev.Sender != want.Sender {
		t.Errorf("Sender = %q, want %q", ev.Sender, want.Sender)
	}
	if ev.IsFork != want.IsFork {
		t.Errorf("IsFork = %v, want %v", ev.IsFork, want.IsFork)
	}
	if ev.Repo == nil {
		t.Fatal("ParsePullRequest returned a nil Repo")
	}
	if got := ev.Repo.FullName(); got != want.Repo.FullName() {
		t.Errorf("Repo.FullName() = %q, want %q", got, want.Repo.FullName())
	}
	if ev.Repo.ForgeType != want.Repo.ForgeType {
		t.Errorf("Repo.ForgeType = %q, want %q", ev.Repo.ForgeType, want.Repo.ForgeType)
	}
	if ev.Repo.CloneURL != want.Repo.CloneURL {
		t.Errorf("Repo.CloneURL = %q, want %q", ev.Repo.CloneURL, want.Repo.CloneURL)
	}
	if ev.Repo.HTMLURL != want.Repo.HTMLURL {
		t.Errorf("Repo.HTMLURL = %q, want %q", ev.Repo.HTMLURL, want.Repo.HTMLURL)
	}
	if ev.Repo.Private != want.Repo.Private {
		t.Errorf("Repo.Private = %v, want %v", ev.Repo.Private, want.Repo.Private)
	}
}

// tamperPR flips a single byte of a valid JSON webhook body so it no longer
// represents the original document. Every fixture body starts with the '{'
// that opens the JSON object; flipping that byte to '[' guarantees the
// tampered body cannot parse back to the original event, independent of
// forge.
func tamperPR(body []byte) []byte {
	tampered := append([]byte(nil), body...)
	tampered[0] = '['
	return tampered
}

const conformanceSecret = "conformance-webhook-secret"

func TestForgeParsePullRequestConformance(t *testing.T) {
	const commitSHA = "a1b2c3d4e5f60718293a4b5c6d7e8f9001a2b3c4"

	fixtures := []conformanceFixture{
		{
			name:           "github",
			forge:          &GitHub{},
			secret:         conformanceSecret,
			eventHeader:    "X-GitHub-Event",
			eventValue:     "pull_request",
			pushEventValue: "push",
			authHeader:     "X-Hub-Signature-256",
			sign: func(body []byte, secret string) string {
				return "sha256=" + hmacSHA256Hex(secret, body)
			},
			// Realistic GitHub pull_request webhook (opened) trimmed to the
			// fields the parser reads. opened from a fork named "acetreasure".
			prBody: []byte(`{
				"action": "opened",
				"number": 12,
				"pull_request": {
					"number": 12,
					"title": "Add CI badge to README",
					"head": {
						"sha": "` + commitSHA + `",
						"ref": "feature/ci-badge",
						"repo": {
							"full_name": "acetreasure/myrepo"
						}
					},
					"base": {
						"ref": "main"
					}
				},
				"repository": {
					"name": "myrepo",
					"full_name": "alice/myrepo",
					"private": false,
					"html_url": "https://github.com/alice/myrepo",
					"clone_url": "https://github.com/alice/myrepo.git",
					"owner": {
						"login": "alice"
					}
				},
				"sender": {
					"login": "acetreasure"
				}
			}`),
			want: PullRequestEvent{
				Repo: &Repo{
					ForgeType: "github",
					Owner:     "alice",
					Name:      "myrepo",
					CloneURL:  "https://github.com/alice/myrepo.git",
					HTMLURL:   "https://github.com/alice/myrepo",
					Private:   false,
				},
				Number:     12,
				Action:     "opened",
				Commit:     commitSHA,
				HeadBranch: "feature/ci-badge",
				BaseBranch: "main",
				Title:      "Add CI badge to README",
				Sender:     "acetreasure",
				IsFork:     true,
			},
			// Same PR number/tip, but the head branch lives in alice/myrepo.
			sameRepoBody: []byte(`{
				"action": "opened",
				"number": 12,
				"pull_request": {
					"number": 12,
					"title": "Add CI badge to README",
					"head": {
						"sha": "` + commitSHA + `",
						"ref": "feature/ci-badge",
						"repo": {
							"full_name": "alice/myrepo"
						}
					},
					"base": {
						"ref": "main"
					}
				},
				"repository": {
					"name": "myrepo",
					"full_name": "alice/myrepo",
					"private": false,
					"html_url": "https://github.com/alice/myrepo",
					"clone_url": "https://github.com/alice/myrepo.git",
					"owner": {
						"login": "alice"
					}
				},
				"sender": {
					"login": "alice"
				}
			}`),
			pushBody: []byte(`{
				"ref": "refs/heads/main",
				"before": "0000000000000000000000000000000000000000",
				"after": "` + commitSHA + `",
				"deleted": false,
				"repository": {
					"name": "myrepo",
					"full_name": "alice/myrepo",
					"private": false,
					"html_url": "https://github.com/alice/myrepo",
					"clone_url": "https://github.com/alice/myrepo.git",
					"owner": {
						"login": "alice"
					}
				},
				"sender": {
					"login": "alice"
				}
			}`),
		},
		{
			name:           "gitlab",
			forge:          &GitLab{},
			secret:         conformanceSecret,
			eventHeader:    "X-Gitlab-Event",
			eventValue:     "Merge Request Hook",
			pushEventValue: "Push Hook",
			authHeader:     "X-Gitlab-Token",
			sign: func(_ []byte, secret string) string {
				// GitLab authenticates with the plain shared secret, not an
				// HMAC of the body. The header value is the secret itself.
				return secret
			},
			// Realistic GitLab merge_request webhook (open) trimmed to the
			// fields the parser reads. The source project (40299811) differs
			// from the target project (3319013): opened from a fork.
			prBody: []byte(`{
				"object_kind": "merge_request",
				"project": {
					"id": 3319013,
					"name": "myproject",
					"namespace": "alice",
					"path_with_namespace": "alice/myproject",
					"web_url": "https://gitlab.com/alice/myproject",
					"git_http_url": "https://gitlab.com/alice/myproject.git",
					"visibility_level": 20
				},
				"object_attributes": {
					"iid": 7,
					"action": "open",
					"title": "Add CI badge to README",
					"source_branch": "feature/ci-badge",
					"target_branch": "main",
					"source_project_id": 40299811,
					"target_project_id": 3319013,
					"last_commit": {
						"id": "` + commitSHA + `"
					}
				},
				"user": {
					"username": "bobfork"
				}
			}`),
			want: PullRequestEvent{
				Repo: &Repo{
					ForgeType: "gitlab",
					Owner:     "alice",
					Name:      "myproject",
					CloneURL:  "https://gitlab.com/alice/myproject.git",
					HTMLURL:   "https://gitlab.com/alice/myproject",
					Private:   false,
				},
				Number:     7,
				Action:     "open",
				Commit:     commitSHA,
				HeadBranch: "feature/ci-badge",
				BaseBranch: "main",
				Title:      "Add CI badge to README",
				Sender:     "bobfork",
				IsFork:     true,
			},
			// Same MR, but the source project equals the target project.
			sameRepoBody: []byte(`{
				"object_kind": "merge_request",
				"project": {
					"id": 3319013,
					"name": "myproject",
					"namespace": "alice",
					"path_with_namespace": "alice/myproject",
					"web_url": "https://gitlab.com/alice/myproject",
					"git_http_url": "https://gitlab.com/alice/myproject.git",
					"visibility_level": 20
				},
				"object_attributes": {
					"iid": 7,
					"action": "open",
					"title": "Add CI badge to README",
					"source_branch": "feature/ci-badge",
					"target_branch": "main",
					"source_project_id": 3319013,
					"target_project_id": 3319013,
					"last_commit": {
						"id": "` + commitSHA + `"
					}
				},
				"user": {
					"username": "alice"
				}
			}`),
			pushBody: []byte(`{
				"object_kind": "push",
				"ref": "refs/heads/main",
				"before": "0000000000000000000000000000000000000000",
				"after": "` + commitSHA + `",
				"user_username": "alice",
				"project": {
					"id": 3319013,
					"name": "myproject",
					"namespace": "alice",
					"path_with_namespace": "alice/myproject",
					"web_url": "https://gitlab.com/alice/myproject",
					"git_http_url": "https://gitlab.com/alice/myproject.git",
					"visibility_level": 20
				}
			}`),
		},
		{
			name:           "forgejo",
			forge:          &Forgejo{},
			secret:         conformanceSecret,
			eventHeader:    "X-Forgejo-Event",
			eventValue:     "pull_request",
			pushEventValue: "push",
			authHeader:     "X-Forgejo-Signature",
			sign: func(body []byte, secret string) string {
				return hmacSHA256Hex(secret, body)
			},
			// Realistic Forgejo pull_request webhook (opened) trimmed to the
			// fields the parser reads. opened from a fork named "acetreasure".
			prBody: []byte(`{
				"action": "opened",
				"number": 3,
				"pull_request": {
					"number": 3,
					"title": "Add CI badge to README",
					"head": {
						"sha": "` + commitSHA + `",
						"ref": "feature/ci-badge",
						"repo": {
							"full_name": "acetreasure/myrepo"
						}
					},
					"base": {
						"ref": "main"
					}
				},
				"repository": {
					"name": "myrepo",
					"full_name": "alice/myrepo",
					"private": false,
					"html_url": "https://codeberg.org/alice/myrepo",
					"clone_url": "https://codeberg.org/alice/myrepo.git",
					"owner": {
						"username": "alice"
					}
				},
				"sender": {
					"username": "acetreasure"
				}
			}`),
			want: PullRequestEvent{
				Repo: &Repo{
					ForgeType: "forgejo",
					Owner:     "alice",
					Name:      "myrepo",
					CloneURL:  "https://codeberg.org/alice/myrepo.git",
					HTMLURL:   "https://codeberg.org/alice/myrepo",
					Private:   false,
				},
				Number:     3,
				Action:     "opened",
				Commit:     commitSHA,
				HeadBranch: "feature/ci-badge",
				BaseBranch: "main",
				Title:      "Add CI badge to README",
				Sender:     "acetreasure",
				IsFork:     true,
			},
			// Same PR, but the head branch lives in alice/myrepo.
			sameRepoBody: []byte(`{
				"action": "opened",
				"number": 3,
				"pull_request": {
					"number": 3,
					"title": "Add CI badge to README",
					"head": {
						"sha": "` + commitSHA + `",
						"ref": "feature/ci-badge",
						"repo": {
							"full_name": "alice/myrepo"
						}
					},
					"base": {
						"ref": "main"
					}
				},
				"repository": {
					"name": "myrepo",
					"full_name": "alice/myrepo",
					"private": false,
					"html_url": "https://codeberg.org/alice/myrepo",
					"clone_url": "https://codeberg.org/alice/myrepo.git",
					"owner": {
						"username": "alice"
					}
				},
				"sender": {
					"username": "alice"
				}
			}`),
			pushBody: []byte(`{
				"ref": "refs/heads/main",
				"before": "0000000000000000000000000000000000000000",
				"after": "` + commitSHA + `",
				"repository": {
					"name": "myrepo",
					"full_name": "alice/myrepo",
					"private": false,
					"html_url": "https://codeberg.org/alice/myrepo",
					"clone_url": "https://codeberg.org/alice/myrepo.git",
					"owner": {
						"username": "alice"
					}
				},
				"sender": {
					"username": "alice"
				}
			}`),
		},
	}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			// 1. A valid PR webhook with a correct signature/token parses to
			// the expected PullRequestEvent.
			t.Run("parses valid pull_request webhook", func(t *testing.T) {
				req := fx.signedRequest(t, fx.prBody, fx.secret)
				ev, err := fx.forge.ParsePullRequest(req, fx.secret)
				if err != nil {
					t.Fatalf("ParsePullRequest(valid webhook) failed: %v", err)
				}
				comparePullRequestEvents(t, ev, fx.want)
			})

			// 2. A body tampered with after the signature was computed (one
			// byte flipped, original header preserved) is rejected.
			//
			// GitHub/Forgejo reject at the HMAC signature check because the
			// signature no longer matches the body. GitLab compares only the
			// plain X-Gitlab-Token header, which is unchanged by a body
			// edit, so rejection relies on the tampered body no longer
			// parsing as the original JSON document.
			t.Run("rejects tampered body with original header", func(t *testing.T) {
				tampered := tamperPR(fx.prBody)
				req := eventRequest(t, fx.eventHeader, fx.eventValue, tampered)
				req.Header.Set(fx.authHeader, fx.sign(fx.prBody, fx.secret))
				if _, err := fx.forge.ParsePullRequest(req, fx.secret); err == nil {
					t.Fatal("ParsePullRequest accepted a tampered body")
				}
			})

			// 3. The wrong secret is rejected.
			t.Run("rejects wrong secret", func(t *testing.T) {
				req := eventRequest(t, fx.eventHeader, fx.eventValue, fx.prBody)
				req.Header.Set(fx.authHeader, fx.sign(fx.prBody, "wrong-"+fx.secret))
				if _, err := fx.forge.ParsePullRequest(req, fx.secret); err == nil {
					t.Fatal("ParsePullRequest accepted a webhook signed with the wrong secret")
				}
			})

			// 4. A missing auth header with a non-empty secret is rejected.
			t.Run("rejects missing auth header", func(t *testing.T) {
				req := eventRequest(t, fx.eventHeader, fx.eventValue, fx.prBody)
				if _, err := fx.forge.ParsePullRequest(req, fx.secret); err == nil {
					t.Fatal("ParsePullRequest accepted a webhook with no signature/token")
				}
			})

			// 5. An empty secret disables verification, exactly as the push
			// path does in every implementation: the auth block is skipped
			// entirely, so the body parses with no header and even a bogus
			// header is ignored. All three implementations behave the same
			// here: there is no divergence to special-case.
			t.Run("empty secret skips verification", func(t *testing.T) {
				req := eventRequest(t, fx.eventHeader, fx.eventValue, fx.prBody)
				ev, err := fx.forge.ParsePullRequest(req, "")
				if err != nil {
					t.Fatalf("ParsePullRequest with empty secret and no header failed: %v", err)
				}
				comparePullRequestEvents(t, ev, fx.want)

				req = eventRequest(t, fx.eventHeader, fx.eventValue, fx.prBody)
				req.Header.Set(fx.authHeader, "bogus-header-value-ignored")
				if _, err := fx.forge.ParsePullRequest(req, ""); err != nil {
					t.Fatalf("ParsePullRequest with empty secret ignored a bogus header: %v", err)
				}
			})

			// 6. A webhook whose event type is a PUSH is rejected by
			// ParsePullRequest, even though the same webhook is a valid push
			// (ParsePush accepts it).
			t.Run("rejects push webhook", func(t *testing.T) {
				req := eventRequest(t, fx.eventHeader, fx.pushEventValue, fx.pushBody)
				req.Header.Set(fx.authHeader, fx.sign(fx.pushBody, fx.secret))
				if _, err := fx.forge.ParsePullRequest(req, fx.secret); err == nil {
					t.Fatal("ParsePullRequest accepted a push webhook")
				}

				// The push body must be a genuine push webhook, otherwise the
				// rejection above would be trivially due to a bad payload.
				pushReq := eventRequest(t, fx.eventHeader, fx.pushEventValue, fx.pushBody)
				pushReq.Header.Set(fx.authHeader, fx.sign(fx.pushBody, fx.secret))
				if _, err := fx.forge.ParsePush(pushReq, fx.secret); err != nil {
					t.Fatalf("push webhook should parse as a push, got error: %v", err)
				}
			})

			// 7. Fork detection: a PR opened from a fork sets IsFork true; a
			// PR from a branch on the same repo sets it false.
			t.Run("fork detection", func(t *testing.T) {
				tests := []struct {
					name string
					body []byte
					want bool
				}{
					{name: "opened from fork", body: fx.prBody, want: true},
					{name: "branch on same repo", body: fx.sameRepoBody, want: false},
				}
				for _, tt := range tests {
					t.Run(tt.name, func(t *testing.T) {
						req := fx.signedRequest(t, tt.body, fx.secret)
						ev, err := fx.forge.ParsePullRequest(req, fx.secret)
						if err != nil {
							t.Fatalf("ParsePullRequest failed: %v", err)
						}
						if ev.IsFork != tt.want {
							t.Errorf("IsFork = %v, want %v", ev.IsFork, tt.want)
						}
					})
				}
			})
		})
	}
}
