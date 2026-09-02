package cmd

import (
	"reflect"
	"testing"
	"time"

	"github.com/omarshahine/trakt-plugin/api"
)

func airedEpisodes(n int, numbers ...int) []struct {
	Number     int        `json:"number"`
	FirstAired *time.Time `json:"first_aired"`
} {
	past := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -30)
	var eps []struct {
		Number     int        `json:"number"`
		FirstAired *time.Time `json:"first_aired"`
	}
	for _, num := range numbers {
		at := past
		eps = append(eps, struct {
			Number     int        `json:"number"`
			FirstAired *time.Time `json:"first_aired"`
		}{Number: num, FirstAired: &at})
	}
	return eps
}

func episodeList(entries ...struct {
	Number     int        `json:"number"`
	FirstAired *time.Time `json:"first_aired"`
}) []struct {
	Number     int        `json:"number"`
	FirstAired *time.Time `json:"first_aired"`
} {
	return entries
}

func TestFilterPendingEpisodes(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	future := now.AddDate(0, 0, 7)

	seasons := []api.ShowSeason{
		{Number: 1, Episodes: airedEpisodes(1, 1, 2)},
		{Number: 2, Episodes: append(
			airedEpisodes(2, 1, 2, 3),
			struct {
				Number     int        `json:"number"`
				FirstAired *time.Time `json:"first_aired"`
			}{Number: 4, FirstAired: &future},
		)},
		{Number: 3}, // announced, no episodes yet
	}

	tests := []struct {
		name         string
		seasons      []api.ShowSeason
		watched      map[string]bool
		wantPending  []api.SyncSeason
		wantWatchedN int
	}{
		{
			name:    "nothing watched: all aired episodes pending, unaired excluded",
			seasons: seasons,
			watched: map[string]bool{},
			wantPending: []api.SyncSeason{
				{Number: 1, Episodes: []api.SyncEpisode{{Number: 1}, {Number: 2}}},
				{Number: 2, Episodes: []api.SyncEpisode{{Number: 1}, {Number: 2}, {Number: 3}}},
			},
		},
		{
			name:    "partially watched: only new episodes pending",
			seasons: seasons,
			watched: map[string]bool{"1:1": true, "1:2": true, "2:1": true},
			wantPending: []api.SyncSeason{
				{Number: 2, Episodes: []api.SyncEpisode{{Number: 2}, {Number: 3}}},
			},
			wantWatchedN: 3,
		},
		{
			name:         "fully caught up: nothing pending",
			seasons:      seasons,
			watched:      map[string]bool{"1:1": true, "1:2": true, "2:1": true, "2:2": true, "2:3": true},
			wantPending:  nil,
			wantWatchedN: 5,
		},
		{
			name: "episodes with unknown air date are never added",
			seasons: []api.ShowSeason{{
				Number:   1,
				Episodes: episodeList(struct {
					Number     int        `json:"number"`
					FirstAired *time.Time `json:"first_aired"`
				}{Number: 1, FirstAired: nil}),
			}},
			watched:     map[string]bool{},
			wantPending: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pending, watchedN := filterPendingEpisodes(tt.seasons, tt.watched, now)
			if !reflect.DeepEqual(pending, tt.wantPending) {
				t.Errorf("pending = %+v, want %+v", pending, tt.wantPending)
			}
			if watchedN != tt.wantWatchedN {
				t.Errorf("watchedCount = %d, want %d", watchedN, tt.wantWatchedN)
			}
		})
	}
}
