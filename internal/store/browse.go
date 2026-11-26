package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// Children returns the seasons of a series or the episodes of a season.
func (s *Store) Children(p *Profile, it *Item) ([]*Item, error) {
	switch it.Kind {
	case "series":
		return s.QueryItems(p, ` AND i.parent_id=? AND i.kind='season' AND i.missing=0`, []any{it.ID},
			`ORDER BY CASE WHEN i.season_num=0 THEN 1 ELSE 0 END, i.season_num`)
	case "season":
		return s.QueryItems(p, ` AND i.parent_id=? AND i.kind='episode' AND i.missing=0`+NotAltEpisode, []any{it.ID},
			`ORDER BY i.episode_num IS NULL, i.episode_num, i.sort_title`)
	}
	return []*Item{}, nil
}

// episodeOrder sorts episodes in watch order; specials (season 0) go last.
const episodeOrder = `ORDER BY CASE WHEN i.season_num=0 THEN 1 ELSE 0 END, i.season_num, i.episode_num IS NULL, i.episode_num, i.sort_title`

// NextEpisode returns the episode after `ep` in the same series (nil if last).
func (s *Store) NextEpisode(p *Profile, ep *Item) (*Item, error) {
	if ep.Kind != "episode" || ep.SeriesID == nil {
		return nil, nil
	}
	eps, err := s.QueryItems(p, ` AND i.series_id=? AND i.kind='episode' AND i.missing=0`+NotAltEpisode, []any{*ep.SeriesID}, episodeOrder)
	if err != nil {
		return nil, err
	}
	for i, e := range eps {
		if e.ID == ep.ID && i+1 < len(eps) {
			n := eps[i+1]
			if n.SeasonNum != nil && *n.SeasonNum == 0 && (ep.SeasonNum == nil || *ep.SeasonNum != 0) {
				return nil, nil // don't roll from the finale into specials
			}
			return n, nil
		}
	}
	return nil, nil
}

// PlayNext picks the episode a "Play" button on a series should start:
// an in-progress episode, else the next up, else the first episode.
func (s *Store) PlayNext(p *Profile, seriesID int64) (*Item, error) {
	eps, err := s.QueryItems(p, ` AND i.series_id=? AND i.kind='episode' AND i.missing=0`+NotAltEpisode, []any{seriesID}, episodeOrder)
	if err != nil || len(eps) == 0 {
		return nil, err
	}
	var lastIdx = -1
	var lastAt int64
	for i, e := range eps {
		if e.UserData.LastPlayedAt != nil && *e.UserData.LastPlayedAt >= lastAt && (e.UserData.Played || e.UserData.Position > 0) {
			lastAt, lastIdx = *e.UserData.LastPlayedAt, i
		}
	}
	if lastIdx >= 0 {
		last := eps[lastIdx]
		if !last.UserData.Played && last.UserData.Position > 0 {
			return last, nil
		}
		for _, e := range eps[lastIdx+1:] {
			if !e.UserData.Played && (e.SeasonNum == nil || *e.SeasonNum != 0) {
				return e, nil
			}
		}
	}
	for _, e := range eps {
		if !e.UserData.Played && (e.SeasonNum == nil || *e.SeasonNum != 0) {
			return e, nil
		}
	}
	return eps[0], nil
}

// ContinueWatching returns in-progress movies/episodes, newest first.
func (s *Store) ContinueWatching(p *Profile, limit int) ([]*Item, error) {
	return s.QueryItems(p, ` AND i.kind IN ('movie','episode') AND i.missing=0 AND ud.position>0 AND COALESCE(ud.played,0)=0
		AND COALESCE(ud.hidden,0)=0
		AND NOT EXISTS(SELECT 1 FROM user_data su WHERE su.item_id=i.series_id AND su.profile_id=ud.profile_id AND su.hidden=1)`+NotAltVersion,
		nil, `ORDER BY ud.last_played_at DESC LIMIT ?`, limit)
}

// NextUp returns, per recently watched series, the first unplayed episode after
// the most recently watched one (specials skipped). Hidden series are skipped,
// and episodes already in progress are left to Continue Watching.
func (s *Store) NextUp(p *Profile, limit int) ([]*Item, error) {
	rows, err := s.DB.Query(`SELECT e.series_id, MAX(u.last_played_at) m FROM user_data u JOIN items e ON e.id=u.item_id
		WHERE u.profile_id=? AND e.kind='episode' AND e.series_id IS NOT NULL AND (u.played=1 OR u.position>0)
		GROUP BY e.series_id ORDER BY m DESC LIMIT 50`, p.ID)
	if err != nil {
		return nil, err
	}
	var series []int64
	for rows.Next() {
		var id int64
		var m sql.NullInt64
		rows.Scan(&id, &m)
		series = append(series, id)
	}
	rows.Close()
	out := []*Item{}
	for _, sid := range series {
		if len(out) >= limit {
			break
		}
		if s.GetUserData(p.ID, sid).Hidden {
			continue
		}
		eps, err := s.QueryItems(p, ` AND i.series_id=? AND i.kind='episode' AND i.missing=0 AND COALESCE(i.season_num,1)<>0`+NotAltEpisode, []any{sid}, episodeOrder)
		if err != nil {
			return nil, err
		}
		lastIdx := -1
		var lastAt int64
		for i, e := range eps {
			if e.UserData.Played && e.UserData.LastPlayedAt != nil && *e.UserData.LastPlayedAt >= lastAt {
				lastAt, lastIdx = *e.UserData.LastPlayedAt, i
			}
		}
		if lastIdx < 0 {
			continue
		}
		for _, e := range eps[lastIdx+1:] {
			if e.UserData.Played {
				continue
			}
			if e.UserData.Position > 0 || e.UserData.Hidden {
				break // in Continue Watching, or explicitly hidden
			}
			out = append(out, e)
			break
		}
	}
	return out, nil
}

// RecentlyAdded lists the newest movies/series of a library (series by newest episode).
func (s *Store) RecentlyAdded(p *Profile, libID int64, limit int) ([]*Item, error) {
	return s.QueryItems(p, ` AND i.library_id=? AND i.kind IN ('movie','series') AND i.missing=0`+NotAltVersion, []any{libID},
		`ORDER BY i.added_at DESC, i.id DESC LIMIT ?`, limit)
}

// Search matches titles, overviews and tags; exact prefix matches come first.
func (s *Store) Search(p *Profile, q string, limit int) ([]*Item, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []*Item{}, nil
	}
	like := "%" + likeEscape(q) + "%"
	prefix := likeEscape(q) + "%"
	items, err := s.QueryItems(p, ` AND i.kind IN ('movie','series','episode') AND i.missing=0 AND (
		i.title LIKE ? ESCAPE '\' OR i.overview LIKE ? ESCAPE '\' OR tr.title LIKE ? ESCAPE '\' OR tr.overview LIKE ? ESCAPE '\' OR
		EXISTS(SELECT 1 FROM item_tags t WHERE t.item_id=i.id AND t.tag LIKE ? ESCAPE '\'))`+NotAltVersion,
		[]any{like, like, like, like, like},
		`ORDER BY CASE WHEN `+locTitle+` LIKE ? ESCAPE '\' THEN 0 WHEN `+locTitle+` LIKE ? ESCAPE '\' THEN 1 ELSE 2 END,
		 CASE i.kind WHEN 'episode' THEN 1 ELSE 0 END, `+locSort+` LIMIT ?`, prefix, like, limit)
	if err == nil {
		s.fillGenresTags(items, p)
	}
	return items, err
}

// Facet is a value with its count. Label is the value in the profile's metadata language, when it differs
// (genres only); filters keep using Value.
type Facet struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
	Count int    `json:"count"`
}

// Facets counts genres and tags over visible movies and series, a film with several versions once.
func (s *Store) Facets(p *Profile, libID int64) (genres, tags []Facet, err error) {
	rf, rargs := RatingSQL(p, "i")
	lib := ""
	args := []any{}
	if libID > 0 {
		lib = " AND i.library_id=?"
		args = append(args, libID)
	}
	args = append(args, rargs...)
	q := func(table, col string) ([]Facet, error) {
		rows, err := s.DB.Query(`SELECT x.`+col+`, COUNT(*) FROM `+table+` x JOIN items i ON i.id=x.item_id
			WHERE i.kind IN ('movie','series') AND i.missing=0`+lib+rf+NotAltVersion+` GROUP BY x.`+col+` COLLATE NOCASE ORDER BY COUNT(*) DESC, x.`+col, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []Facet{}
		for rows.Next() {
			var f Facet
			rows.Scan(&f.Value, &f.Count)
			out = append(out, f)
		}
		return out, rows.Err()
	}
	if genres, err = q("item_genres", "genre"); err != nil {
		return
	}
	if labels := s.genreLabels(metaLang(p)); len(labels) > 0 {
		for i := range genres {
			if l := labels[genres[i].Value]; l != "" && l != genres[i].Value {
				genres[i].Label = l
			}
		}
	}
	tags, err = q("item_tags", "tag")
	return
}

// FacetsOf counts one facet — "genre", "tag" or "year" — over the items a profile may see: films and series, or
// only the given kinds ("movie", "series"), optionally in one library. Genres and tags keep their stored value
// (no translated label); years come in ascending order, the others by count.
func (s *Store) FacetsOf(p *Profile, libID int64, kinds []string, facet string) ([]Facet, error) {
	var sel, from, group, order string
	switch facet {
	case "genre":
		sel, from, group, order = "x.genre", " JOIN item_genres x ON x.item_id=i.id", " GROUP BY x.genre COLLATE NOCASE", " ORDER BY COUNT(*) DESC, x.genre"
	case "tag":
		sel, from, group, order = "x.tag", " JOIN item_tags x ON x.item_id=i.id", " GROUP BY x.tag COLLATE NOCASE", " ORDER BY COUNT(*) DESC, x.tag"
	case "year":
		sel, from, group, order = "CAST(i.year AS TEXT)", "", " GROUP BY i.year", " ORDER BY i.year"
	default:
		return nil, fmt.Errorf("unknown facet %q", facet)
	}
	var ph []string
	var args []any
	for _, k := range kinds {
		if k == "movie" || k == "series" {
			ph = append(ph, "?")
			args = append(args, k)
		}
	}
	if len(ph) == 0 {
		ph, args = []string{"?", "?"}, []any{"movie", "series"}
	}
	where := " WHERE i.kind IN (" + strings.Join(ph, ",") + ") AND i.missing=0"
	if facet == "year" {
		where += " AND i.year > 0"
	}
	if libID > 0 {
		where += " AND i.library_id=?"
		args = append(args, libID)
	}
	rf, rargs := RatingSQL(p, "i")
	args = append(args, rargs...)
	rows, err := s.DB.Query(`SELECT `+sel+`, COUNT(*) FROM items i`+from+where+rf+NotAltVersion+group+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Facet{}
	for rows.Next() {
		var f Facet
		if err := rows.Scan(&f.Value, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
