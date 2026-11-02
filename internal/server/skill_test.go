package server_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Arif-Karim/mcp-hub/internal/server"
	"github.com/Arif-Karim/mcp-hub/internal/skillfiles"
)

type fakeSkill struct {
	state    skillfiles.State
	err      error
	installs int
}

func (f *fakeSkill) State() skillfiles.State { return f.state }

func (f *fakeSkill) Install() (skillfiles.State, error) {
	f.installs++
	if f.err != nil {
		return skillfiles.State{}, f.err
	}
	f.state = skillfiles.State{Installed: true, UpToDate: true, Path: f.state.Path}
	return f.state, nil
}

func skillServer(f *fakeSkill) http.Handler {
	return server.New(server.Options{Version: "x", Skill: f})
}

func TestSkillInstallRoute(t *testing.T) {
	f := &fakeSkill{state: skillfiles.State{Path: "/home/dev/.claude/skills/mcp-hub/SKILL.md"}}
	rec := httptest.NewRecorder()
	skillServer(f).ServeHTTP(rec, localReq(http.MethodPost, "/api/v1/skill/install", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	var got skillfiles.State
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := skillfiles.State{Installed: true, UpToDate: true, Path: "/home/dev/.claude/skills/mcp-hub/SKILL.md"}
	if got != want || f.installs != 1 {
		t.Fatalf("body = %+v (installs %d), want %+v once", got, f.installs, want)
	}
}

func TestSkillInstallFailureIsInternalWithoutPath(t *testing.T) {
	f := &fakeSkill{err: errors.New("open /home/dev/.claude/skills: permission denied")}
	rec := httptest.NewRecorder()
	skillServer(f).ServeHTTP(rec, localReq(http.MethodPost, "/api/v1/skill/install", nil))
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError || body.Error.Code != "internal" {
		t.Fatalf("got %d %q, want 500 internal", rec.Code, body.Error.Code)
	}
	if body.Error.Message != "could not install the Claude Code skill; see the daemon log" {
		t.Fatalf("message = %q", body.Error.Message)
	}
}

func TestSkillInstallRejectsWrongMethodAndCrossSite(t *testing.T) {
	f := &fakeSkill{}
	h := skillServer(f)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localReq(http.MethodGet, "/api/v1/skill/install", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
		t.Fatalf("GET: %d Allow=%q, want 405 Allow=POST", rec.Code, rec.Header().Get("Allow"))
	}

	r := localReq(http.MethodPost, "/api/v1/skill/install", nil)
	r.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST: status = %d, want 403", rec.Code)
	}
	if f.installs != 0 {
		t.Fatalf("installs = %d, want 0", f.installs)
	}
}

func TestSkillRouteAbsentWithoutOption(t *testing.T) {
	rec := httptest.NewRecorder()
	server.New(server.Options{Version: "x"}).ServeHTTP(rec, localReq(http.MethodPost, "/api/v1/skill/install", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
