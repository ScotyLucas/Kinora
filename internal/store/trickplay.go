package store

import "strconv"

// TrickplaySource is a video file the trickplay generator works on.
type TrickplaySource struct {
	ID         int64
	Path       string
	RuntimeSec int
	HDR        string
	Sig        string // size-mtime: a replaced file gets new thumbnails
}

// TrickplaySources lists the present movies and episodes, one file per film or episode
// (other versions get thumbnails on demand when they are played).
func (s *Store) TrickplaySources() ([]TrickplaySource, error) {
	rows, err := s.DB.Query(`SELECT i.id, i.path, COALESCE(i.runtime_sec,0), COALESCE(i.hdr,''), i.size, i.mtime FROM items i
		WHERE i.kind IN ('movie','episode') AND i.missing=0 AND i.path<>''` + NotAltVersion + ` ORDER BY i.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrickplaySource
	for rows.Next() {
		var t TrickplaySource
		var size, mtime int64
		if err := rows.Scan(&t.ID, &t.Path, &t.RuntimeSec, &t.HDR, &size, &mtime); err != nil {
			return nil, err
		}
		t.Sig = fileSig(size, mtime)
		out = append(out, t)
	}
	return out, rows.Err()
}

// TrickplaySource returns one item's file, or ErrNotFound for anything but a present movie or episode.
func (s *Store) TrickplaySource(id int64) (TrickplaySource, error) {
	t := TrickplaySource{ID: id}
	var size, mtime int64
	err := s.DB.QueryRow(`SELECT path, COALESCE(runtime_sec,0), COALESCE(hdr,''), size, mtime FROM items
		WHERE id=? AND kind IN ('movie','episode') AND missing=0 AND path<>''`, id).Scan(&t.Path, &t.RuntimeSec, &t.HDR, &size, &mtime)
	if err != nil {
		return t, ErrNotFound
	}
	t.Sig = fileSig(size, mtime)
	return t, nil
}

func fileSig(size, mtime int64) string {
	return strconv.FormatInt(size, 10) + "-" + strconv.FormatInt(mtime, 10)
}
