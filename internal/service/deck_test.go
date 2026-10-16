package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/store/sqlite"
)

func TestDeckPersistsAcrossRestartAndSeparatesOwners(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "deck.db")
	db, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Settings{Deps: Deps{Store: db}}
	first, err := s.Deck(ctx, "phone:a")
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"needs", "calendar", "projects", "mywork", "agents", "analytics"} {
		if first.Slots[i] == nil || first.Slots[i].Button != id {
			t.Fatalf("default %d = %+v", i, first)
		}
	}
	if first.Slots[6] != nil || first.Slots[7] != nil || first.Slots[8] != nil {
		t.Fatal("default sockets occupied")
	}

	projects := &Projects{Deps: s.Deps, Git: fakeGit{}}
	project, err := projects.Register(ctx, "/repos/deck", "Deck")
	if err != nil {
		t.Fatal(err)
	}
	first.Slots[6] = &DeckSlot{ID: "workflow", Button: "sprint-summary", ProjectID: project.ID}
	first.Slots[0], first.Slots[8] = nil, first.Slots[0]
	saved, err := s.SetDeck(ctx, "phone:a", first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDeck(ctx, "phone:a", first); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}

	original, err := s.PrepareDeckTask(ctx, &Tasks{Deps: s.Deps}, "phone:a", "workflow", "restart-request-1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s = &Settings{Deps: Deps{Store: db}}
	got, err := s.Deck(ctx, "phone:a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != saved.Version || got.Slots[0] != nil || got.Slots[8].Button != "needs" {
		t.Fatalf("restart: %+v", got)
	}
	other, _ := s.Deck(ctx, "phone:b")
	if other.Version != 0 || other.Slots[0].Button != "needs" {
		t.Fatal("layout leaked between phones")
	}
	empty := DeckLayout{Version: got.Version}
	if _, err = s.SetDeck(ctx, "phone:a", empty); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Deck(ctx, "phone:a")
	for _, slot := range got.Slots {
		if slot != nil {
			t.Fatal("empty deck reset to defaults")
		}
	}
	// An accepted invocation keeps its target after restart and removal of its button.
	replay, err := s.PrepareDeckTask(ctx, &Tasks{Deps: s.Deps}, "phone:a", "workflow", "restart-request-1234")
	if err != nil || replay.ID != original.ID {
		t.Fatalf("restart replay = %+v, %v", replay, err)
	}
	history, err := s.DeckHistory(ctx, "phone:a")
	if err != nil || len(history) != 1 || history[0].TaskID != original.ID {
		t.Fatalf("history = %+v, %v", history, err)
	}
}

func TestDeckRejectsInvalidConfiguration(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Settings{Deps: Deps{Store: db}, Catalog: catalog{"codex": {}}}
	for _, slot := range []DeckSlot{
		{ID: "x", Button: "arbitrary-command"},
		{ID: "x", Button: "git", Agent: "codex"},
		{ID: "x", Button: "find-bugs", Agent: "codex"},
		{ID: "x", Button: "find-bugs", ProjectID: "missing", Agent: "unknown"},
		{ID: "x", Button: "find-bugs", ProjectID: "missing", Agent: "codex", Model: "--unsafe"},
	} {
		in := DefaultDeck()
		in.Slots[6] = &slot
		if _, err := s.SetDeck(context.Background(), "owner", in); err == nil {
			t.Fatalf("accepted %+v", slot)
		}
	}
	in := DefaultDeck()
	in.Slots[6] = in.Slots[0]
	if _, err := s.SetDeck(context.Background(), "owner", in); err == nil {
		t.Fatal("duplicate slot IDs accepted")
	}
}
