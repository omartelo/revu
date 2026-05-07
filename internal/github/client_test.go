package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeCall struct {
	name string
	args []string
	env  []string
}

type fakeExec struct {
	stdout []byte
	stderr []byte
	err    error

	// loginStdout overrides stdout for `gh api user --jq .login` invocations;
	// lets tests exercise GetPRDetails (which internally calls whoAmI) without
	// polluting the primary stdout fixture.
	loginStdout []byte

	// diffStdout, if non-nil, is returned for `gh pr diff` invocations so
	// tests can mix a view fixture on stdout with a diff fixture on the
	// same fake without routing by global state.
	diffStdout []byte

	// mergeStderr/mergeErr, when set, override stderr/err for `gh pr merge`
	// invocations — lets a single fake drive both happy-path view fetches
	// and a merge failure response.
	mergeStderr []byte
	mergeErr    error

	// reviewStderr/reviewErr, when set, override stderr/err for
	// `gh pr review` invocations — same pattern as mergeStderr/mergeErr,
	// scoped ao fluxo de approve (REV-62).
	reviewStderr []byte
	reviewErr    error

	calls []fakeCall

	// Back-compat mirrors of the LAST call — existing tests that issue a
	// single call continue to read gotName/gotArgs/gotEnv unchanged.
	gotName string
	gotArgs []string
	gotEnv  []string
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	c := fakeCall{name: name, args: append([]string(nil), args...)}
	f.calls = append(f.calls, c)
	f.gotName = name
	f.gotArgs = c.args
	return f.responseFor(args)
}

func (f *fakeExec) RunEnv(_ context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	c := fakeCall{name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)}
	f.calls = append(f.calls, c)
	f.gotName = name
	f.gotArgs = c.args
	f.gotEnv = c.env
	return f.responseFor(args)
}

func (f *fakeExec) responseFor(args []string) ([]byte, []byte, error) {
	if f.loginStdout != nil && len(args) >= 2 && args[0] == "api" && args[1] == "user" {
		return f.loginStdout, nil, nil
	}
	if f.diffStdout != nil && len(args) >= 2 && args[0] == "pr" && args[1] == "diff" {
		return f.diffStdout, nil, nil
	}
	if len(args) >= 2 && args[0] == "pr" && args[1] == "merge" {
		return nil, f.mergeStderr, f.mergeErr
	}
	if len(args) >= 2 && args[0] == "pr" && args[1] == "review" {
		return nil, f.reviewStderr, f.reviewErr
	}
	return f.stdout, f.stderr, f.err
}

type staticTokens struct {
	token string
	err   error
}

func (s staticTokens) TokenForActive(context.Context) (string, error) {
	return s.token, s.err
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestAuthStatus(t *testing.T) {
	tests := []struct {
		name    string
		stderr  []byte
		runErr  error
		wantErr error
	}{
		{"ok", nil, nil, nil},
		{"auth expired", []byte("You are not logged into any GitHub hosts"), errors.New("exit 1"), ErrAuthExpired},
		{"rate limit", []byte("API rate limit exceeded for user"), errors.New("exit 1"), ErrRateLimited},
		{"transient net", []byte("Could not resolve host: api.github.com"), errors.New("exit 1"), ErrTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := &fakeExec{stderr: tt.stderr, err: tt.runErr}
			c := NewClient(fe)
			err := c.AuthStatus(context.Background())
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("want nil err, got %v", err)
				}
			} else {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("want errors.Is(%v), got %v", tt.wantErr, err)
				}
			}
			if fe.gotName != "gh" {
				t.Fatalf("want cmd gh, got %s", fe.gotName)
			}
			wantArgs := []string{"auth", "status"}
			if !reflect.DeepEqual(fe.gotArgs, wantArgs) {
				t.Fatalf("want args %v, got %v", wantArgs, fe.gotArgs)
			}
		})
	}
}

func TestListReviewRequested_Happy(t *testing.T) {
	fe := &fakeExec{stdout: loadFixture(t, "search_prs.json")}
	c := NewClient(fe)
	got, err := c.ListReviewRequested(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 prs, got %d", len(got))
	}
	want0 := PRSummary{
		ID:        "octocat/hello-world#142",
		Number:    142,
		Repo:      "octocat/hello-world",
		Title:     "feat: add foo",
		URL:       "https://github.com/octocat/hello-world/pull/142",
		Author:    "alice",
		IsDraft:   false,
		UpdatedAt: time.Date(2026, 4, 23, 14, 30, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(got[0], want0) {
		t.Fatalf("pr[0] mismatch:\nwant %+v\ngot  %+v", want0, got[0])
	}
	if got[1].ID != "acme/widgets#7" || !got[1].IsDraft {
		t.Fatalf("pr[1] mismatch: %+v", got[1])
	}
	wantArgs := []string{
		"search", "prs",
		"--review-requested=@me",
		"--state=open",
		"--json", "number,title,url,repository,author,isDraft,updatedAt",
		"--limit", "100",
	}
	if !reflect.DeepEqual(fe.gotArgs, wantArgs) {
		t.Fatalf("args mismatch:\nwant %v\ngot  %v", wantArgs, fe.gotArgs)
	}
}

func TestListReviewRequested_Errors(t *testing.T) {
	tests := []struct {
		name    string
		stdout  []byte
		stderr  []byte
		runErr  error
		wantErr error
	}{
		{"auth expired", nil, []byte("gh: authentication required"), errors.New("exit 1"), ErrAuthExpired},
		{"rate limit", nil, []byte("API rate limit exceeded"), errors.New("exit 1"), ErrRateLimited},
		{"transient", nil, []byte("Could not resolve host"), errors.New("exit 1"), ErrTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := &fakeExec{stdout: tt.stdout, stderr: tt.stderr, err: tt.runErr}
			c := NewClient(fe)
			_, err := c.ListReviewRequested(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("want errors.Is(%v), got %v", tt.wantErr, err)
			}
		})
	}
}

func TestListReviewRequested_MalformedJSON(t *testing.T) {
	fe := &fakeExec{stdout: []byte(`{"not an array`)}
	c := NewClient(fe)
	_, err := c.ListReviewRequested(context.Background())
	if err == nil {
		t.Fatal("want decode error")
	}
	if errors.Is(err, ErrAuthExpired) || errors.Is(err, ErrRateLimited) || errors.Is(err, ErrTransient) {
		t.Fatalf("should not be sentinel, got %v", err)
	}
}

func TestGetPRDetails_Happy(t *testing.T) {
	fe := &fakeExec{
		stdout:      loadFixture(t, "pr_view.json"),
		loginStdout: []byte("alice\n"),
	}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	got, err := c.GetPRDetails(context.Background(), url)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := &PRDetails{
		Additions:   142,
		Deletions:   37,
		State:       "OPEN",
		MergedAt:    nil,
		IsDraft:     false,
		ReviewState: "APPROVED",
		Branch:      "feat/foo",
		AvatarURL:   "https://avatars.githubusercontent.com/u/9?v=4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %+v, got %+v", want, got)
	}
	wantPRView := []string{"pr", "view", url, "--json", "additions,deletions,state,mergedAt,isDraft,headRefName,author,latestReviews"}
	if len(fe.calls) < 1 || !reflect.DeepEqual(fe.calls[0].args, wantPRView) {
		t.Fatalf("pr view call mismatch:\nwant %v\ngot calls %+v", wantPRView, fe.calls)
	}
	// Second call is the whoami lookup feeding ReviewState.
	if len(fe.calls) < 2 || fe.calls[1].args[0] != "api" || fe.calls[1].args[1] != "user" {
		t.Fatalf("expected whoami call, got %+v", fe.calls)
	}
}

func TestGetPRDetails_ReviewStateCachesLogin(t *testing.T) {
	fe := &fakeExec{
		stdout:      loadFixture(t, "pr_view.json"),
		loginStdout: []byte("alice\n"),
	}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	if _, err := c.GetPRDetails(context.Background(), url); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := c.GetPRDetails(context.Background(), url); err != nil {
		t.Fatalf("second: %v", err)
	}
	// 2 pr-view calls + 1 whoami call. Second enrich must read login from cache.
	whoami := 0
	for _, c := range fe.calls {
		if len(c.args) >= 2 && c.args[0] == "api" && c.args[1] == "user" {
			whoami++
		}
	}
	if whoami != 1 {
		t.Fatalf("whoami must be cached across calls, got %d invocations", whoami)
	}
}

func TestGetPRDetails_Errors(t *testing.T) {
	tests := []struct {
		name    string
		stderr  []byte
		wantErr error
	}{
		{"auth expired", []byte("You are not logged in"), ErrAuthExpired},
		{"rate limit", []byte("API rate limit exceeded"), ErrRateLimited},
		{"transient", []byte("connection refused"), ErrTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := &fakeExec{stderr: tt.stderr, err: errors.New("exit 1")}
			c := NewClient(fe)
			_, err := c.GetPRDetails(context.Background(), "x")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("want errors.Is(%v), got %v", tt.wantErr, err)
			}
		})
	}
}

func TestGetPRDetails_MalformedJSON(t *testing.T) {
	fe := &fakeExec{stdout: []byte(`not json`)}
	c := NewClient(fe)
	_, err := c.GetPRDetails(context.Background(), "x")
	if err == nil {
		t.Fatal("want decode error")
	}
}

func TestClassify_UnknownStderrWrapsRunErr(t *testing.T) {
	stderr := []byte("something weird happened\nline 2")
	runErr := errors.New("exit 2")
	err := classify(stderr, runErr)
	if !errors.Is(err, runErr) {
		t.Fatalf("want wrap runErr, got %v", err)
	}
	// Sanity: must include first line of stderr for debuggability.
	if got := err.Error(); !contains(got, "something weird happened") {
		t.Fatalf("want first stderr line in msg, got %q", got)
	}
}

func TestRunGH_InjectsGHToken(t *testing.T) {
	fe := &fakeExec{stdout: []byte(`[]`)}
	c := NewClient(fe, staticTokens{token: "ghp_test_token"})

	_, err := c.ListReviewRequested(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !containsEnv(fe.gotEnv, "GH_TOKEN=ghp_test_token") {
		t.Fatalf("GH_TOKEN missing from env: %v", fe.gotEnv)
	}
	// Also ensure ambient vars were preserved (at least PATH usually exists).
	if len(fe.gotEnv) < 2 {
		t.Errorf("env appears truncated: %v", fe.gotEnv)
	}
}

func TestRunGH_NoTokenUsesAmbientEnv(t *testing.T) {
	fe := &fakeExec{stdout: []byte(`[]`)}
	c := NewClient(fe, staticTokens{token: ""})

	_, err := c.ListReviewRequested(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if fe.gotEnv != nil {
		t.Fatalf("expected RunEnv to be bypassed when token empty, got env %v", fe.gotEnv)
	}
}

func TestAuthStatus_DoesNotInjectToken(t *testing.T) {
	fe := &fakeExec{}
	c := NewClient(fe, staticTokens{token: "ghp_should_not_be_passed"})
	if err := c.AuthStatus(context.Background()); err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	if fe.gotEnv != nil {
		t.Errorf("auth status must use ambient env, got %v", fe.gotEnv)
	}
}

func containsEnv(env []string, want string) bool {
	return slices.Contains(env, want)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestGetPRFullDetails_Happy(t *testing.T) {
	fe := &fakeExec{stdout: loadFixture(t, "pr_full_view.json")}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	got, err := c.GetPRFullDetails(context.Background(), url)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Number != 142 || got.Title != "feat: add foo" {
		t.Fatalf("basic fields mismatch: %+v", got)
	}
	if got.Author != "alice" {
		t.Fatalf("author flatten failed: %q", got.Author)
	}
	if got.Additions != 142 || got.Deletions != 37 || got.ChangedFiles != 3 {
		t.Fatalf("counters mismatch: %+v", got)
	}
	if got.Mergeable != "MERGEABLE" {
		t.Fatalf("mergeable: %q", got.Mergeable)
	}
	if got.BaseRefName != "main" || got.HeadRefName != "feature/foo" {
		t.Fatalf("refs mismatch: %s ← %s", got.BaseRefName, got.HeadRefName)
	}
	if len(got.Labels) != 2 || got.Labels[0].Name != "feature" {
		t.Fatalf("labels mismatch: %+v", got.Labels)
	}
	if len(got.Reviews) != 2 || got.Reviews[0].Author != "bob" || got.Reviews[1].State != "APPROVED" {
		t.Fatalf("reviews mismatch: %+v", got.Reviews)
	}
	if len(got.Files) != 3 || got.Files[0].Path != "cmd/foo/main.go" {
		t.Fatalf("files mismatch: %+v", got.Files)
	}
	// StatusChecks: CheckRun build/SUCCESS, CheckRun test/in-progress with no
	// conclusion, StatusContext ci/circle mapped via State.
	if len(got.StatusChecks) != 3 {
		t.Fatalf("status checks count: %d", len(got.StatusChecks))
	}
	if got.StatusChecks[0].Name != "build" || got.StatusChecks[0].Conclusion != "SUCCESS" {
		t.Fatalf("check[0]: %+v", got.StatusChecks[0])
	}
	if got.StatusChecks[1].Status != "IN_PROGRESS" {
		t.Fatalf("check[1] status: %+v", got.StatusChecks[1])
	}
	if got.StatusChecks[2].Name != "ci/circle" || got.StatusChecks[2].Conclusion != "SUCCESS" {
		t.Fatalf("check[2] (StatusContext) flatten failed: %+v", got.StatusChecks[2])
	}
	wantArgs := []string{"pr", "view", url, "--json", fullDetailsJSONFields}
	if !reflect.DeepEqual(fe.gotArgs, wantArgs) {
		t.Fatalf("args mismatch:\nwant %v\ngot  %v", wantArgs, fe.gotArgs)
	}
}

func TestGetPRFullDetails_Cache(t *testing.T) {
	fe := &fakeExec{stdout: loadFixture(t, "pr_full_view.json")}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	if _, err := c.GetPRFullDetails(context.Background(), url); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := c.GetPRFullDetails(context.Background(), url); err != nil {
		t.Fatalf("second: %v", err)
	}
	views := 0
	for _, call := range fe.calls {
		if len(call.args) >= 2 && call.args[0] == "pr" && call.args[1] == "view" {
			views++
		}
	}
	if views != 1 {
		t.Fatalf("want 1 pr view call (cache), got %d", views)
	}
}

func TestGetPRFullDetails_Errors(t *testing.T) {
	tests := []struct {
		name    string
		stderr  []byte
		wantErr error
	}{
		{"auth expired", []byte("You are not logged in"), ErrAuthExpired},
		{"rate limit", []byte("API rate limit exceeded"), ErrRateLimited},
		{"transient", []byte("connection refused"), ErrTransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := &fakeExec{stderr: tt.stderr, err: errors.New("exit 1")}
			c := NewClient(fe)
			_, err := c.GetPRFullDetails(context.Background(), "x")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("want errors.Is(%v), got %v", tt.wantErr, err)
			}
		})
	}
}

func TestGetPRDiff_Happy(t *testing.T) {
	fe := &fakeExec{diffStdout: loadFixture(t, "pr_diff_small.txt")}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	got, err := c.GetPRDiff(context.Background(), url)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !contains(got, "diff --git a/cmd/foo/main.go") {
		t.Fatalf("diff content not returned verbatim: %q", got[:min(80, len(got))])
	}
	wantArgs := []string{"pr", "diff", url}
	if !reflect.DeepEqual(fe.gotArgs, wantArgs) {
		t.Fatalf("args mismatch:\nwant %v\ngot  %v", wantArgs, fe.gotArgs)
	}
}

func TestGetPRDiff_Cache(t *testing.T) {
	fe := &fakeExec{diffStdout: loadFixture(t, "pr_diff_small.txt")}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	if _, err := c.GetPRDiff(context.Background(), url); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := c.GetPRDiff(context.Background(), url); err != nil {
		t.Fatalf("second: %v", err)
	}
	diffs := 0
	for _, call := range fe.calls {
		if len(call.args) >= 2 && call.args[0] == "pr" && call.args[1] == "diff" {
			diffs++
		}
	}
	if diffs != 1 {
		t.Fatalf("want 1 pr diff call (cache), got %d", diffs)
	}
}

func TestMergePR_Squash(t *testing.T) {
	fe := &fakeExec{}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	if err := c.MergePR(context.Background(), url, MergeMethodSquash); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	wantArgs := []string{"pr", "merge", url, "--squash", "--delete-branch=false"}
	if !reflect.DeepEqual(fe.gotArgs, wantArgs) {
		t.Fatalf("args mismatch:\nwant %v\ngot  %v", wantArgs, fe.gotArgs)
	}
}

func TestMergePR_Merge(t *testing.T) {
	fe := &fakeExec{}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	if err := c.MergePR(context.Background(), url, MergeMethodMerge); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	wantArgs := []string{"pr", "merge", url, "--merge", "--delete-branch=false"}
	if !reflect.DeepEqual(fe.gotArgs, wantArgs) {
		t.Fatalf("args mismatch:\nwant %v\ngot  %v", wantArgs, fe.gotArgs)
	}
}

func TestMergePR_InvalidMethod(t *testing.T) {
	fe := &fakeExec{}
	c := NewClient(fe)
	err := c.MergePR(context.Background(), "x", MergeMethod("rebase"))
	if err == nil {
		t.Fatal("want error for unsupported method")
	}
	if len(fe.calls) != 0 {
		t.Fatalf("must not invoke gh for invalid method, got %+v", fe.calls)
	}
}

func TestMergePR_ErrorClassification(t *testing.T) {
	tests := []struct {
		name    string
		stderr  []byte
		wantErr error
	}{
		{
			"conflict",
			[]byte("Pull request is not mergeable: the merge commit cannot be cleanly created"),
			ErrMergeConflict,
		},
		{"permission", []byte("Resource not accessible by personal access token"), ErrMergePermission},
		{"draft", []byte("Pull request is in draft state"), ErrNotMergeable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := &fakeExec{mergeStderr: tt.stderr, mergeErr: errors.New("exit 1")}
			c := NewClient(fe)
			err := c.MergePR(context.Background(), "x", MergeMethodSquash)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("want errors.Is(%v), got %v", tt.wantErr, err)
			}
		})
	}
}

func TestMergePR_InvalidatesCache(t *testing.T) {
	fe := &fakeExec{stdout: loadFixture(t, "pr_full_view.json")}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	if _, err := c.GetPRFullDetails(context.Background(), url); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	if err := c.MergePR(context.Background(), url, MergeMethodSquash); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Post-merge fetch must hit gh again since the cache was invalidated.
	if _, err := c.GetPRFullDetails(context.Background(), url); err != nil {
		t.Fatalf("post-merge fetch: %v", err)
	}
	views := 0
	for _, call := range fe.calls {
		if len(call.args) >= 2 && call.args[0] == "pr" && call.args[1] == "view" {
			views++
		}
	}
	if views != 2 {
		t.Fatalf("cache invalidation failed: want 2 pr view calls, got %d", views)
	}
}

func TestApprovePR_Success(t *testing.T) {
	fe := &fakeExec{}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	if err := c.ApprovePR(context.Background(), url); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	wantArgs := []string{"pr", "review", url, "--approve"}
	if !reflect.DeepEqual(fe.gotArgs, wantArgs) {
		t.Fatalf("args mismatch:\nwant %v\ngot  %v", wantArgs, fe.gotArgs)
	}
}

func TestApprovePR_ErrorClassification(t *testing.T) {
	tests := []struct {
		name    string
		stderr  []byte
		wantErr error
	}{
		{
			"self approve",
			[]byte("GraphQL: Can not approve your own pull request (addPullRequestReview)"),
			ErrApproveSelf,
		},
		{
			"auth expired",
			[]byte("authentication required"),
			ErrAuthExpired,
		},
		{
			"permission denied",
			[]byte("Resource not accessible by personal access token"),
			ErrMergePermission,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fe := &fakeExec{reviewStderr: tt.stderr, reviewErr: errors.New("exit 1")}
			c := NewClient(fe)
			err := c.ApprovePR(context.Background(), "x")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("want errors.Is(%v), got %v", tt.wantErr, err)
			}
		})
	}
}

func TestApprovePR_InvalidatesCache(t *testing.T) {
	fe := &fakeExec{stdout: loadFixture(t, "pr_full_view.json")}
	c := NewClient(fe)
	url := "https://github.com/octocat/hello-world/pull/142"
	if _, err := c.GetPRFullDetails(context.Background(), url); err != nil {
		t.Fatalf("warmup: %v", err)
	}
	if err := c.ApprovePR(context.Background(), url); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := c.GetPRFullDetails(context.Background(), url); err != nil {
		t.Fatalf("post-approve fetch: %v", err)
	}
	views := 0
	for _, call := range fe.calls {
		if len(call.args) >= 2 && call.args[0] == "pr" && call.args[1] == "view" {
			views++
		}
	}
	if views != 2 {
		t.Fatalf("cache invalidation failed: want 2 pr view calls, got %d", views)
	}
}

// TestSearchPRsJSONFields_Subset trava regressão silenciosa: `gh search prs`
// aceita uma lista fixa de campos via `--json` (ver searchPRsAllowedFields).
// Se alguém adicionar um campo novo a searchPRsJSONFields que o gh não
// aceita, o poll inteiro quebra em runtime ("Unknown JSON field"). Este
// teste falha em compile time da suite (rápido) ao invés de runtime do
// daemon (que só dispara após instalação).
func TestSearchPRsJSONFields_Subset(t *testing.T) {
	allowed := make(map[string]struct{}, len(searchPRsAllowedFields))
	for _, f := range searchPRsAllowedFields {
		allowed[f] = struct{}{}
	}
	for field := range strings.SplitSeq(searchPRsJSONFields, ",") {
		field = strings.TrimSpace(field)
		if _, ok := allowed[field]; !ok {
			t.Errorf("--json field %q não está em searchPRsAllowedFields; "+
				"gh search prs vai abortar com Unknown JSON field. "+
				"Mover esse dado pra GetPRDetails (gh pr view) ou confirmar "+
				"que o gh suporta agora e adicionar à lista canônica.",
				field)
		}
	}
}
