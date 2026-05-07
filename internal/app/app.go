// Package app is the Wails v2 bridge between the Go core and the React
// frontend. Methods on App become JS-callable bindings; keep the surface
// small and serializable.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	appconfig "github.com/meopedevts/revu/internal/config"
	"github.com/meopedevts/revu/internal/github"
	"github.com/meopedevts/revu/internal/poller"
	"github.com/meopedevts/revu/internal/profiles"
	"github.com/meopedevts/revu/internal/store"
	"github.com/meopedevts/revu/internal/uilimits"
)

// EventProfilesActiveChanged is emitted on the Wails bus whenever the active
// profile changes. Frontend listens to refresh the header badge.
const EventProfilesActiveChanged = "profiles:active-changed"

// App is the exported type Wails generates TS/JS bindings for.
type App struct {
	store     store.Store
	cfgMgr    *appconfig.Manager
	profiles  *profiles.Service
	gh        github.Client
	onRefresh func()
	// onWindowShown é disparado quando a janela é apresentada via
	// ShowWindow/ShowSettings (REV-51). Usado pra limpar o badge attention
	// do tray + persistir ack no store.
	onWindowShown func()
	log           *slog.Logger

	mu  sync.RWMutex
	ctx context.Context
}

// Option customizes App.
type Option func(*App)

// WithLogger injects a logger.
func WithLogger(l *slog.Logger) Option {
	return func(a *App) {
		if l != nil {
			a.log = l
		}
	}
}

// WithProfiles injects the profile service so the bridge can expose CRUD
// handlers to the frontend.
func WithProfiles(p *profiles.Service) Option {
	return func(a *App) {
		a.profiles = p
	}
}

// WithGitHubClient injects the gh-backed client used by the PR details
// bindings. When nil, GetPRDetails/GetPRDiff/MergePR return an explicit
// error so the frontend can surface "feature unavailable" instead of
// panicking.
func WithGitHubClient(c github.Client) Option {
	return func(a *App) {
		a.gh = c
	}
}

// New wires the bridge. onRefresh is called by RefreshNow to ping the poller.
// The callback can be rewired post-construction via SetOnRefresh to break the
// cyclic dep between App and Poller (App needs poller.Trigger; Poller needs
// App.OnPollEvent). cfgMgr may be nil in smoke builds — the settings-related
// bindings degrade to defaults / sentinel errors in that case.
func New(s store.Store, cfgMgr *appconfig.Manager, onRefresh func(), opts ...Option) *App {
	a := &App{
		store:     s,
		cfgMgr:    cfgMgr,
		onRefresh: onRefresh,
		log:       slog.Default(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// SetOnRefresh replaces the refresh callback. Safe to call after New but
// before Wails runtime is serving requests.
func (a *App) SetOnRefresh(fn func()) {
	a.mu.Lock()
	a.onRefresh = fn
	a.mu.Unlock()
}

// SetOnWindowShown registra o callback disparado quando a janela é
// apresentada via ShowWindow/ShowSettings*. Wired pelo run.go após o tray
// existir — chamada típica limpa attention + ack no store.
func (a *App) SetOnWindowShown(fn func()) {
	a.mu.Lock()
	a.onWindowShown = fn
	a.mu.Unlock()
}

// fireWindowShown invoca o callback registrado, se houver. Helper privado
// pra evitar duplicar lock + nil-check em cada caller.
func (a *App) fireWindowShown() {
	a.mu.RLock()
	fn := a.onWindowShown
	a.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// OnStartup is the Wails lifecycle hook fired when the runtime is ready.
// Store the ctx so later methods can call wruntime.WindowShow / Hide.
func (a *App) OnStartup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

// getCtx returns the Wails runtime context, if set.
func (a *App) getCtx() context.Context {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ctx
}

// WailsCtx exposes the Wails runtime context for callers that need to invoke
// package-level runtime helpers (e.g. runtime.Quit from an out-of-band
// shutdown watcher). Returns nil before OnStartup has fired.
func (a *App) WailsCtx() context.Context { return a.getCtx() }

// ListPendingPRs returns PRs with review_pending=true, most-recent first.
func (a *App) ListPendingPRs() []store.PRRecord {
	return a.store.GetPending(a.callCtx())
}

// ListHistoryPRs returns PRs with review_pending=false, most-recent first.
func (a *App) ListHistoryPRs() []store.PRRecord {
	return a.store.GetHistory(a.callCtx())
}

// AcknowledgeTray persiste o ack do estado attention do tray. Chamado pelo
// frontend quando a janela monta (REV-54) — garante que abrir via window
// manager / atalho de teclado limpe o "novo desde última visualização"
// mesmo quando o caminho não passa por ShowWindow (que já dispara ack via
// onWindowShown). Erros propagam pra o frontend porque ack falhado deixa o
// dot eternamente aceso.
func (a *App) AcknowledgeTray() error {
	return a.store.Acknowledge(a.callCtx(), time.Now().UTC())
}

// GetTrayAcknowledgedAt devolve o timestamp do último ack como ISO-8601 ou
// string vazia se nunca acked. Frontend compara com firstSeenAt do PR pra
// decidir se renderiza o dot "novo".
func (a *App) GetTrayAcknowledgedAt() (string, error) {
	t, ok, err := a.store.AcknowledgedAt(a.callCtx())
	if err != nil {
		return "", fmt.Errorf("get tray acknowledged at: %w", err)
	}
	if !ok {
		return "", nil
	}
	return t.UTC().Format(time.RFC3339Nano), nil
}

// OpenPRInBrowser hands the URL off to xdg-open. Errors are logged, not
// surfaced — the frontend has no recovery path.
func (a *App) OpenPRInBrowser(url string) {
	if url == "" {
		return
	}
	ctx := a.getCtx()
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "xdg-open", url)
	if err := cmd.Start(); err != nil {
		a.log.Warn("xdg-open failed", "url", url, "err", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}

// RefreshNow asks the poller to run an out-of-schedule tick.
func (a *App) RefreshNow() {
	a.mu.RLock()
	fn := a.onRefresh
	a.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// ShowWindow reveals the window (used by the tray "Abrir" item and after
// launch from autostart when user wants to inspect the list). Dispara
// onWindowShown (REV-51) pra limpar attention + ack no store.
func (a *App) ShowWindow() {
	ctx := a.getCtx()
	if ctx == nil {
		return
	}
	wruntime.WindowShow(ctx)
	a.fireWindowShown()
}

// HideWindow hides the window without terminating the process — used by the
// frontend when the user clicks the X (HideWindowOnClose handles the native
// close; this method lets the React side trigger a hide from custom UI).
func (a *App) HideWindow() {
	ctx := a.getCtx()
	if ctx == nil {
		return
	}
	wruntime.WindowHide(ctx)
}

// NavigatePayload é o payload do evento "ui:navigate". Section é opcional
// e permite deep-link em uma seção específica de settings (ex.: "sync").
type NavigatePayload struct {
	Target  string `json:"target"`
	Section string `json:"section,omitempty"`
}

// ShowSettings shows the window (if hidden) and emits ui:navigate so the
// frontend switches to the settings view. Wired from the tray "Configurações"
// item.
func (a *App) ShowSettings() {
	a.ShowSettingsSection("")
}

// ShowSettingsSection é como ShowSettings mas indica a seção alvo
// (ex.: "accounts","sync"). Section vazia → frontend usa default. Dispara
// onWindowShown (REV-51) pra limpar attention + ack.
func (a *App) ShowSettingsSection(section string) {
	ctx := a.getCtx()
	if ctx == nil {
		return
	}
	wruntime.WindowShow(ctx)
	wruntime.EventsEmit(ctx, "ui:navigate", NavigatePayload{
		Target:  "settings",
		Section: section,
	})
	a.fireWindowShown()
}

// GetConfig returns the current runtime configuration. Falls back to the
// spec baseline when no Manager is wired (smoke path).
func (a *App) GetConfig() appconfig.Config {
	if a.cfgMgr == nil {
		return appconfig.Defaults()
	}
	return a.cfgMgr.Current()
}

// UpdateConfig persists c to disk after strict validation. Returns
// *appconfig.ValidationError on bad input, wrapped I/O errors on disk
// failure, or a sentinel when no Manager is wired.
func (a *App) UpdateConfig(c appconfig.Config) error {
	if a.cfgMgr == nil {
		return errors.New("config manager not available")
	}
	return a.cfgMgr.Update(c)
}

// ClearHistory deletes every non-OPEN record and returns the row count.
// Used by the settings "Limpar histórico agora" button.
func (a *App) ClearHistory() (int, error) {
	return a.store.ClearHistory(a.callCtx())
}

const (
	themeLight = "light"
	themeDark  = "dark"
)

// GetTheme returns the persisted UI theme ("light" or "dark"). Falls back to
// "light" when no Manager is wired (smoke path) or the value on disk is
// unexpected — keeps the frontend boot deterministic.
func (a *App) GetTheme() string {
	if a.cfgMgr == nil {
		return themeLight
	}
	t := a.cfgMgr.Current().Theme
	if t != themeLight && t != themeDark {
		return themeLight
	}
	return t
}

// SetTheme persists the new theme atomically via the config manager. Returns
// *appconfig.ValidationError on unsupported values, wrapped I/O errors on
// disk failure, or a sentinel when no Manager is wired.
func (a *App) SetTheme(theme string) error {
	if a.cfgMgr == nil {
		return errors.New("config manager not available")
	}
	c := a.cfgMgr.Current()
	c.Theme = theme
	return a.cfgMgr.Update(c)
}

// errGitHubUnavailable is returned when a PR-details binding is called
// without a gh client wired. Clean error for smoke/test builds.
var errGitHubUnavailable = errors.New("github client not available")

// mergeMethodSquash / mergeMethodMerge são os identificadores enviados pelo
// frontend nos métodos MergePR / ApproveAndMergePR. Centralizados como
// constantes pra evitar repetição e drift.
const (
	mergeMethodSquash = "squash"
	mergeMethodMerge  = "merge"
)

// errPRNotFound signals that the frontend referenced an id the store no
// longer tracks — e.g. history was cleared between list and click.
var errPRNotFound = errors.New("pr not tracked")

// GetPRDetails fetches the full metadata for the PR view. prID is the
// store-side id (owner/repo#number); the URL is resolved server-side so
// the frontend never has to learn the gh command shape.
func (a *App) GetPRDetails(prID string) (*github.PRFullDetails, error) {
	if a.gh == nil {
		return nil, errGitHubUnavailable
	}
	ctx := a.callCtx()
	rec, ok := a.store.GetByID(ctx, prID)
	if !ok {
		return nil, errPRNotFound
	}
	return a.gh.GetPRFullDetails(ctx, rec.URL)
}

// GetPRDiff returns the raw unified diff for the PR, or an empty string if
// (additions+deletions) exceeds the configured uilimits.DetailsDiffLimit. The
// frontend treats "" as "PR too big, show the open-in-GitHub fallback".
func (a *App) GetPRDiff(prID string) (string, error) {
	if a.gh == nil {
		return "", errGitHubUnavailable
	}
	ctx := a.callCtx()
	rec, ok := a.store.GetByID(ctx, prID)
	if !ok {
		return "", errPRNotFound
	}
	if rec.Additions+rec.Deletions > uilimits.DetailsDiffLimit {
		return "", nil
	}
	return a.gh.GetPRDiff(ctx, rec.URL)
}

// ApproveAndMergeResult informa qual passo do fluxo composto Approve+Merge
// concluiu. FailedStep vazio + ErrorMessage vazio == sucesso. Quando algum
// passo falha, o método retorna o struct populado e nil error — o frontend
// distingue passo do erro pra mostrar mensagem específica (REV-62). Erros de
// validação prévia (gh indisponível, PR não encontrado, método inválido)
// continuam vindo como error pra rejeitar a Promise no JS.
type ApproveAndMergeResult struct {
	FailedStep   string `json:"failedStep,omitempty"`
	Approved     bool   `json:"approved"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// MergePR runs `gh pr merge` with the chosen method. On success it nudges
// the poller so the next tick re-enriches the PR — the store then detects
// MergedAt != nil and flips state to MERGED, which moves the PR into the
// history tab via the existing pr:status-changed event path.
func (a *App) MergePR(prID string, method string) error {
	if a.gh == nil {
		return errGitHubUnavailable
	}
	ctx := a.callCtx()
	rec, ok := a.store.GetByID(ctx, prID)
	if !ok {
		return errPRNotFound
	}
	var m github.MergeMethod
	switch method {
	case mergeMethodSquash:
		m = github.MergeMethodSquash
	case mergeMethodMerge:
		m = github.MergeMethodMerge
	default:
		return fmt.Errorf("unsupported merge method: %q", method)
	}
	if err := a.gh.MergePR(ctx, rec.URL, m); err != nil {
		return err
	}
	// Nudge the poller out-of-schedule so the newly-merged state is picked
	// up right away instead of waiting for the next tick.
	a.mu.RLock()
	fn := a.onRefresh
	a.mu.RUnlock()
	if fn != nil {
		fn()
	}
	return nil
}

// ApproveAndMergePR roda `gh pr review --approve` e em sequência o merge
// com o método escolhido. Existe pra contornar branch rulesets que exigem
// review aprovado antes de merge (REV-62). Erros de gh em qualquer passo
// vêm embutidos no struct — Promise resolve sempre que o pipeline foi
// invocado. Erros de validação prévia caem em error normal.
func (a *App) ApproveAndMergePR(prID string, method string) (ApproveAndMergeResult, error) {
	res := ApproveAndMergeResult{}
	if a.gh == nil {
		return res, errGitHubUnavailable
	}
	ctx := a.callCtx()
	rec, ok := a.store.GetByID(ctx, prID)
	if !ok {
		return res, errPRNotFound
	}
	var m github.MergeMethod
	switch method {
	case mergeMethodSquash:
		m = github.MergeMethodSquash
	case mergeMethodMerge:
		m = github.MergeMethodMerge
	default:
		return res, fmt.Errorf("unsupported merge method: %q", method)
	}

	if err := a.gh.ApprovePR(ctx, rec.URL); err != nil {
		res.FailedStep = "approve"
		res.ErrorMessage = err.Error()
		return res, nil //nolint:nilerr // erro vem embutido no struct (REV-62 — Wails descarta o struct quando error != nil)
	}
	res.Approved = true

	if err := a.gh.MergePR(ctx, rec.URL, m); err != nil {
		res.FailedStep = "merge"
		res.ErrorMessage = err.Error()
		return res, nil //nolint:nilerr // erro vem embutido no struct (REV-62 — Wails descarta o struct quando error != nil)
	}

	a.mu.RLock()
	fn := a.onRefresh
	a.mu.RUnlock()
	if fn != nil {
		fn()
	}
	return res, nil
}

// OnPollEvent is the EventHandler wired into the poller. It forwards each
// event to the Wails event bus so the frontend can react live. Events emitted
// before OnStartup (i.e. before the Wails runtime is up) are dropped — the
// frontend will pick up the current state on its initial fetch.
func (a *App) OnPollEvent(e poller.Event) {
	ctx := a.getCtx()
	if ctx == nil {
		return
	}
	wruntime.EventsEmit(ctx, e.Kind, e)
}

// EmitActiveProfileChanged relays an active-profile change from the profiles
// service to the frontend. Wiring is done by main via SubscribeActive.
func (a *App) EmitActiveProfileChanged(p profiles.Profile) {
	ctx := a.getCtx()
	if ctx == nil {
		return
	}
	wruntime.EventsEmit(ctx, EventProfilesActiveChanged, p)
}

// ===== Profiles bindings =====
//
// Every binding below degrades to a clear error when a.profiles is nil so the
// smoke path (no DB wired) still boots; the frontend treats nil as "profiles
// feature disabled".

var errProfilesUnavailable = errors.New("profiles service not available")

// ListProfiles returns every profile known to the store.
func (a *App) ListProfiles() ([]profiles.Profile, error) {
	if a.profiles == nil {
		return nil, errProfilesUnavailable
	}
	return a.profiles.List(a.callCtx())
}

// GetActiveProfile returns the currently-active profile.
func (a *App) GetActiveProfile() (profiles.Profile, error) {
	if a.profiles == nil {
		return profiles.Profile{}, errProfilesUnavailable
	}
	return a.profiles.GetActive(a.callCtx())
}

// CreateProfileRequest is the frontend-facing shape of CreateProfile's input.
type CreateProfileRequest struct {
	Name       string `json:"name"`
	Method     string `json:"auth_method"`
	Token      string `json:"token"`
	MakeActive bool   `json:"make_active"`
}

// CreateProfile validates inputs, stores the token in the keyring when
// applicable, and persists the profile.
func (a *App) CreateProfile(req CreateProfileRequest) (profiles.Profile, error) {
	if a.profiles == nil {
		return profiles.Profile{}, errProfilesUnavailable
	}
	return a.profiles.Create(a.callCtx(), profiles.CreateParams{
		Name:       req.Name,
		Method:     profiles.AuthMethod(req.Method),
		Token:      req.Token,
		MakeActive: req.MakeActive,
	})
}

// UpdateProfileRequest carries partial updates. Empty/omitted fields are
// left untouched; token="" means "do not rotate".
type UpdateProfileRequest struct {
	ID     string  `json:"id"`
	Name   *string `json:"name,omitempty"`
	Method *string `json:"auth_method,omitempty"`
	Token  *string `json:"token,omitempty"`
}

// UpdateProfile applies a partial update.
func (a *App) UpdateProfile(req UpdateProfileRequest) (profiles.Profile, error) {
	if a.profiles == nil {
		return profiles.Profile{}, errProfilesUnavailable
	}
	u := profiles.Update{Name: req.Name, Token: req.Token}
	if req.Method != nil {
		m := profiles.AuthMethod(*req.Method)
		u.Method = &m
	}
	return a.profiles.Update(a.callCtx(), req.ID, u)
}

// DeleteProfile removes a profile. Rejects the active one and the last.
func (a *App) DeleteProfile(id string) error {
	if a.profiles == nil {
		return errProfilesUnavailable
	}
	return a.profiles.Delete(a.callCtx(), id)
}

// SetActiveProfile flips which profile is active.
func (a *App) SetActiveProfile(id string) error {
	if a.profiles == nil {
		return errProfilesUnavailable
	}
	return a.profiles.SetActive(a.callCtx(), id)
}

// ValidateToken asks GitHub who owns the PAT. Returns the GitHub login on
// success; never logs the token.
func (a *App) ValidateToken(token string) (string, error) {
	if a.profiles == nil {
		return "", errProfilesUnavailable
	}
	return a.profiles.ValidateToken(a.callCtx(), token)
}

// callCtx returns the Wails runtime context if ready, else Background.
// Wails bindings always have a ctx but during smoke/testing it may be nil.
func (a *App) callCtx() context.Context {
	if c := a.getCtx(); c != nil {
		return c
	}
	return context.Background()
}
