package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/pkg/pluginsdk"
	"github.com/stretchr/testify/require"
)

type cursorSettingsHost struct {
	*fakeHost
	mu                    sync.Mutex
	state                 map[string]any
	readError, writeError error
}

func (h *cursorSettingsHost) GetState(_ context.Context, scope, scopeID, key string) (map[string]any, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if scope != "instance" || scopeID != "" || key != cursorTeamStateKey {
		return nil, false, errors.New("unexpected state key")
	}
	return maps.Clone(h.state), h.state != nil, h.readError
}

func (h *cursorSettingsHost) SetState(_ context.Context, scope, scopeID, key string, state map[string]any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if scope != "instance" || scopeID != "" || key != cursorTeamStateKey {
		return errors.New("unexpected state key")
	}
	if h.writeError != nil {
		return h.writeError
	}
	h.state = maps.Clone(state)
	return nil
}

func newCursorSettingsPlugin(t *testing.T, cfg map[string]any) (*plugin, *cursorSettingsHost) {
	t.Helper()
	p := newTestPlugin(t, cfg, nil, providerRunner(nil, map[string][]byte{"cursor": []byte(cursorEnterpriseCLI)}))
	host := &cursorSettingsHost{fakeHost: &fakeHost{config: cfg}}
	p.SetHost(host)
	p.cursor = cursorTestClient(t, cursorTeamFixtureHandler(t, nil, nil))
	return p, host
}

func selectCursorTeam(t *testing.T, p *plugin, body string) *pluginsdk.WebhookResponse {
	t.Helper()
	response, err := p.HandleWebhook(context.Background(), &pluginsdk.WebhookRequest{
		WebhookKey: webhookKeyCursorTeam, Method: "POST", Body: []byte(body),
	})
	require.NoError(t, err)
	return response
}

func TestCursorSettingsListsNamedTeamsAndMigratesLegacySelection(t *testing.T) {
	cfg := map[string]any{cursorTeamSetting: "  " + cursorSelectedTeam + " ", cursorCookieSetting: "private-cookie", "display_status_bar_mode": "both"}
	p, host := newCursorSettingsPlugin(t, cfg)
	p.cursor = cursorTestClient(t, cursorTeamFixtureHandler(t, map[string]string{
		"/api/dashboard/teams": `{"teams":[{"id":30677937,"name":"Zulu"},{"id":"30677936","name":"Alpha"},{"id":30677937,"name":"Duplicate"},{"id":0,"name":"Invalid"},{"id":7}]}`,
	}, nil))
	response, err := p.HandleWebhook(context.Background(), webhookGet(webhookKeyCursorTeams, ""))
	require.NoError(t, err)
	require.EqualValues(t, http.StatusOK, response.Status)
	var report cursorTeamsReport
	require.NoError(t, json.Unmarshal(response.Body, &report))
	require.Equal(t, cursorSelectedTeam, report.SelectedTeamID, "do not default to the first team")
	require.Equal(t, []cursorTeamOption{{"30677936", "Alpha"}, {"7", "Team 7"}, {cursorSelectedTeam, "Zulu"}}, report.Teams)
	require.Equal(t, map[string]any{"team_id": cursorSelectedTeam}, host.state)
	require.NotContains(t, string(response.Body), "private-cookie")
	require.NotContains(t, string(response.Body), "requestQuota")
	require.Equal(t, "both", cfg["display_status_bar_mode"])

	// The generated form no longer submits the old team field. State survives
	// both that save and recreation of the plugin process.
	delete(cfg, cursorTeamSetting)
	restarted, _ := newCursorSettingsPlugin(t, cfg)
	restarted.SetHost(host)
	effective, err := restarted.cursorConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, cursorSelectedTeam, effective[cursorTeamSetting])
	require.Equal(t, "private-cookie", effective[cursorCookieSetting])
}

func TestCursorSettingsSavePersistsSelectionAndClearsCachedOtherTeam(t *testing.T) {
	cfg := codexbarConfig(map[string]any{cursorTeamSetting: "30677936", "codexbar_providers": "cursor"})
	p, host := newCursorSettingsPlugin(t, cfg)
	p.snapshot = &AllProvidersReport{Providers: []ProviderUsage{*cursorBase(t)}}
	response := selectCursorTeam(t, p, `{"team_id":"30677937"}`)
	require.EqualValues(t, http.StatusOK, response.Status)
	require.Equal(t, map[string]any{"team_id": cursorSelectedTeam}, host.state)
	require.Equal(t, "30677936", cfg[cursorTeamSetting], "the other settings form's values are never rewritten")
	snapshot, _ := p.currentSnapshot()
	require.Nil(t, snapshot)
	report := p.pollOnce(context.Background(), 0)
	require.Len(t, report.Providers, 1)
	require.Equal(t, cursorSelectedTeam, report.Providers[0].TeamID)

	// Account usage can be explicitly restored even when the session expires;
	// the old manual team ID must not become active again on restart.
	p.cursor.auth = func(context.Context, map[string]any) (cursorAuth, error) { return cursorAuth{}, errors.New("expired") }
	response = selectCursorTeam(t, p, `{"team_id":""}`)
	require.EqualValues(t, http.StatusOK, response.Status)
	p.SetHost(host)
	effective, err := p.cursorConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "", effective[cursorTeamSetting])
}

func TestCursorSettingsRejectsInvalidUnavailableOrUnsavedSelections(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"team_id":null}`, `{"team_id":30677937}`, `{"team_id":"-1"}`, `{"team_id":"3e7"}`, `{"team_id":"1.5"}`, `{"team_id":"1","name":"fake"}`, `{"team_id":"1"} {}`, `{"team_id":"999"}`} {
		t.Run(body, func(t *testing.T) {
			p, host := newCursorSettingsPlugin(t, nil)
			host.state = map[string]any{"team_id": cursorSelectedTeam}
			snapshot := &AllProvidersReport{}
			p.snapshot = snapshot
			response := selectCursorTeam(t, p, body)
			require.EqualValues(t, http.StatusBadRequest, response.Status)
			require.Equal(t, cursorSelectedTeam, host.state["team_id"])
			current, _ := p.currentSnapshot()
			require.Same(t, snapshot, current)
		})
	}
	for _, failure := range []string{"session", "storage"} {
		t.Run(failure, func(t *testing.T) {
			p, host := newCursorSettingsPlugin(t, nil)
			host.state = map[string]any{"team_id": "30677936"}
			if failure == "session" {
				p.cursor = cursorTestClient(t, cursorTeamFixtureHandler(t, map[string]string{"/api/dashboard/teams": "unavailable"}, nil))
			} else {
				host.writeError = errors.New("private-storage-error")
			}
			response := selectCursorTeam(t, p, `{"team_id":"30677937"}`)
			require.GreaterOrEqual(t, response.Status, int32(500))
			require.Equal(t, "30677936", host.state["team_id"])
			require.NotContains(t, string(response.Body), "private-storage-error")
			require.NotContains(t, string(response.Body), "DO-NOT-EXPOSE")
		})
	}
}

func TestCursorSettingsUnavailableStateCannotExposeUnscopedUsage(t *testing.T) {
	p, host := newCursorSettingsPlugin(t, codexbarConfig(map[string]any{"codexbar_providers": "cursor"}))
	host.readError = errors.New("unavailable storage")
	report := p.pollOnce(context.Background(), 0)
	require.Empty(t, report.Providers)
	require.Len(t, report.Unavailable, 1)
	require.Contains(t, report.Unavailable[0].Message, "Cannot read the saved Cursor team")
}

func TestCursorSettingsListErrorsAndMethods(t *testing.T) {
	for _, teams := range []string{`{"teams":[]}`, `{"teams":{}}`, `unavailable`} {
		t.Run(teams, func(t *testing.T) {
			p, _ := newCursorSettingsPlugin(t, nil)
			p.cursor = cursorTestClient(t, cursorTeamFixtureHandler(t, map[string]string{"/api/dashboard/teams": teams}, nil))
			response := p.cursorTeamsWebhook(context.Background(), "GET")
			if teams == `{"teams":[]}` {
				require.EqualValues(t, http.StatusOK, response.Status)
			} else {
				require.EqualValues(t, http.StatusBadGateway, response.Status)
			}
			require.Contains(t, string(response.Body), `"teams":[]`)
			require.NotContains(t, string(response.Body), "DO-NOT-EXPOSE")
		})
	}
	p, _ := newCursorSettingsPlugin(t, nil)
	require.EqualValues(t, http.StatusMethodNotAllowed, p.cursorTeamsWebhook(context.Background(), "POST").Status)
	require.EqualValues(t, http.StatusMethodNotAllowed, p.cursorTeamWebhook(context.Background(), webhookGet(webhookKeyCursorTeam, "")).Status)
}

func TestCursorSettingsSwitchDuringPollDiscardsPreviousTeam(t *testing.T) {
	p, _ := newCursorSettingsPlugin(t, codexbarConfig(map[string]any{cursorTeamSetting: "30677936", "codexbar_providers": "cursor"}))
	started, resume := make(chan struct{}), make(chan struct{})
	var blocked atomic.Bool
	p.cursor = cursorTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body := "{}"
		switch r.URL.Path {
		case "/api/auth/me":
			body = `{"sub":"user_test","email":"cursor@example.test"}`
		case "/api/dashboard/teams":
			body = cursorTeamsFixture
		case "/api/dashboard/team", "/api/dashboard/get-team-spend":
			var request map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			id := string(request["teamId"])
			if r.URL.Path == "/api/dashboard/team" {
				body = `{"teamId":` + id + `,"userId":42}`
			} else {
				if id == "30677936" && blocked.CompareAndSwap(false, true) {
					close(started)
					select {
					case <-resume:
					case <-r.Context().Done():
						return
					}
				}
				body = `{"teamId":` + id + `,"teamMemberSpend":[{"userId":42,"fastPremiumRequests":37}]}`
			}
		}
		_, _ = w.Write([]byte(body))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan *AllProvidersReport, 1)
	go func() { result <- p.pollOnce(ctx, 0) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("poll never reached the old team")
	}
	response := selectCursorTeam(t, p, `{"team_id":"30677937"}`)
	close(resume)
	require.EqualValues(t, http.StatusOK, response.Status)
	select {
	case report := <-result:
		require.Len(t, report.Providers, 1)
		require.Equal(t, cursorSelectedTeam, report.Providers[0].TeamID)
		cached, _ := p.currentSnapshot()
		require.Same(t, report, cached)
	case <-ctx.Done():
		t.Fatal("poll did not finish after team selection")
	}
}
