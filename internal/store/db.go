// Package store is Kinora's SQLite persistence layer.
//
// Jellyfin problem it addresses: large libraries make the UI crawl while a scan
// is running, and backups/migrations are awkward. Kinora uses SQLite in WAL mode
// with short write transactions (readers never block on the scanner), a
// versioned migration list, and a one-click consistent backup via VACUUM INTO.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ErrNotFound is returned when a row does not exist (or is hidden by the rating filter).
var ErrNotFound = errors.New("nem található")

// Store wraps the database handle.
type Store struct {
	DB   *sql.DB
	path string

	mu        sync.RWMutex
	overrides map[string]string // settings forced by environment variables
}

// Open opens (and migrates) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("az adatbázis útvonalában nem lehet ? vagy # karakter: %s", path)
	}
	dsn := path + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=10000&_foreign_keys=on&_txlock=immediate"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(16)
	// Keep the connections open between requests: database/sql keeps only 2 idle by default, so under concurrent
	// load connections were closed and reopened all the time, and every new SQLite connection starts cold (schema
	// parse, empty page cache). Idle ones still close after 5 minutes.
	db.SetMaxIdleConns(16)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("adatbázis megnyitása: %w", err)
	}
	s := &Store{DB: db, path: path, overrides: map[string]string{}}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migráció: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

func now() int64 { return time.Now().Unix() }

// migrations are applied in order; never edit an existing entry, only append.
var migrations = []string{
	// 1: initial schema
	`
CREATE TABLE users(
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  pass_hash TEXT NOT NULL,
  is_admin INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE profiles(
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  color TEXT NOT NULL DEFAULT '#c97c5d',
  pin_hash TEXT,
  is_kids INTEGER NOT NULL DEFAULT 0,
  max_rating_level INTEGER,
  sub_langs TEXT NOT NULL DEFAULT '',
  audio_langs TEXT NOT NULL DEFAULT '',
  sub_mode TEXT NOT NULL DEFAULT 'auto',
  autoplay_next INTEGER NOT NULL DEFAULT 1,
  max_bitrate INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX profiles_user ON profiles(user_id);
CREATE TABLE sessions(
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  profile_id INTEGER REFERENCES profiles(id) ON DELETE SET NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE invites(
  code TEXT PRIMARY KEY,
  created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  note TEXT NOT NULL DEFAULT '',
  uses_left INTEGER NOT NULL,
  expires_at INTEGER,
  created_at INTEGER NOT NULL
);
CREATE TABLE libraries(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('movies','shows','mixed')),
  last_scan_at INTEGER,
  created_at INTEGER NOT NULL
);
CREATE TABLE items(
  id INTEGER PRIMARY KEY,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK(kind IN ('movie','series','season','episode')),
  parent_id INTEGER,
  series_id INTEGER,
  key TEXT NOT NULL,
  title TEXT NOT NULL,
  sort_title TEXT NOT NULL,
  year INTEGER,
  season_num INTEGER,
  episode_num INTEGER,
  episode_end INTEGER,
  overview TEXT NOT NULL DEFAULT '',
  content_rating TEXT NOT NULL DEFAULT '',
  rating_level INTEGER,
  community_rating REAL,
  runtime_sec INTEGER NOT NULL DEFAULT 0,
  path TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  mtime INTEGER NOT NULL DEFAULT 0,
  container TEXT NOT NULL DEFAULT '',
  video_codec TEXT NOT NULL DEFAULT '',
  audio_codec TEXT NOT NULL DEFAULT '',
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  bitrate INTEGER NOT NULL DEFAULT 0,
  hdr TEXT NOT NULL DEFAULT '',
  credits_start REAL,
  tmdb_id INTEGER,
  has_poster INTEGER NOT NULL DEFAULT 0,
  has_backdrop INTEGER NOT NULL DEFAULT 0,
  missing INTEGER NOT NULL DEFAULT 0,
  sidecar_sig TEXT NOT NULL DEFAULT '',
  added_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(library_id, key)
);
CREATE INDEX items_lib_kind_sort ON items(library_id, kind, sort_title);
CREATE INDEX items_parent ON items(parent_id);
CREATE INDEX items_series_ep ON items(series_id, season_num, episode_num);
CREATE INDEX items_added ON items(added_at DESC);
CREATE INDEX items_path ON items(path);
CREATE TABLE media_streams(
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  idx INTEGER NOT NULL,
  type TEXT NOT NULL,
  type_idx INTEGER NOT NULL,
  codec TEXT NOT NULL DEFAULT '',
  lang TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  channels INTEGER NOT NULL DEFAULT 0,
  is_default INTEGER NOT NULL DEFAULT 0,
  is_forced INTEGER NOT NULL DEFAULT 0,
  external_path TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(item_id, type, type_idx)
);
CREATE TABLE chapters(
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  start REAL NOT NULL,
  title TEXT NOT NULL DEFAULT ''
);
CREATE INDEX chapters_item ON chapters(item_id);
CREATE TABLE item_genres(
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  genre TEXT NOT NULL COLLATE NOCASE,
  PRIMARY KEY(item_id, genre)
);
CREATE INDEX item_genres_genre ON item_genres(genre);
CREATE TABLE item_tags(
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  tag TEXT NOT NULL COLLATE NOCASE,
  PRIMARY KEY(item_id, tag)
);
CREATE INDEX item_tags_tag ON item_tags(tag);
CREATE TABLE collections(
  id INTEGER PRIMARY KEY,
  profile_id INTEGER REFERENCES profiles(id) ON DELETE CASCADE,
  owner_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('manual','smart')),
  rules TEXT NOT NULL DEFAULT '',
  pinned INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE collection_items(
  collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  position INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(collection_id, item_id)
);
CREATE INDEX collection_items_item ON collection_items(item_id);
CREATE TABLE user_data(
  profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  position REAL NOT NULL DEFAULT 0,
  played INTEGER NOT NULL DEFAULT 0,
  play_count INTEGER NOT NULL DEFAULT 0,
  last_played_at INTEGER,
  favorite INTEGER NOT NULL DEFAULT 0,
  stars INTEGER CHECK(stars BETWEEN 1 AND 5),
  hidden INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(profile_id, item_id)
);
CREATE INDEX user_data_item ON user_data(item_id);
CREATE INDEX user_data_last ON user_data(profile_id, last_played_at DESC);
CREATE TABLE settings(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`,
	// 2: device registry on sessions (native apps, TVs), profile lock for Quick Connect kids devices
	`
ALTER TABLE sessions ADD COLUMN device_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN device_name TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN platform TEXT NOT NULL DEFAULT 'web';
ALTER TABLE sessions ADD COLUMN app_version TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN last_seen INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN profile_lock INTEGER REFERENCES profiles(id) ON DELETE CASCADE;
UPDATE sessions SET device_id=lower(hex(randomblob(8))) WHERE device_id='';
CREATE UNIQUE INDEX sessions_device ON sessions(device_id);
CREATE INDEX sessions_user ON sessions(user_id);
`,
	// 3: skip intro / credits segments, analysis bookkeeping, per-profile skip mode
	`
CREATE TABLE item_segments(
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  type TEXT NOT NULL CHECK(type IN ('intro','recap','credits','preview')),
  start REAL NOT NULL,
  end REAL NOT NULL,
  source TEXT NOT NULL CHECK(source IN ('manual','chapter','auto')),
  confidence REAL NOT NULL DEFAULT 1,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(item_id, type, source)
);
CREATE TABLE segment_scans(
  item_id INTEGER PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  file_sig TEXT NOT NULL,
  scanned_at INTEGER NOT NULL,
  error TEXT NOT NULL DEFAULT ''
);
ALTER TABLE profiles ADD COLUMN skip_intro TEXT NOT NULL DEFAULT 'manual';
`,
	// 4: online metadata in the viewers' languages (the items table holds the metadata_lang one)
	`
CREATE TABLE item_i18n(
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  lang TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  sort_title TEXT NOT NULL DEFAULT '',
  overview TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(item_id, lang)
);
CREATE TABLE genre_i18n(
  lang TEXT NOT NULL,
  genre TEXT NOT NULL,
  label TEXT NOT NULL,
  PRIMARY KEY(lang, genre)
);
`,
	// 5: versions of a film grouped by IMDb id, the profile's chosen version, downloaded subtitles
	`
ALTER TABLE items ADD COLUMN imdb_id TEXT NOT NULL DEFAULT '';
CREATE INDEX items_imdb ON items(imdb_id) WHERE imdb_id<>'';
CREATE TABLE version_prefs(
  profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  imdb_id TEXT NOT NULL,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(profile_id, imdb_id)
);
CREATE TABLE item_subtitles(
  id INTEGER PRIMARY KEY,
  item_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  lang TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL,
  provider TEXT NOT NULL,
  file_id TEXT NOT NULL,
  hearing_impaired INTEGER NOT NULL DEFAULT 0,
  forced INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  UNIQUE(item_id, provider, file_id)
);
`,
	// 6: built-in acquisition (internal/acquire): indexers, download clients, downloads
	`
CREATE TABLE acq_indexers(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'torznab',
  url TEXT NOT NULL,
  api_key TEXT NOT NULL DEFAULT '',
  categories TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  priority INTEGER NOT NULL DEFAULT 25,
  last_ok_at INTEGER,
  last_error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TABLE acq_clients(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'qbittorrent',
  url TEXT NOT NULL,
  username TEXT NOT NULL DEFAULT '',
  password TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT 'kinora',
  save_path TEXT NOT NULL DEFAULT '',
  path_remote TEXT NOT NULL DEFAULT '',
  path_local TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);
CREATE TABLE acq_grabs(
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL DEFAULT 'movie',
  tmdb_id INTEGER NOT NULL DEFAULT 0,
  imdb_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  year INTEGER NOT NULL DEFAULT 0,
  library_id INTEGER REFERENCES libraries(id) ON DELETE SET NULL,
  release_title TEXT NOT NULL,
  indexer_id INTEGER REFERENCES acq_indexers(id) ON DELETE SET NULL,
  indexer_name TEXT NOT NULL DEFAULT '',
  client_id INTEGER REFERENCES acq_clients(id) ON DELETE SET NULL,
  info_hash TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  quality TEXT NOT NULL DEFAULT '',
  score INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'downloading',
  progress REAL NOT NULL DEFAULT 0,
  eta INTEGER NOT NULL DEFAULT -1,
  speed INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  target_path TEXT NOT NULL DEFAULT '',
  requested_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX acq_grabs_status ON acq_grabs(status);
CREATE INDEX acq_grabs_tmdb ON acq_grabs(kind, tmdb_id);
`,
	// 7: acquisition requests (anyone may ask for a film), the blocklist of failed releases
	`
CREATE TABLE acq_requests(
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL DEFAULT 'movie',
  tmdb_id INTEGER NOT NULL,
  imdb_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  year INTEGER NOT NULL DEFAULT 0,
  poster TEXT NOT NULL DEFAULT '',
  user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
  profile_id INTEGER REFERENCES profiles(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  note TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  next_try_at INTEGER NOT NULL DEFAULT 0,
  decided_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  seen INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX acq_requests_status ON acq_requests(status);
CREATE INDEX acq_requests_user ON acq_requests(user_id, created_at);
CREATE INDEX acq_requests_tmdb ON acq_requests(kind, tmdb_id);
ALTER TABLE acq_grabs ADD COLUMN request_id INTEGER REFERENCES acq_requests(id) ON DELETE SET NULL;
CREATE TABLE acq_blocklist(
  info_hash TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
`,
	// 8: acquisition of series: monitored series, their episodes, season / episode downloads, season requests
	`
CREATE TABLE acq_series(
  id INTEGER PRIMARY KEY,
  tmdb_id INTEGER NOT NULL UNIQUE,
  imdb_id TEXT NOT NULL DEFAULT '',
  tvdb_id INTEGER NOT NULL DEFAULT 0,
  title TEXT NOT NULL,
  original_title TEXT NOT NULL DEFAULT '',
  english_title TEXT NOT NULL DEFAULT '',
  year INTEGER NOT NULL DEFAULT 0,
  poster TEXT NOT NULL DEFAULT '',
  library_id INTEGER REFERENCES libraries(id) ON DELETE SET NULL,
  monitor TEXT NOT NULL DEFAULT 'all',
  ended INTEGER NOT NULL DEFAULT 0,
  refreshed_at INTEGER NOT NULL DEFAULT 0,
  added_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE acq_episodes(
  series_id INTEGER NOT NULL REFERENCES acq_series(id) ON DELETE CASCADE,
  season INTEGER NOT NULL,
  episode INTEGER NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  air_date TEXT NOT NULL DEFAULT '',
  monitored INTEGER NOT NULL DEFAULT 1,
  have INTEGER NOT NULL DEFAULT 0,
  grab_id INTEGER REFERENCES acq_grabs(id) ON DELETE SET NULL,
  searched_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(series_id, season, episode)
);
ALTER TABLE acq_grabs ADD COLUMN series_id INTEGER REFERENCES acq_series(id) ON DELETE SET NULL;
ALTER TABLE acq_grabs ADD COLUMN season INTEGER NOT NULL DEFAULT -1;
ALTER TABLE acq_grabs ADD COLUMN episodes TEXT NOT NULL DEFAULT '';
ALTER TABLE acq_requests ADD COLUMN seasons TEXT NOT NULL DEFAULT '';
`,
	// 9: download clients that name downloads themselves (Usenet), quality profiles, upgrades
	`
ALTER TABLE acq_grabs ADD COLUMN download_id TEXT NOT NULL DEFAULT '';
UPDATE acq_grabs SET download_id=info_hash;
CREATE TABLE acq_profiles(
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  json TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
ALTER TABLE acq_series ADD COLUMN profile_id INTEGER REFERENCES acq_profiles(id) ON DELETE SET NULL;
ALTER TABLE acq_grabs ADD COLUMN profile_id INTEGER REFERENCES acq_profiles(id) ON DELETE SET NULL;
ALTER TABLE acq_grabs ADD COLUMN replaces TEXT NOT NULL DEFAULT '';
ALTER TABLE acq_grabs ADD COLUMN upgrade_checked_at INTEGER NOT NULL DEFAULT 0;
`,
	// 10: the built-in indexer engine: uploaded definitions (Cardigann YAML), indexers that run one
	`
CREATE TABLE acq_definitions(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL DEFAULT '',
  yaml TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
ALTER TABLE acq_indexers ADD COLUMN definition_id TEXT NOT NULL DEFAULT '';
ALTER TABLE acq_indexers ADD COLUMN config TEXT NOT NULL DEFAULT '{}';
`,
	// 11: a request remembers the library its requester chose
	`
ALTER TABLE acq_requests ADD COLUMN library_id INTEGER REFERENCES libraries(id) ON DELETE SET NULL;
`,
	// 12: series downloads can go to their own client category and folder
	`
ALTER TABLE acq_clients ADD COLUMN category_tv TEXT NOT NULL DEFAULT '';
ALTER TABLE acq_clients ADD COLUMN save_path_tv TEXT NOT NULL DEFAULT '';
`,
	// 13: a profile's audio and subtitle language for one film or series (episodes share their series')
	`
CREATE TABLE title_langs(
  profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  title_id INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  audio_lang TEXT NOT NULL DEFAULT '',
  sub_lang TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(profile_id, title_id)
);
`,
	// 14: what a profile has watched, by TMDB id, so it survives the file and the library
	// (the Felfedezés page shows it); movies are season 0 episode 0
	`
CREATE TABLE watch_history(
  profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK(kind IN ('movie','series')),
  tmdb_id INTEGER NOT NULL,
  season INTEGER NOT NULL DEFAULT 0,
  episode INTEGER NOT NULL DEFAULT 0,
  watched_at INTEGER NOT NULL,
  PRIMARY KEY(profile_id, kind, tmdb_id, season, episode)
);
INSERT OR IGNORE INTO watch_history(profile_id,kind,tmdb_id,season,episode,watched_at)
  SELECT u.profile_id, 'movie', i.tmdb_id, 0, 0, COALESCE(u.last_played_at, i.updated_at)
  FROM user_data u JOIN items i ON i.id=u.item_id
  WHERE u.played=1 AND i.kind='movie' AND i.tmdb_id>0;
INSERT OR IGNORE INTO watch_history(profile_id,kind,tmdb_id,season,episode,watched_at)
  SELECT u.profile_id, 'series', s.tmdb_id, e.season_num, e.episode_num, COALESCE(u.last_played_at, e.updated_at)
  FROM user_data u JOIN items e ON e.id=u.item_id JOIN items s ON s.id=e.series_id
  WHERE u.played=1 AND e.kind='episode' AND s.tmdb_id>0 AND e.season_num IS NOT NULL AND e.episode_num IS NOT NULL;
`,
	// 15: watch party chat lines a member reported, for the admin to review
	`
CREATE TABLE party_reports(
  id INTEGER PRIMARY KEY,
  room TEXT NOT NULL,
  reporter_user INTEGER REFERENCES users(id) ON DELETE SET NULL,
  reporter_name TEXT NOT NULL,
  sender_user INTEGER REFERENCES users(id) ON DELETE SET NULL,
  sender_name TEXT NOT NULL,
  text TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
`,
	// 16: libraries an account may see ('' = all, else comma-separated library ids)
	`ALTER TABLE users ADD COLUMN libraries TEXT NOT NULL DEFAULT '';`,
	// 17: iOS push through the relay (one row per logged-in device), and which fulfilled
	// requests were already pushed (existing ones count as pushed: no flood after the upgrade)
	`
CREATE TABLE push_ios(
  device_id TEXT PRIMARY KEY REFERENCES sessions(device_id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  handle TEXT NOT NULL,
  key BLOB NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX push_ios_user ON push_ios(user_id);
ALTER TABLE acq_requests ADD COLUMN pushed INTEGER NOT NULL DEFAULT 0;
UPDATE acq_requests SET pushed=1;
`,
	// 18: push notifications for every request change (an outbox filled by triggers, so no code path that
	// changes a status can forget it), per-device language and categories, and live download activities
	`
CREATE TABLE push_outbox(
  id INTEGER PRIMARY KEY,
  request_id INTEGER NOT NULL,
  status TEXT NOT NULL,
  old_status TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TRIGGER push_request_status AFTER UPDATE OF status ON acq_requests
WHEN NEW.status <> OLD.status AND (NEW.status IN ('denied','failed','available') OR (NEW.status='approved' AND OLD.status='pending'))
BEGIN
  INSERT INTO push_outbox(request_id,status,old_status,created_at) VALUES(NEW.id,NEW.status,OLD.status,CAST(strftime('%s','now') AS INTEGER));
END;
CREATE TRIGGER push_request_new AFTER INSERT ON acq_requests WHEN NEW.status='pending'
BEGIN
  INSERT INTO push_outbox(request_id,status,created_at) VALUES(NEW.id,'pending',CAST(strftime('%s','now') AS INTEGER));
END;
ALTER TABLE push_ios ADD COLUMN lang TEXT NOT NULL DEFAULT '';
ALTER TABLE push_ios ADD COLUMN meta_lang TEXT NOT NULL DEFAULT '';
ALTER TABLE push_ios ADD COLUMN muted TEXT NOT NULL DEFAULT '';
ALTER TABLE push_ios ADD COLUMN live INTEGER NOT NULL DEFAULT 0;
CREATE TABLE push_live(
  device_id TEXT NOT NULL REFERENCES push_ios(device_id) ON DELETE CASCADE,
  request_id INTEGER NOT NULL,
  started_at INTEGER NOT NULL,
  sent_at INTEGER NOT NULL,
  progress REAL NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT '',
  ended INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(device_id, request_id)
);
INSERT INTO settings(key,value) VALUES('push_episode_id', COALESCE((SELECT MAX(id) FROM items),0))
  ON CONFLICT(key) DO UPDATE SET value=excluded.value;
`,
	// imports from other servers: which external user became which account, and one-time password links
	// for accounts created without a password
	`CREATE TABLE import_links(
  source TEXT NOT NULL,
  external_id TEXT NOT NULL,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(source, external_id)
);
CREATE TABLE password_links(
  code_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);`,
	// what an import from Sonarr / Radarr / Prowlarr created, so running it again updates instead of duplicating
	`CREATE TABLE import_map(
  source TEXT NOT NULL,
  kind TEXT NOT NULL,
  external_id TEXT NOT NULL,
  local_id INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(source, kind, external_id)
);`,
	// a library can span several folders: the extra ones as a JSON array next to the main path
	`ALTER TABLE libraries ADD COLUMN extra_paths TEXT NOT NULL DEFAULT '[]';`,
	// Re-probe files with embedded subtitles on the next scan to populate their disposition.
	`ALTER TABLE media_streams ADD COLUMN hearing_impaired INTEGER NOT NULL DEFAULT 0;
UPDATE items SET sidecar_sig='' WHERE id IN (SELECT item_id FROM media_streams WHERE type='subtitle' AND external_path='');`,
	// snowfall on the screens outside the player, per profile (on by default)
	`ALTER TABLE profiles ADD COLUMN snow INTEGER NOT NULL DEFAULT 1;`,
}

// ErrNewerSchema: the database was written by a newer Kinora (after rolling back an update, for example).
// Running against a schema this binary does not know can fail later or write rows the newer version misreads.
var ErrNewerSchema = errors.New("az adatbázist egy újabb Kinora-verzió hozta létre")

// AllowNewerSchema opens such a database anyway (KINORA_ALLOW_NEWER_DB=1).
var AllowNewerSchema bool

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var v int
	err := s.DB.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.DB.Exec(`INSERT INTO schema_version(version) VALUES(0)`); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if v > len(migrations) && !AllowNewerSchema {
		return fmt.Errorf("%w (az adatbázis sémája %d, ez a verzió %d-ig ismeri): frissítsd a Kinorát, vagy állíts vissza egy mentést; "+
			"saját felelősségre: KINORA_ALLOW_NEWER_DB=1", ErrNewerSchema, v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migráció %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`UPDATE schema_version SET version=?`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the applied migration count.
func (s *Store) SchemaVersion() int {
	var v int
	s.DB.QueryRow(`SELECT version FROM schema_version`).Scan(&v)
	return v
}

// ---------------------------------------------------------------- settings

// SettingDefaults are used when a key is not stored.
var SettingDefaults = map[string]string{
	"server_name":          "Kinora",
	"tmdb_key":             "",
	"metadata_lang":        "hu-HU",
	"encoder":              "auto",
	"video_codec":          "auto",
	"public_url":           "",
	"scan_interval_min":    "15",
	"quickconnect_enabled": "1",
	"discovery_enabled":    "1",
	"brand_accent":         "",
	"intro_detection":      "1",
	"intro_window_min":     "10",
	"opensubtitles_key":    "",
	"opensubtitles_user":   "",
	"opensubtitles_pass":   "",
	"acq_enabled":          "0",
	"acq_profile":          "",
	"acq_import_mode":      "hardlink",
	"acq_auto_approve":     "admins",
	"acq_quota_week":       "0",
	"acq_library":          "",
	"acq_retry_hours":      "6",
	"acq_library_tv":       "",
	"push_enabled":         "0",
	"push_relay_url":       "https://kinorarelay.projectone.dev",
}

// SetOverride forces a setting from the environment (shown as locked in the admin UI).
func (s *Store) SetOverride(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[key] = value
}

// Overridden reports whether key is forced by an environment variable.
func (s *Store) Overridden(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.overrides[key]
	return ok
}

// Setting returns the effective value: env override > stored > default.
func (s *Store) Setting(key string) string {
	s.mu.RLock()
	if v, ok := s.overrides[key]; ok {
		s.mu.RUnlock()
		return v
	}
	s.mu.RUnlock()
	var v string
	if err := s.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return SettingDefaults[key]
	}
	return v
}

// SettingInt returns an integer setting, falling back to def.
func (s *Store) SettingInt(key string, def int) int {
	n, err := strconv.Atoi(s.Setting(key))
	if err != nil {
		return def
	}
	return n
}

// SetSetting stores a value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ---------------------------------------------------------------- backup

// Backup writes a consistent copy of the live database to dst using VACUUM INTO.
func (s *Store) Backup(ctx context.Context, dst string) error {
	os.Remove(dst)
	_, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, dst)
	return err
}
