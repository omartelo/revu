package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/meopedevts/revu/internal/github"
)

type fakeClient struct {
	err error
}

func (f *fakeClient) AuthStatus(_ context.Context) error { return f.err }
func (f *fakeClient) ListReviewRequested(_ context.Context) ([]github.PRSummary, error) {
	return nil, nil
}
func (f *fakeClient) GetPRDetails(_ context.Context, _ string) (*github.PRDetails, error) {
	return nil, nil
}
func (f *fakeClient) GetPRFullDetails(_ context.Context, _ string) (*github.PRFullDetails, error) {
	return nil, nil
}
func (f *fakeClient) GetPRDiff(_ context.Context, _ string) (string, error) { return "", nil }
func (f *fakeClient) MergePR(_ context.Context, _ string, _ github.MergeMethod) error {
	return nil
}
func (f *fakeClient) ApprovePR(_ context.Context, _ string) error {
	return nil
}

func TestCheckGHInPath(t *testing.T) {
	ok := checkGHInPath(func(_ string) (string, error) { return "/usr/bin/gh", nil })
	if !ok.OK {
		t.Fatalf("want OK, got %+v", ok)
	}
	bad := checkGHInPath(func(_ string) (string, error) { return "", errors.New("not found") })
	if bad.OK || bad.Detail == "" {
		t.Fatalf("want failure with detail, got %+v", bad)
	}
}

func TestCheckGHAuth(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantOK   bool
		contains string
	}{
		{"ok", nil, true, ""},
		{"auth expired", github.ErrAuthExpired, false, "gh auth login"},
		{"rate limited", github.ErrRateLimited, false, "rate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := checkGHAuth(context.Background(), &fakeClient{err: tt.err})
			if r.OK != tt.wantOK {
				t.Fatalf("want OK=%v, got %+v", tt.wantOK, r)
			}
			if tt.contains != "" && !strings.Contains(r.Detail, tt.contains) {
				t.Fatalf("want detail contains %q, got %q", tt.contains, r.Detail)
			}
		})
	}
}

func TestCheckDBus(t *testing.T) {
	r := checkDBus(func(string) string { return "unix:path=/run/user/1000/bus" })
	if !r.OK {
		t.Fatalf("want OK, got %+v", r)
	}
	r = checkDBus(func(string) string { return "" })
	if r.OK {
		t.Fatalf("want failure, got %+v", r)
	}
}

func TestCheckAppIndicator(t *testing.T) {
	ctx := context.Background()

	// pkg-config present and returns success
	r := checkAppIndicator(ctx,
		func(s string) (string, error) {
			if s == "pkg-config" {
				return "/usr/bin/pkg-config", nil
			}
			return "", errors.New("nope")
		},
		func(_ context.Context, name string, _ ...string) error {
			if name == "pkg-config" {
				return nil
			}
			return errors.New("unexpected")
		},
	)
	if !r.OK {
		t.Fatalf("want OK via pkg-config, got %+v", r)
	}

	// pkg-config absent, ldconfig fallback succeeds
	r = checkAppIndicator(ctx,
		func(string) (string, error) { return "", errors.New("no pkg-config") },
		func(_ context.Context, name string, _ ...string) error {
			if name == "sh" {
				return nil
			}
			return errors.New("not matched")
		},
	)
	if !r.OK {
		t.Fatalf("want OK via ldconfig, got %+v", r)
	}

	// both fail
	r = checkAppIndicator(ctx,
		func(string) (string, error) { return "/usr/bin/pkg-config", nil },
		func(_ context.Context, _ string, _ ...string) error { return errors.New("missing") },
	)
	if r.OK {
		t.Fatalf("want failure, got %+v", r)
	}
}

func TestPrintResults(t *testing.T) {
	var buf bytes.Buffer
	failed := printResults(&buf, []checkResult{
		{Name: "a", OK: true},
		{Name: "b", OK: false, Detail: "broken"},
		{Name: "c", OK: false}, // no detail
	})
	if failed != 2 {
		t.Fatalf("want 2 failed, got %d", failed)
	}
	out := buf.String()
	for _, want := range []string{"✓ a\n", "✗ b: broken\n", "✗ c\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q\n---\n%s", want, out)
		}
	}
}
