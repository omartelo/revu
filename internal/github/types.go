package github

import (
	"errors"
	"time"
)

// PRSummary is the flattened shape returned by ListReviewRequested. The ID
// follows the store convention "owner/repo#number" (see SPEC §5.3).
//
// Branch e AvatarURL não fazem parte do search (limitação do gh) — vivem em
// PRDetails e chegam ao store via RefreshPRStatus. Permanecer fora deste
// struct deixa explícito que o caminho hot path não os carrega.
type PRSummary struct {
	ID        string    `json:"id"`
	Number    int       `json:"number"`
	Repo      string    `json:"repo"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Author    string    `json:"author"`
	IsDraft   bool      `json:"isDraft"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PRDetails is the enrichment returned by GetPRDetails (diff + current state
// + the review state for the active user). ReviewState follows the GitHub
// REST vocabulary narrowed to what the store persists: PENDING, APPROVED,
// CHANGES_REQUESTED, COMMENTED.
type PRDetails struct {
	Additions   int        `json:"additions"`
	Deletions   int        `json:"deletions"`
	State       string     `json:"state"`
	MergedAt    *time.Time `json:"mergedAt"`
	IsDraft     bool       `json:"isDraft"`
	ReviewState string     `json:"reviewState"`
	// Branch e AvatarURL chegam aqui (não no search) porque `gh search prs`
	// não suporta `headRefName` e o `author` nele retornado não inclui
	// `avatarUrl`. Populados via `gh pr view` no enrich do poller.
	Branch    string `json:"branch"`
	AvatarURL string `json:"avatarUrl"`
}

// PRFullDetails is the payload for the in-app PR details view — metadata,
// reviewers, labels, status checks, and the list of changed files. The diff
// itself is fetched separately via GetPRDiff so the UI can render metadata
// while the (potentially larger) diff loads, and so the 500-line threshold
// can skip fetching the diff entirely for big PRs.
type PRFullDetails struct {
	Number       int           `json:"number"`
	Title        string        `json:"title"`
	Body         string        `json:"body"`
	URL          string        `json:"url"`
	State        string        `json:"state"`
	IsDraft      bool          `json:"isDraft"`
	Author       string        `json:"author"`
	Additions    int           `json:"additions"`
	Deletions    int           `json:"deletions"`
	ChangedFiles int           `json:"changedFiles"`
	Labels       []Label       `json:"labels"`
	Reviews      []Review      `json:"reviews"`
	StatusChecks []StatusCheck `json:"statusChecks"`
	Files        []ChangedFile `json:"files"`
	Mergeable    string        `json:"mergeable"`
	BaseRefName  string        `json:"baseRefName"`
	HeadRefName  string        `json:"headRefName"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
	MergedAt     *time.Time    `json:"mergedAt"`
}

// Label is a GitHub PR label — name + 6-char hex color (no leading #).
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Review is a single reviewer decision on the PR.
type Review struct {
	Author      string    `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// StatusCheck unifies CheckRun and StatusContext entries from
// statusCheckRollup. For CheckRun, Conclusion holds the outcome; for
// StatusContext the raw State (SUCCESS/FAILURE/PENDING/ERROR) is reused in
// Conclusion so the UI can render a single badge regardless of source.
type StatusCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

// ChangedFile is an entry in gh pr view --json files.
type ChangedFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// MergeMethod is the strategy passed to `gh pr merge`. Only squash and merge
// are supported in the REV-13 MVP — rebase is intentionally left out because
// it rewrites commits and is more likely to trip branch protection.
type MergeMethod string

const (
	MergeMethodSquash MergeMethod = "squash"
	MergeMethodMerge  MergeMethod = "merge"
)

// rawLatestReview mirrors an entry of `gh pr view --json latestReviews`.
// Only the minimum fields needed to attribute the review and read its state
// are declared; anything else in the payload is ignored by encoding/json.
type rawLatestReview struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State string `json:"state"`
}

// rawPRView is the shape of the JSON payload returned by GetPRDetails.
// REV-54 adiciona headRefName + author.avatarUrl pra alimentar o card
// — campos que `gh search prs` não expõe.
type rawPRView struct {
	Additions     int               `json:"additions"`
	Deletions     int               `json:"deletions"`
	State         string            `json:"state"`
	MergedAt      *time.Time        `json:"mergedAt"`
	IsDraft       bool              `json:"isDraft"`
	HeadRefName   string            `json:"headRefName"`
	Author        rawLogin          `json:"author"`
	LatestReviews []rawLatestReview `json:"latestReviews"`
}

// rawPRFullView is the superset payload of `gh pr view --json …` used by
// GetPRFullDetails. Nested shapes are flattened into the public types above.
type rawPRFullView struct {
	Number       int              `json:"number"`
	Title        string           `json:"title"`
	Body         string           `json:"body"`
	URL          string           `json:"url"`
	State        string           `json:"state"`
	IsDraft      bool             `json:"isDraft"`
	Author       rawLogin         `json:"author"`
	Additions    int              `json:"additions"`
	Deletions    int              `json:"deletions"`
	ChangedFiles int              `json:"changedFiles"`
	Labels       []rawLabel       `json:"labels"`
	Reviews      []rawReview      `json:"reviews"`
	StatusCheck  []rawStatusCheck `json:"statusCheckRollup"`
	Files        []ChangedFile    `json:"files"`
	Mergeable    string           `json:"mergeable"`
	BaseRefName  string           `json:"baseRefName"`
	HeadRefName  string           `json:"headRefName"`
	CreatedAt    time.Time        `json:"createdAt"`
	UpdatedAt    time.Time        `json:"updatedAt"`
	MergedAt     *time.Time       `json:"mergedAt"`
}

type rawLogin struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatarUrl"`
}

type rawLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type rawReview struct {
	Author      rawLogin  `json:"author"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// rawStatusCheck absorbs both CheckRun and StatusContext entries from
// statusCheckRollup. The gh CLI emits a heterogeneous array; to avoid an
// explicit __typename switch we read every field we might need and flatten
// in flattenStatusCheck() below.
type rawStatusCheck struct {
	Typename   string `json:"__typename"`
	Name       string `json:"name"`
	Context    string `json:"context"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
	DetailsURL string `json:"detailsUrl"`
	TargetURL  string `json:"targetUrl"`
}

// Sentinel errors let callers branch on failure mode (poller backoff, tray
// error state, merge confirmation). They are returned via [errors.Is] after
// classify().
var (
	ErrAuthExpired     = errors.New("gh auth expired")
	ErrRateLimited     = errors.New("github rate limited")
	ErrTransient       = errors.New("gh transient failure")
	ErrNotMergeable    = errors.New("pr not mergeable")
	ErrMergePermission = errors.New("no write permission to merge")
	ErrMergeConflict   = errors.New("merge conflict")
	ErrApproveSelf     = errors.New("cannot approve own pull request")
)

// rawSearchPR mirrors the nested JSON from `gh search prs` so we can decode
// the wire shape before flattening into PRSummary. `gh search prs --json`
// não expõe `headRefName` nem `avatarUrl` (ver `gh search prs --json invalid`
// pra lista canônica). Esses campos chegam via `gh pr view` no enrich.
type rawSearchPR struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	IsDraft   bool      `json:"isDraft"`
	UpdatedAt time.Time `json:"updatedAt"`
}
