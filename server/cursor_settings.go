package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/kandev/kandev/pkg/pluginsdk"
)

const (
	webhookKeyCursorTeams = "cursor-teams"
	webhookKeyCursorTeam  = "cursor-team"
	cursorTeamStateKey    = "cursor-team"
)

type cursorTeamOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cursorTeamsReport struct {
	Teams          []cursorTeamOption `json:"teams"`
	SelectedTeamID string             `json:"selected_team_id"`
	Error          string             `json:"error,omitempty"`
}

// Selection lives in plugin state so saving the host's separate schema form
// cannot overwrite it. Migrate the old manual ID before that form drops it.
// An explicit empty selection overrides even a legacy ID still in config.
func (p *plugin) cursorConfig(ctx context.Context) (map[string]any, error) {
	p.cursorSettingsMu.Lock()
	defer p.cursorSettingsMu.Unlock()
	host := p.Host()
	if host == nil {
		return map[string]any{}, nil
	}
	cfg, err := host.GetConfig(ctx)
	if err != nil {
		return nil, errors.New("Cannot read Cursor settings. Try again.")
	}
	cfg = maps.Clone(cfg)
	if cfg == nil {
		cfg = map[string]any{}
	}
	selection, found, err := host.GetState(ctx, "instance", "", cursorTeamStateKey)
	if err != nil {
		return nil, errors.New("Cannot read the saved Cursor team. Try again.")
	}
	if found {
		id, ok := selection["team_id"].(string)
		if !ok {
			return nil, errors.New("Cannot read the saved Cursor team. Choose a team again in plugin settings.")
		}
		cfg[cursorTeamSetting] = id
	} else if cursorTeamConfigured(cfg) {
		id, err := cursorConfiguredTeamID(cfg)
		if err != nil {
			return nil, err
		}
		canonical := strconv.FormatInt(id, 10)
		if err := host.SetState(ctx, "instance", "", cursorTeamStateKey, map[string]any{"team_id": canonical}); err != nil {
			return nil, errors.New("Cannot preserve the saved Cursor team. Try again.")
		}
		cfg[cursorTeamSetting] = canonical
	}
	return cfg, nil
}

func (c *cursorClient) teamOptions(ctx context.Context, cfg map[string]any) ([]cursorTeamOption, error) {
	auth, err := c.auth(ctx, cfg)
	if err != nil {
		return nil, err
	}
	teams, err := c.teams(ctx, auth)
	if err != nil {
		return nil, err
	}
	options := []cursorTeamOption{}
	seen := map[int64]bool{}
	for _, team := range teams {
		id := cursorID(team["id"])
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		option := cursorTeamOption{ID: strconv.FormatInt(id, 10), Name: strings.TrimSpace(team.text("name"))}
		if option.Name == "" {
			option.Name = "Team " + option.ID
		}
		options = append(options, option)
	}
	sort.Slice(options, func(i, j int) bool {
		a, b := strings.ToLower(options[i].Name), strings.ToLower(options[j].Name)
		if a == b {
			return options[i].ID < options[j].ID
		}
		return a < b
	})
	return options, nil
}

func (p *plugin) cursorTeamsWebhook(ctx context.Context, method string) *pluginsdk.WebhookResponse {
	if method != http.MethodGet {
		return cursorSettingsError(http.StatusMethodNotAllowed, "Use GET to list Cursor teams.")
	}
	ctx, cancel := context.WithTimeout(ctx, perProviderTimeout)
	defer cancel()
	report := cursorTeamsReport{Teams: []cursorTeamOption{}}
	cfg, err := p.cursorConfig(ctx)
	if err == nil {
		report.SelectedTeamID, _ = cfg[cursorTeamSetting].(string)
		if p.cursor == nil {
			err = errors.New("Cursor integration is unavailable.")
		} else {
			var teams []cursorTeamOption
			teams, err = p.cursor.teamOptions(ctx, cfg)
			if err == nil {
				report.Teams = teams
			}
		}
	}
	status := http.StatusOK
	if err != nil {
		report.Error, status = err.Error(), http.StatusBadGateway
	}
	return jsonResponse(int32(status), marshalOr(report, `{}`))
}

func cursorSettingsError(status int32, message string) *pluginsdk.WebhookResponse {
	return jsonResponse(status, marshalOr(map[string]string{"error": message}, `{}`))
}

func (p *plugin) cursorTeamWebhook(ctx context.Context, req *pluginsdk.WebhookRequest) *pluginsdk.WebhookResponse {
	if req.Method != http.MethodPost {
		return cursorSettingsError(http.StatusMethodNotAllowed, "Use POST to select a Cursor team.")
	}
	var body struct {
		TeamID *string `json:"team_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(req.Body))
	decoder.DisallowUnknownFields()
	if len(req.Body) > 4096 || decoder.Decode(&body) != nil || body.TeamID == nil || decoder.Decode(new(any)) != io.EOF {
		return cursorSettingsError(http.StatusBadRequest, "Provide a team_id string, or an empty string for account usage.")
	}
	id := strings.TrimSpace(*body.TeamID)
	if id != "" && (cursorID(json.RawMessage(id)) == 0 || strings.HasPrefix(id, "\"")) {
		return cursorSettingsError(http.StatusBadRequest, "Choose a valid Cursor team.")
	}
	ctx, cancel := context.WithTimeout(ctx, perProviderTimeout)
	defer cancel()
	if id != "" {
		id = strconv.FormatInt(cursorID(json.RawMessage(id)), 10)
		cfg, err := p.cursorConfig(ctx)
		if err != nil {
			return cursorSettingsError(http.StatusInternalServerError, err.Error())
		}
		if p.cursor == nil {
			return cursorSettingsError(http.StatusServiceUnavailable, "Cursor integration is unavailable.")
		}
		teams, err := p.cursor.teamOptions(ctx, cfg)
		if err != nil {
			return cursorSettingsError(http.StatusBadGateway, err.Error())
		}
		available := false
		for _, team := range teams {
			available = available || team.ID == id
		}
		if !available {
			return cursorSettingsError(http.StatusBadRequest, "This team is no longer available. Reload the team list and choose again.")
		}
	}
	if err := p.saveCursorTeam(ctx, id); err != nil {
		return cursorSettingsError(http.StatusInternalServerError, err.Error())
	}
	if !p.disablePoller {
		go p.pollOnce(context.Background(), cacheTTL)
	}
	return jsonResponse(http.StatusOK, marshalOr(map[string]string{"selected_team_id": id}, `{}`))
}

func (p *plugin) saveCursorTeam(ctx context.Context, id string) error {
	p.cursorSettingsMu.Lock()
	defer p.cursorSettingsMu.Unlock()
	host := p.Host()
	if host == nil || host.SetState(ctx, "instance", "", cursorTeamStateKey, map[string]any{"team_id": id}) != nil {
		return errors.New("Cannot save the Cursor team. Your previous selection is unchanged.")
	}
	p.mu.Lock()
	p.cursorSelectionRevision++
	p.snapshot, p.snapshotAt = nil, p.now()
	p.mu.Unlock()
	return nil
}
