package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Client is the surface used by the poller, the app bindings, and by
// `revu doctor`.
type Client interface {
	AuthStatus(ctx context.Context) error
	ListReviewRequested(ctx context.Context) ([]PRSummary, error)
	GetPRDetails(ctx context.Context, url string) (*PRDetails, error)
	GetPRFullDetails(ctx context.Context, url string) (*PRFullDetails, error)
	GetPRDiff(ctx context.Context, url string) (string, error)
	MergePR(ctx context.Context, url string, method MergeMethod) error
	ApprovePR(ctx context.Context, url string) error
}

// detailsCacheTTL is how long GetPRFullDetails / GetPRDiff results stay cached
// in memory. Short enough that stale mergeable state / review counts don't
// linger past a realistic "click around the details view" session, long
// enough to absorb double-clicks and back-and-forth navigation.
const detailsCacheTTL = 30 * time.Second

// reviewStatePending é o estado neutro normalizado quando não há review
// resolvido pra `login` (sem reviewer, em DISMISSED, ou sem matches).
const reviewStatePending = "PENDING"

// reviewStateFor picks the review state for login among latestReviews and
// normalizes it to the four-value domain persisted by the store. Any review
// in states GitHub doesn't expose here (PENDING, DISMISSED) collapses into
// "PENDING" so the UI can show "still yours to review".
func reviewStateFor(login string, reviews []rawLatestReview) string {
	if login == "" {
		return reviewStatePending
	}
	for _, r := range reviews {
		if r.Author.Login != login {
			continue
		}
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED", "COMMENTED":
			return r.State
		default:
			return reviewStatePending
		}
	}
	return reviewStatePending
}

// TokenProvider returns the PAT for the currently active profile, or "" when
// the profile defers to the ambient `gh auth login` session. The client
// resolves it per call so switching profiles takes effect immediately, and
// so tokens never live on a long-lived struct field.
type TokenProvider interface {
	TokenForActive(ctx context.Context) (string, error)
}

// noopTokenProvider satisfies TokenProvider by always returning "". Used when
// the caller has no profile service wired (e.g., unit tests of unrelated
// flows, or `gh auth status` bootstrap).
type noopTokenProvider struct{}

func (noopTokenProvider) TokenForActive(context.Context) (string, error) { return "", nil }

type ghClient struct {
	exec   Executor
	tokens TokenProvider

	// whoMu guards the cache of `gh api user --jq .login` results keyed by
	// token. A per-token cache is required because REV-15 lets the user
	// switch active profiles — each profile's token resolves to a different
	// login and the enrichment needs "which review is mine" at poll time.
	whoMu sync.Mutex
	who   map[string]string

	// cache stores GetPRFullDetails and GetPRDiff results keyed by
	// "namespace:url" so repeat views in the details screen don't re-hit
	// `gh`. Invalidated after a successful MergePR.
	cache *memCache
}

// NewClient wires a Client around the given Executor. The optional tokens
// argument enables per-call GH_TOKEN injection (keyring profiles). Pass nil
// to rely entirely on the ambient gh CLI session.
func NewClient(e Executor, tokens ...TokenProvider) Client {
	var tp TokenProvider = noopTokenProvider{}
	if len(tokens) > 0 && tokens[0] != nil {
		tp = tokens[0]
	}
	return &ghClient{
		exec:   e,
		tokens: tp,
		who:    make(map[string]string),
		cache:  newMemCache(detailsCacheTTL),
	}
}

func (c *ghClient) AuthStatus(ctx context.Context) error {
	// AuthStatus is only meaningful for the gh-cli session; never inject a
	// token here — that would mask a broken ambient login.
	_, stderr, err := c.exec.Run(ctx, "gh", "auth", "status")
	if err != nil {
		return classify(stderr, err)
	}
	return nil
}

// searchPRsJSONFields lista os campos passados pra `gh search prs --json`.
// MUST ser subset de [searchPRsAllowedFields]; o teste TestSearchPRsJSONFields_Subset
// trava regressões silenciosas (campo inexistente derruba o poll inteiro
// porque gh aborta com "Unknown JSON field").
var searchPRsJSONFields = "number,title,url,repository,author,isDraft,updatedAt"

// searchPRsAllowedFields é a lista canônica de campos que `gh search prs --json`
// aceita (extraída via `gh search prs --json invalid`). Hardcoded aqui pra que
// CI flagre quando alguém adiciona um campo novo a [searchPRsJSONFields] sem
// confirmar suporte. Versão do gh referenciada: 2.x (REV-54).
//
// Notavelmente AUSENTES (e que poderiam ser desejáveis):
//   - headRefName / baseRefName — disponíveis só em `gh pr view`.
//   - author.avatarUrl — author retorna {id, is_bot, login, type, url} apenas.
var searchPRsAllowedFields = []string{
	"assignees", "author", "authorAssociation", "body", "closedAt",
	"commentsCount", "createdAt", "id", "isDraft", "isLocked",
	"isPullRequest", "labels", "number", "repository", "state",
	"title", "updatedAt", "url",
}

func (c *ghClient) ListReviewRequested(ctx context.Context) ([]PRSummary, error) {
	args := []string{
		"search", "prs",
		"--review-requested=@me",
		"--state=open",
		"--json", searchPRsJSONFields,
		"--limit", "100",
	}
	stdout, stderr, err := c.runGH(ctx, args...)
	if err != nil {
		return nil, classify(stderr, err)
	}
	var raw []rawSearchPR
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, fmt.Errorf("decode search prs: %w", err)
	}
	out := make([]PRSummary, 0, len(raw))
	for _, r := range raw {
		out = append(out, PRSummary{
			ID:        fmt.Sprintf("%s#%d", r.Repository.NameWithOwner, r.Number),
			Number:    r.Number,
			Repo:      r.Repository.NameWithOwner,
			Title:     r.Title,
			URL:       r.URL,
			Author:    r.Author.Login,
			IsDraft:   r.IsDraft,
			UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

func (c *ghClient) GetPRDetails(ctx context.Context, url string) (*PRDetails, error) {
	stdout, stderr, err := c.runGH(ctx,
		"pr", "view", url,
		"--json", "additions,deletions,state,mergedAt,isDraft,headRefName,author,latestReviews",
	)
	if err != nil {
		return nil, classify(stderr, err)
	}
	var raw rawPRView
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, fmt.Errorf("decode pr view: %w", err)
	}
	login, _ := c.whoAmI(ctx) // best-effort; failure just collapses to PENDING
	return &PRDetails{
		Additions:   raw.Additions,
		Deletions:   raw.Deletions,
		State:       raw.State,
		MergedAt:    raw.MergedAt,
		IsDraft:     raw.IsDraft,
		ReviewState: reviewStateFor(login, raw.LatestReviews),
		Branch:      raw.HeadRefName,
		AvatarURL:   raw.Author.AvatarURL,
	}, nil
}

// fullDetailsJSONFields enumerates the columns fetched by GetPRFullDetails.
// Kept as a package var so the test suite can assert on the exact gh args.
var fullDetailsJSONFields = "number,title,body,state,isDraft,author,url,additions,deletions,changedFiles,labels,reviews,statusCheckRollup,files,createdAt,updatedAt,mergedAt,mergeable,baseRefName,headRefName"

// GetPRFullDetails fetches the full metadata set the in-app details view
// needs. Cached for detailsCacheTTL so rapid back-and-forth navigation in
// the UI hits `gh` at most once per PR per TTL window.
func (c *ghClient) GetPRFullDetails(ctx context.Context, url string) (*PRFullDetails, error) {
	key := "details:" + url
	if v, ok := c.cache.get(key); ok {
		if cached, ok := v.(*PRFullDetails); ok {
			return cached, nil
		}
	}
	stdout, stderr, err := c.runGH(ctx,
		"pr", "view", url,
		"--json", fullDetailsJSONFields,
	)
	if err != nil {
		return nil, classify(stderr, err)
	}
	var raw rawPRFullView
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return nil, fmt.Errorf("decode pr view full: %w", err)
	}
	out := &PRFullDetails{
		Number:       raw.Number,
		Title:        raw.Title,
		Body:         raw.Body,
		URL:          raw.URL,
		State:        raw.State,
		IsDraft:      raw.IsDraft,
		Author:       raw.Author.Login,
		Additions:    raw.Additions,
		Deletions:    raw.Deletions,
		ChangedFiles: raw.ChangedFiles,
		Labels:       flattenLabels(raw.Labels),
		Reviews:      flattenReviews(raw.Reviews),
		StatusChecks: flattenStatusChecks(raw.StatusCheck),
		Files:        raw.Files,
		Mergeable:    raw.Mergeable,
		BaseRefName:  raw.BaseRefName,
		HeadRefName:  raw.HeadRefName,
		CreatedAt:    raw.CreatedAt,
		UpdatedAt:    raw.UpdatedAt,
		MergedAt:     raw.MergedAt,
	}
	c.cache.set(key, out)
	return out, nil
}

// GetPRDiff returns the raw unified diff as produced by `gh pr diff`. The
// result is cached for detailsCacheTTL — diff bodies can be sizeable and
// shouldn't be re-fetched on every scroll-into-view.
func (c *ghClient) GetPRDiff(ctx context.Context, url string) (string, error) {
	key := "diff:" + url
	if v, ok := c.cache.get(key); ok {
		if cached, ok := v.(string); ok {
			return cached, nil
		}
	}
	stdout, stderr, err := c.runGH(ctx, "pr", "diff", url)
	if err != nil {
		return "", classify(stderr, err)
	}
	s := string(stdout)
	c.cache.set(key, s)
	return s, nil
}

// MergePR invokes `gh pr merge <url> --squash|--merge` and invalidates the
// cache entries for this PR so the next GetPRFullDetails / GetPRDiff sees
// fresh state. `--delete-branch=false` is pinned so we never touch the
// user's branch-cleanup preference implicitly.
func (c *ghClient) MergePR(ctx context.Context, url string, method MergeMethod) error {
	var methodFlag string
	switch method {
	case MergeMethodSquash:
		methodFlag = "--squash"
	case MergeMethodMerge:
		methodFlag = "--merge"
	default:
		return fmt.Errorf("unsupported merge method: %q", method)
	}
	_, stderr, err := c.runGH(ctx, "pr", "merge", url, methodFlag, "--delete-branch=false")
	if err != nil {
		return classify(stderr, err)
	}
	c.cache.invalidate(url)
	return nil
}

// ApprovePR invoca `gh pr review <url> --approve` e invalida o cache do PR pra
// próxima leitura refletir o novo review state. Existe pra suportar fluxos de
// branch ruleset que exigem review aprovado antes do merge (REV-62). Self-
// approve cai em ErrApproveSelf via classify().
func (c *ghClient) ApprovePR(ctx context.Context, url string) error {
	_, stderr, err := c.runGH(ctx, "pr", "review", url, "--approve")
	if err != nil {
		return classify(stderr, err)
	}
	c.cache.invalidate(url)
	return nil
}

func flattenLabels(in []rawLabel) []Label {
	out := make([]Label, 0, len(in))
	for _, l := range in {
		out = append(out, Label(l))
	}
	return out
}

func flattenReviews(in []rawReview) []Review {
	out := make([]Review, 0, len(in))
	for _, r := range in {
		out = append(out, Review{
			Author:      r.Author.Login,
			State:       r.State,
			SubmittedAt: r.SubmittedAt,
		})
	}
	return out
}

// flattenStatusChecks normalizes the heterogeneous entries of
// statusCheckRollup (CheckRun | StatusContext) into a single shape the UI
// can render without case analysis.
func flattenStatusChecks(in []rawStatusCheck) []StatusCheck {
	out := make([]StatusCheck, 0, len(in))
	for _, r := range in {
		name := r.Name
		if name == "" {
			name = r.Context
		}
		status := r.Status
		if status == "" && r.State != "" {
			// StatusContext has no Status/Conclusion split — its State IS
			// the outcome. Surface it as "COMPLETED" so the UI treats it
			// like a finished check.
			status = "COMPLETED"
		}
		conclusion := r.Conclusion
		if conclusion == "" {
			conclusion = r.State
		}
		url := r.DetailsURL
		if url == "" {
			url = r.TargetURL
		}
		out = append(out, StatusCheck{
			Name:       name,
			Status:     status,
			Conclusion: conclusion,
			URL:        url,
		})
	}
	return out
}

// whoAmI returns the login associated with the currently active profile's
// token, caching the result per-token. A failure is returned as ("", err)
// and callers fall back to treating the review as PENDING — missing this
// field never breaks polling.
func (c *ghClient) whoAmI(ctx context.Context) (string, error) {
	token, err := c.tokens.TokenForActive(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve token for whoami: %w", err)
	}
	key := token
	if key == "" {
		key = "__ambient__"
	}
	c.whoMu.Lock()
	if cached, ok := c.who[key]; ok {
		c.whoMu.Unlock()
		return cached, nil
	}
	c.whoMu.Unlock()

	stdout, stderr, err := c.runGH(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", classify(stderr, err)
	}
	login := strings.TrimSpace(string(stdout))

	c.whoMu.Lock()
	c.who[key] = login
	c.whoMu.Unlock()
	return login, nil
}

// runGH resolves the active token, builds the env override, and invokes gh.
// When the token is empty (gh-cli profile) the ambient env is used untouched.
// The token variable goes out of scope immediately after the child process
// exits — no struct fields, no logs.
func (c *ghClient) runGH(ctx context.Context, args ...string) ([]byte, []byte, error) {
	token, err := c.tokens.TokenForActive(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve token: %w", err)
	}
	if token == "" {
		return c.exec.Run(ctx, "gh", args...)
	}
	env := append(os.Environ(), "GH_TOKEN="+token)
	if envRunner, ok := c.exec.(EnvExecutor); ok {
		return envRunner.RunEnv(ctx, env, "gh", args...)
	}
	// Executor does not support env injection. Caller passed a non-env-aware
	// impl despite wiring a TokenProvider — surface a clear error instead of
	// silently falling back to ambient.
	return nil, nil, errors.New("executor does not support GH_TOKEN injection")
}

// classify maps the stderr of a failed `gh` invocation to a sentinel error.
// Matching is substring-based because gh does not expose structured error
// codes — fragile to version changes, acceptable for the MVP.
func classify(stderr []byte, runErr error) error {
	s := string(stderr)
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "not logged"),
		strings.Contains(lower, "authentication required"),
		strings.Contains(lower, "authentication failed"):
		return ErrAuthExpired
	case strings.Contains(lower, "rate limit"),
		strings.Contains(lower, "api rate limit exceeded"):
		return ErrRateLimited
	case strings.Contains(lower, "could not resolve"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "timeout"):
		return ErrTransient
	case strings.Contains(lower, "merge conflict"),
		strings.Contains(lower, "conflicts with"),
		strings.Contains(lower, "not mergeable"):
		// "not mergeable" can come from either a real conflict or an unmet
		// protected-branch rule; the UI treats both as "not mergeable" so
		// routing both to ErrMergeConflict is fine for MVP feedback.
		return ErrMergeConflict
	case strings.Contains(lower, "must have admin rights"),
		strings.Contains(lower, "does not have permission"),
		strings.Contains(lower, "requires write access"),
		strings.Contains(lower, "resource not accessible"):
		return ErrMergePermission
	case strings.Contains(lower, "pull request is in draft"),
		strings.Contains(lower, "pull request is closed"):
		return ErrNotMergeable
	case strings.Contains(lower, "can not approve your own pull request"),
		strings.Contains(lower, "cannot approve your own pull request"),
		strings.Contains(lower, "can not approve your own"):
		return ErrApproveSelf
	}
	if runErr == nil {
		runErr = errors.New("gh failed")
	}
	return fmt.Errorf("gh: %s: %w", firstLine(s), runErr)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	if s == "" {
		return "no stderr"
	}
	return s
}
