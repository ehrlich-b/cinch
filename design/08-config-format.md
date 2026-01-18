# Configuration Format Deep Dive

## Why This Matters

The config file is cinch's primary user interface. Most users will interact with cinch by:
1. Writing/editing the config file
2. Running `git push`

That's it. The config file IS the product experience. Get it wrong, and every user interaction is painful.

## The Constraints

1. **Simplicity** - The whole point is `command = "make ci"`. Don't make it complex.
2. **Familiarity** - Developers have opinions. Strong ones.
3. **No footguns** - YAML's implicit typing has ruined careers.
4. **Tooling** - Syntax highlighting, validation, IDE support.
5. **Comments** - People need to explain their configs.
6. **Version control** - Diffs should be readable.

## Format Options

### TOML

```toml
command = "make ci"

[trigger]
branches = ["main", "develop"]
pull_requests = true

[runner]
labels = ["linux"]
```

**Pros:**
- Explicit types (strings are strings, not booleans or floats)
- Comments supported
- Flat, readable
- No indentation hell
- Rust/Go communities love it

**Cons:**
- Less familiar to JS/web developers
- Table syntax `[section]` feels dated to some
- Nested structures get ugly: `[a.b.c.d]`
- "Why not just YAML?"

**Footgun potential:** Low. TOML is boringly safe.

### YAML

```yaml
command: make ci

trigger:
  branches:
    - main
    - develop
  pull_requests: true

runner:
  labels:
    - linux
```

**Pros:**
- Most familiar (GitHub Actions, Kubernetes, Ansible)
- Clean multiline strings
- Widely supported in editors
- "I already know this"

**Cons:**
- The Norway Problem: `country: NO` → `country: false`
- `on: true` → `{true: true}` (GitHub Actions actually hit this)
- Indentation matters (whitespace bugs)
- 1.1 vs 1.2 spec differences
- `:` in values requires quoting
- Implicit typing is a security risk (billion laughs attack vectors)
- People HATE debugging YAML indent issues

**Footgun potential:** HIGH. YAML has destroyed production systems.

### JSON

```json
{
  "command": "make ci",
  "trigger": {
    "branches": ["main", "develop"],
    "pull_requests": true
  },
  "runner": {
    "labels": ["linux"]
  }
}
```

**Pros:**
- Universal - every language parses it
- Explicit types
- No ambiguity
- Everyone knows it

**Cons:**
- No comments (deal-breaker for many)
- Verbose (all those quotes and braces)
- Trailing comma errors
- Diffs are noisy

**Footgun potential:** Low, but the no-comments thing is brutal.

### JSON5 / JSONC

```jsonc
{
  // This is the main CI command
  command: "make ci",

  trigger: {
    branches: ["main", "develop"],
    pull_requests: true,  // trailing comma OK
  },

  runner: {
    labels: ["linux"],
  }
}
```

**Pros:**
- Comments!
- Trailing commas
- Unquoted keys
- Familiar to JS developers
- VS Code natively supports JSONC

**Cons:**
- Not actually JSON (can't use `JSON.parse`)
- Less universal than JSON
- Some may find it confusing ("wait, is this JSON?")

**Footgun potential:** Low.

### Dhall / CUE / Nickel (Configuration Languages)

```dhall
{ command = "make ci"
, trigger = { branches = ["main"], pull_requests = True }
}
```

**Pros:**
- Type-safe, validated at parse time
- Programmable (functions, imports)
- Can't represent invalid configs

**Cons:**
- Learning curve
- "WTF is this syntax"
- Tooling less mature
- Overkill for simple configs

**Footgun potential:** Low, but adoption barrier is high.

### No Config / Convention Over Configuration

```bash
# No config file at all. Just run:
make ci
# Or detect package.json → npm test
# Or detect Cargo.toml → cargo test
```

**Pros:**
- Zero config for common cases
- Can't misconfigure what doesn't exist
- Maximum simplicity

**Cons:**
- How do you customize anything?
- Different projects have different conventions
- Implicit magic annoys people

**Footgun potential:** Medium (surprising defaults).

## The Multi-Format Approach

Many tools support multiple formats. Users choose what they like:

```
cinch will look for (in order):
1. .cinch.yaml / .cinch.yml
2. .cinch.toml
3. .cinch.json
4. cinch.yaml / cinch.yml  (no dot)
5. cinch.toml
6. cinch.json
```

**Examples of tools that do this:**
- Prettier: `.prettierrc`, `.prettierrc.json`, `.prettierrc.yaml`, `.prettierrc.toml`, `prettier.config.js`
- ESLint: `.eslintrc.json`, `.eslintrc.yaml`, `.eslintrc.js`
- Babel: `babel.config.json`, `babel.config.js`, `.babelrc`
- Jest: `jest.config.js`, `jest.config.json`

**Pros:**
- User choice
- Familiar format for their ecosystem
- Go devs use TOML, JS devs use JSON, DevOps use YAML
- No format wars

**Cons:**
- More code to maintain (3 parsers)
- Documentation needs examples in all formats
- "Which one should I use?" decision paralysis
- Potential subtle differences in parsing

## Recommendation: Support All Three Formats

After consideration, there's no good reason to exclude any format. The parsing cost is trivial - we compose into a common config object regardless. Let users use what they prefer.

### YAML
- It's what people expect from CI config (GitHub Actions trained everyone)
- Muscle memory matters
- Parse STRICTLY: YAML 1.2 only, fail loud on ambiguous values

### JSON
- Universal fallback
- Good for programmatic generation
- Comments via JSONC parser (allow `//` comments)

### TOML
- Rust/Go developers prefer it
- Explicit types, no footguns
- Clean and readable

### Strict YAML Parsing

```go
// Use yaml.v3 with strict settings
decoder := yaml.NewDecoder(r)
decoder.KnownFields(true)  // Error on unknown fields

// Post-parse validation
func validateConfig(c *Config) error {
    // Reject suspicious values that YAML might have mangled
    if c.Command == "true" || c.Command == "false" {
        return errors.New("command looks like it was parsed as boolean - quote it")
    }
    // ... more safety checks
}
```

### The Config Schema

Regardless of format, the schema is:

```yaml
# Required: what to run
command: make ci

# Optional: container settings (see 09-containerization.md)
container:
  image: node:20-alpine           # explicit image
  # OR
  dockerfile: ./Dockerfile.ci      # build from this
  # OR
  devcontainer: true              # use .devcontainer/ (default if exists)
  # OR
  # container: none               # bare metal (no container)

# Optional: persistent caches (mounted into container)
cache:
  - name: dependencies
    path: ./node_modules
  - name: build-cache
    path: ./.next/cache

# Optional: when to run (defaults to always)
trigger:
  branches: [main, develop]      # default: all branches
  pull_requests: true            # default: true
  paths: ["src/**", "tests/**"]  # default: all paths
  paths_ignore: ["docs/**"]      # default: none
  schedule: "0 0 * * *"          # cron expression for scheduled builds
  manual: true                   # allow manual trigger from UI/API

# Optional: which worker(s) run this
workers: [linux-amd64, linux-arm64]  # fan-out: one job per label
# OR for single worker:
# runner:
#   labels: [linux, amd64]       # default: any worker

# Optional: limits
timeout: 30m                      # default: 30m

# Optional: extra env vars (non-secret)
env:
  CI: "true"
  NODE_ENV: test

# Optional: files to extract after build
artifacts:
  - path: ./dist
    name: build-output
```

## File Discovery Order

```
.cinch.yaml    ← Prefer dot-prefix (hidden file)
.cinch.yml
.cinch.toml
.cinch.json
cinch.yaml     ← Non-hidden fallback
cinch.yml
cinch.toml
cinch.json
```

## Alternative: Hybrid Approach

What if the config is SO simple it barely needs a file?

```yaml
# Minimal: just the command
command: make ci
```

Or even inline in a comment in another file:

```makefile
# cinch: make ci
```

This is probably too clever. But worth considering for the "zero friction" angle.

## Edge Cases

### Multi-command

Some users want to run multiple commands:

```yaml
# Option A: Shell string (current design)
command: make lint && make test && make build

# Option B: Array (explicit)
command:
  - make lint
  - make test
  - make build

# Option C: Named steps (getting complex...)
steps:
  - name: lint
    command: make lint
  - name: test
    command: make test
```

**Recommendation:** Option A (shell string). Keep it simple. Users can put complex logic in their Makefile.

### Matrix Builds

```yaml
# We said we don't do this. But if we did...
matrix:
  os: [ubuntu, macos]
  node: [18, 20]
```

**Recommendation:** Don't. Use separate config files or run locally on multiple machines. This is a slippery slope to GitHub Actions complexity.

### Secrets

```yaml
# DO NOT DO THIS - secrets in config file
env:
  AWS_ACCESS_KEY: AKIA...  # NO!

# Instead: reference env vars on worker
# Worker runs with AWS_ACCESS_KEY in its environment
# Command just uses $AWS_ACCESS_KEY
```

**Recommendation:** Never support secrets in config. They're env vars on the worker machine.

## Documentation Format

README and docs should show both formats:

```markdown
## Configuration

Create `.cinch.yaml` in your repo root:

\```yaml
command: make ci
\```

Or if you prefer JSON, create `.cinch.json`:

\```json
{
  "command": "make ci"
}
\```
```

## Final Decision

| Format | Support | File Names |
|--------|---------|------------|
| YAML | Yes | .cinch.yaml, .cinch.yml, cinch.yaml, cinch.yml |
| TOML | Yes | .cinch.toml, cinch.toml |
| JSON | Yes | .cinch.json, cinch.json |

Rationale:
1. All formats compose to the same config struct
2. Parsing cost is trivial - just import three libraries
3. Let users match their repo's style
4. No format wars

## Implementation Notes

```go
func LoadConfig(repoPath string) (*Config, error) {
    // Discovery order - first match wins
    candidates := []struct {
        name   string
        parser func([]byte) (*Config, error)
    }{
        {".cinch.yaml", parseYAML},
        {".cinch.yml", parseYAML},
        {".cinch.toml", parseTOML},
        {".cinch.json", parseJSON},
        {"cinch.yaml", parseYAML},
        {"cinch.yml", parseYAML},
        {"cinch.toml", parseTOML},
        {"cinch.json", parseJSON},
    }

    for _, c := range candidates {
        path := filepath.Join(repoPath, c.name)
        if data, err := os.ReadFile(path); err == nil {
            return c.parser(data)
        }
    }

    return nil, ErrNoConfig
}
```

## Open Questions

1. **Should we support `package.json` "cinch" key?** (Like how Jest supports `"jest": {...}` in package.json)
   - Pro: No extra file for JS projects
   - Con: Mixing concerns

2. **Should we auto-detect common commands?** (No config = try `make ci`, `npm test`, etc.)
   - Pro: Zero config for common cases
   - Con: Surprising behavior

3. **Should we support `.cinchrc`?** (Dotfile convention)
   - Pro: Familiar pattern
   - Con: Yet another filename

For v0.1: Keep it simple. `.cinch.yaml` and `.cinch.json`. Revisit after user feedback.
