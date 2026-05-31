package ui

import (
	"strings"
	"testing"

	"github.com/0xjuanma/golazo/internal/api"
)

func TestRenderAggregateSection_WithData(t *testing.T) {
	details := &api.MatchDetails{
		AggregateScore:     "5 - 7",
		WhoLostOnAggregate: "Juventus",
	}

	lines := renderAggregateSection(details, 60)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "AGG.") {
		t.Errorf("expected output to contain %q, got:\n%s", "AGG.", joined)
	}
	if !strings.Contains(joined, "5 - 7") {
		t.Errorf("expected output to contain %q, got:\n%s", "5 - 7", joined)
	}
	if !strings.Contains(joined, "Juventus eliminated") {
		t.Errorf("expected output to contain %q, got:\n%s", "Juventus eliminated", joined)
	}
}

func TestRenderAggregateSection_NoEliminated(t *testing.T) {
	details := &api.MatchDetails{
		AggregateScore:     "3 - 1",
		WhoLostOnAggregate: "",
	}

	lines := renderAggregateSection(details, 60)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "AGG.") {
		t.Errorf("expected output to contain %q", "AGG.")
	}
	if !strings.Contains(joined, "3 - 1") {
		t.Errorf("expected output to contain %q", "3 - 1")
	}
	if strings.Contains(joined, "eliminated") {
		t.Errorf("expected no %q line when WhoLostOnAggregate is empty", "eliminated")
	}
}

func TestRenderMatchDetails_AggregateSection_Rendered(t *testing.T) {
	home, away := 3, 2
	details := &api.MatchDetails{
		Match: api.Match{
			Status:   api.MatchStatusFinished,
			HomeTeam: api.Team{Name: "Galatasaray", ShortName: "Gala"},
			AwayTeam: api.Team{Name: "Juventus", ShortName: "Juve"},
		},
		AggregateScore:     "5 - 7",
		WhoLostOnAggregate: "Juventus",
	}
	details.HomeScore = &home
	details.AwayScore = &away

	header, _ := RenderMatchDetails(MatchDetailsConfig{
		Width:   80,
		Height:  40,
		Details: details,
	})

	if !strings.Contains(header, "AGG.") {
		t.Errorf("RenderMatchDetails header should contain %q when AggregateScore is set", "AGG.")
	}
}

func TestRenderMatchDetails_NoAggregateSection_WhenEmpty(t *testing.T) {
	home, away := 2, 1
	details := &api.MatchDetails{
		Match: api.Match{
			Status:   api.MatchStatusFinished,
			HomeTeam: api.Team{Name: "Arsenal", ShortName: "ARS"},
			AwayTeam: api.Team{Name: "Chelsea", ShortName: "CHE"},
		},
	}
	details.HomeScore = &home
	details.AwayScore = &away

	header, _ := RenderMatchDetails(MatchDetailsConfig{
		Width:   80,
		Height:  40,
		Details: details,
	})

	if strings.Contains(header, "AGG.") {
		t.Errorf("RenderMatchDetails header should NOT contain %q for non-knockout match", "AGG.")
	}
}
