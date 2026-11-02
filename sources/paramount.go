package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Paramount reads Paramount+'s Keep Watching row: one in-progress title per
// show with season, episode and percent. An episode at or past MinProgress
// counts as watched. No timestamps are available (caller uses run time);
// earlier episodes come from Up Next, which Paramount+ also feeds.
type Paramount struct {
	Cookies     CookieLoader
	MinProgress int
}

func (p *Paramount) Name() string { return "paramount" }

func (p *Paramount) Fetch(ctx context.Context) ([]Watch, error) {
	jar, err := p.Cookies(ctx, ".paramountplus.com")
	if err != nil {
		return nil, err
	}
	resp, err := fetch(ctx, "https://www.paramountplus.com/carousels/homepage/keepwatching/offset/0/limit/50/",
		map[string]string{"Cookie": jar.Header(), "Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	if resp.Status != 200 {
		return nil, fmt.Errorf("paramount+: HTTP %d", resp.Status)
	}
	return ParseParamount(resp.Body, p.MinProgress)
}

// ParseParamount decodes the keep-watching carousel.
func ParseParamount(body []byte, minProgress int) ([]Watch, error) {
	var doc struct {
		Success bool `json:"success"`
		Result  struct {
			Data []struct {
				Title           string   `json:"title"`
				ShowSeriesTitle string   `json:"showSeriesTitle"`
				SeasonNumber    looseNum `json:"seasonNumber"`
				EpisodeNumber   looseNum `json:"episodeNumber"`
				Progress        looseNum `json:"progress"`
				IsMovie         bool     `json:"isMovie"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("paramount+: %w (signed in?)", err)
	}
	var out []Watch
	for _, it := range doc.Result.Data {
		prog := float64(it.Progress)
		if math.IsNaN(prog) || prog < float64(minProgress) || prog > 100 {
			continue
		}
		if it.IsMovie {
			out = append(out, Watch{Kind: KindMovie, Source: "paramount", MovieTitle: it.Title})
			continue
		}
		sf, ef := float64(it.SeasonNumber), float64(it.EpisodeNumber)
		if sf != math.Trunc(sf) || ef != math.Trunc(ef) {
			continue
		}
		s, e := int(sf), int(ef)
		show := it.ShowSeriesTitle
		if show == "" {
			show = it.Title
		}
		if s <= 0 || e <= 0 || show == "" {
			continue
		}
		out = append(out, Watch{Kind: KindEpisode, Source: "paramount", ShowTitle: show, Season: s, Episode: e})
	}
	return out, nil
}

// looseNum decodes a number sent as a JSON number, a numeric string, or ""
// (Paramount+ uses all three).
type looseNum float64

func (n *looseNum) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch t := v.(type) {
	case float64:
		*n = looseNum(t)
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			f = 0
		}
		*n = looseNum(f)
	}
	return nil
}
