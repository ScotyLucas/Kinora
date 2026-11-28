package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ScotyLucas/kinora/internal/media"
	"github.com/ScotyLucas/kinora/internal/store"
)

func splitLangs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// playableItem loads a visible movie/episode with streams, or writes an error.
func (s *Server) playableItem(w http.ResponseWriter, r *http.Request, c *reqCtx) *store.Item {
	if !c.tokenAllows(pathID(r, "id")) {
		writeErr(w, http.StatusForbidden, "a lejátszási token másik elemhez tartozik")
		return nil
	}
	it, err := s.st.ItemFull(pathID(r, "id"), c.Profile)
	if err != nil {
		writeErr(w, http.StatusNotFound, "az elem nem található")
		return nil
	}
	if it.Kind != "movie" && it.Kind != "episode" {
		writeErr(w, http.StatusBadRequest, "ez az elem nem játszható le közvetlenül (sorozat vagy évad)")
		return nil
	}
	if it.Missing {
		writeErr(w, http.StatusConflict, "a fájl jelenleg nem érhető el (hiányzik vagy a megosztás nincs csatolva)")
		return nil
	}
	return it
}

func (s *Server) encoderSetting() string { return s.st.Setting("encoder") }

func (s *Server) handlePlayback(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var req media.PlaybackRequest
	if !decode(w, r, &req) {
		return
	}
	it := s.playableItem(w, r, c)
	if it == nil {
		return
	}
	if req.PrevSession != "" {
		if old := s.Media.Get(req.PrevSession, c.User.ID); old != nil {
			s.Media.Stop(old.ID)
		}
	}
	if req.Start < 0 || (it.RuntimeSec > 0 && req.Start >= float64(it.RuntimeSec)) {
		req.Start = 0
	}
	maxK := c.Profile.MaxBitrate
	if req.MaxBitrate > 0 && (maxK == 0 || req.MaxBitrate < maxK) {
		maxK = req.MaxBitrate
	}
	// the profile's languages, with this film's or series' own choice first
	tl := s.st.TitleLangsFor(c.Profile.ID, store.TitleOf(it))
	d := media.Decide(it, req, media.WithTitleLang(tl.AudioLang, splitLangs(c.Profile.AudioLangs)), maxK, s.Encoders.ToneMap())
	tok := s.mintStreamToken(c, it.ID)
	subs := []map[string]any{}
	for _, st := range it.Streams {
		if st.Type == "subtitle" {
			subs = append(subs, map[string]any{"idx": st.TypeIndex, "lang": st.Lang, "title": st.Title, "codec": st.Codec,
				"forced": st.Forced, "text_based": st.TextBased, "url": withStreamToken(fmt.Sprintf("/api/items/%d/subtitles/%d", it.ID, st.TypeIndex), tok)})
		}
	}
	resp := map[string]any{
		"stream_token":  tok,
		"subtitles":     subs,
		"segments":      s.effectiveSegments(it),
		"mode":          d.Mode,
		"reasons":       d.Reasons,
		"audio_idx":     d.AudioIdx,
		"sub_idx":       media.ChooseSubtitle(it.Streams, d.AudioIdx, tl.SubLang, splitLangs(c.Profile.SubLangs), c.Profile.SubMode),
		"title_langs":   tl,
		"streams":       it.Streams,
		"chapters":      it.Chapters,
		"duration":      it.RuntimeSec,
		"credits_start": it.CreditsStart,
		"start_offset":  0.0,
		"item":          map[string]any{"id": it.ID, "title": it.Title, "series_title": it.SeriesTitle, "season_num": it.SeasonNum, "episode_num": it.EpisodeNum, "kind": it.Kind, "series_id": it.SeriesID},
		"source": map[string]any{"container": it.Container, "video_codec": it.VideoCodec, "audio_codec": it.AudioCodec,
			"width": it.Width, "height": it.Height, "bitrate": it.Bitrate, "hdr": it.HDR},
	}
	if d.Mode == "direct" {
		resp["url"] = withStreamToken(fmt.Sprintf("/api/stream/%d", it.ID), tok)
		resp["encoder"] = ""
		writeJSON(w, http.StatusOK, resp)
		return
	}
	enc := s.Encoders.PickFor(s.encoderSetting(), s.st.Setting("video_codec"), req.Caps)
	sess, err := s.Media.Start(r.Context(), media.HLSSpec{Item: it, UserID: c.User.ID, Decision: d, Start: req.Start, Encoder: enc})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, media.ErrBusy) {
			status = http.StatusServiceUnavailable
		}
		s.log.Warn("playback failed", "item", it.Title, "mode", d.Mode, "err", err)
		writeJSON(w, status, map[string]any{"error": err.Error(), "reasons": d.Reasons, "mode": d.Mode})
		return
	}
	resp["url"] = withStreamToken("/api/hls/"+sess.ID+"/index.m3u8", tok)
	resp["session"] = sess.ID
	resp["start_offset"] = req.Start
	if d.VideoCopy {
		resp["encoder"] = "copy"
		resp["encoder_label"] = "nincs videó-átkódolás (copy)"
	} else {
		if real, ok := media.EncoderByName(sess.Encoder); ok {
			enc = real // the session may have moved to another GPU family
		}
		resp["encoder"] = enc.Name
		resp["encoder_label"] = enc.Label
		resp["decode"] = sess.Decode
		resp["bitrate"] = d.Bitrate
		resp["width"] = d.Width
	}
	writeJSON(w, http.StatusOK, resp)
}

var contentTypes = map[string]string{
	".mkv": "video/x-matroska", ".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime",
	".avi": "video/x-msvideo", ".ts": "video/mp2t", ".m2ts": "video/mp2t", ".wmv": "video/x-ms-wmv", ".flv": "video/x-flv",
	".mpg": "video/mpeg", ".mpeg": "video/mpeg", ".ogv": "video/ogg",
}

// contentDisposition builds an RFC 6266 header with an UTF-8 filename.
func contentDisposition(kind, name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, kind, ascii, rfc5987(name))
}

// rfc5987 percent-encodes every byte outside RFC 5987's attr-char. url.PathEscape is not
// enough: it leaves ; , ( ) = : @ * ' in place, and a ";" ends the header parameter early.
func rfc5987(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	it := s.playableItem(w, r, c)
	if it == nil {
		return
	}
	f, err := os.Open(it.Path)
	if err != nil {
		s.log.Warn("stream: file not readable", "item", it.ID, "err", err)
		writeErr(w, http.StatusConflict, "a fájl nem olvasható (részletek a szerver naplójában)")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.internalErr(w, r, err)
		return
	}
	ext := strings.ToLower(filepath.Ext(it.Path))
	if ct := contentTypes[ext]; ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition", contentDisposition("attachment", downloadName(it, "")+ext))
	}
	w.Header().Set("Cache-Control", "private, no-transform")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func downloadName(it *store.Item, quality string) string {
	name := it.Title
	if it.Kind == "episode" && it.SeriesTitle != "" {
		se := ""
		if it.SeasonNum != nil && it.EpisodeNum != nil {
			se = fmt.Sprintf(" S%02dE%02d", *it.SeasonNum, *it.EpisodeNum)
		}
		name = it.SeriesTitle + se + " - " + it.Title
	} else if it.Year != nil {
		name = fmt.Sprintf("%s (%d)", it.Title, *it.Year)
	}
	if quality != "" {
		name += " [" + quality + "]"
	}
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
}

var reHLSFile = regexp.MustCompile(`^(index\.m3u8|init\.mp4|seg\d{5,}\.(ts|m4s))$`)

func (s *Server) handleHLS(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	file := r.PathValue("file")
	if strings.ContainsAny(file, `/\`) || strings.HasPrefix(file, ".") || !reHLSFile.MatchString(file) {
		writeErr(w, http.StatusBadRequest, "érvénytelen fájlnév")
		return
	}
	sess := s.Media.Get(r.PathValue("session"), c.User.ID)
	if sess == nil || !c.tokenAllows(sess.ItemID) {
		writeErr(w, http.StatusNotFound, "a lejátszási munkamenet lejárt – indítsd újra a lejátszást")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	if file == "index.m3u8" {
		b, err := sess.Playlist()
		if err != nil {
			writeErr(w, http.StatusNotFound, "a playlist még nem áll rendelkezésre")
			return
		}
		if st := r.URL.Query().Get("st"); st != "" {
			b = rewritePlaylist(b, st)
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Write(b)
		return
	}
	p := sess.SegmentPath(file)
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "a szegmens még nem készült el")
		return
	}
	if strings.HasSuffix(file, ".ts") {
		w.Header().Set("Content-Type", "video/mp2t")
	} else {
		w.Header().Set("Content-Type", "video/mp4")
	}
	http.ServeFile(w, r, p)
}

func (s *Server) handleHLSStop(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	if sess := s.Media.Get(r.PathValue("session"), c.User.ID); sess != nil {
		s.Media.Stop(sess.ID)
	}
	ok(w)
}

func (s *Server) handleSubtitle(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	it := s.playableItem(w, r, c)
	if it == nil {
		return
	}
	idx := int(pathID(r, "idx"))
	for _, st := range it.Streams {
		if st.Type == "subtitle" && st.TypeIndex == idx {
			p, err := s.Subs.VTT(r.Context(), it, st)
			if err != nil {
				writeErr(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
			w.Header().Set("Cache-Control", "private, max-age=3600")
			http.ServeFile(w, r, p)
			return
		}
	}
	writeErr(w, http.StatusNotFound, "nincs ilyen felirat")
}

// ------------------------------------------------------------------ downloads

func (s *Server) handleDownloadCreate(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	var in struct {
		Quality  string `json:"quality"`
		AudioIdx *int   `json:"audio_idx"`
	}
	if !decode(w, r, &in) {
		return
	}
	it := s.playableItem(w, r, c)
	if it == nil {
		return
	}
	if in.Quality == "original" || in.Quality == "" {
		// the URL carries a stream token, so a download manager without the session can fetch it too
		writeJSON(w, http.StatusOK, map[string]any{"status": "done", "url": withStreamToken(fmt.Sprintf("/api/stream/%d?dl=1", it.ID), s.mintStreamToken(c, it.ID))})
		return
	}
	q, ok := media.QualityByName(in.Quality)
	if !ok {
		writeErr(w, http.StatusBadRequest, "ismeretlen minőség (1080p, 720p, 480p, 360p vagy original)")
		return
	}
	tl := s.st.TitleLangsFor(c.Profile.ID, store.TitleOf(it))
	audio := media.ChooseAudio(it.Streams, in.AudioIdx, media.WithTitleLang(tl.AudioLang, splitLangs(c.Profile.AudioLangs)))
	job, err := s.Downloads.Enqueue(it, c.User.ID, q, audio, downloadName(it, q.Name)+".mp4")
	if err != nil {
		writeErr(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

// downloadJob is a job as the client sees it: a finished one carries a file URL with a stream token of its title,
// for download managers that do not share the client's session (the Android app's).
type downloadJob struct {
	media.Job
	FileURL string `json:"file_url,omitempty"`
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	out := []downloadJob{}
	for _, j := range s.Downloads.Jobs(c.User.ID) {
		if !s.st.ItemVisible(j.ItemID, c.Profile) {
			continue
		}
		dj := downloadJob{Job: j}
		if j.Status == "done" {
			dj.FileURL = withStreamToken("/api/downloads/"+j.Key+"/file", s.mintStreamToken(c, j.ItemID))
		}
		out = append(out, dj)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out, "qualities": media.Qualities})
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	j, path, found := s.Downloads.Get(r.PathValue("key"))
	// a stream token opens only the downloads of its own title
	if !found || !j.HasUser(c.User.ID) || !s.st.ItemVisible(j.ItemID, c.Profile) || !c.tokenAllows(j.ItemID) {
		writeErr(w, http.StatusNotFound, "a letöltés nem található")
		return
	}
	if j.Status != "done" {
		writeErr(w, http.StatusConflict, "a letöltés még nem készült el")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusGone, "a letöltött fájl már nem érhető el")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", j.FileName))
	http.ServeContent(w, r, "", time.Unix(j.DoneAt, 0), f)
}

// ------------------------------------------------------------------ encoders

func (s *Server) handleHWTest(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	res := s.Encoders.SelfTest(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"results": res, "selected": s.Encoders.Pick(s.encoderSetting()), "tonemap": s.Encoders.ToneMap()})
}
