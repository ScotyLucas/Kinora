package push

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ScotyLucas/kinora/internal/store"
)

// maxPlain keeps the encrypted payload under the relay's 2048-byte limit.
const maxPlain = 1500

// Notification categories a device can turn off (PushPrefs.Muted).
const (
	CatRequests      = "requests"       // the device's own requests: approved, denied, failed, arrived
	CatEpisodes      = "episodes"       // new episodes of series the profile follows
	CatAdminRequests = "admin_requests" // admins: someone asked for something
	CatRecap         = "recap"          // the Napló's monthly recap is ready (early in the month)
)

// Categories lists every category, in the order the app shows them.
var Categories = []string{CatRequests, CatEpisodes, CatRecap, CatAdminRequests}

// Message is the plaintext the iOS Notification Service Extension decrypts. Title and body are
// already in the device's language.
type Message struct {
	V     int    `json:"v"`
	Type  string `json:"type"` // request_approved | request_denied | request_failed | request_available | request_new | episodes | recap | test
	Title string `json:"title"`
	Body  string `json:"body"`
	Item  int64  `json:"item,omitempty"`  // library item to open
	Href  string `json:"href,omitempty"`  // the same in the web client
	Image string `json:"image,omitempty"` // path on this server, fetched with the app's session
	Tag   string `json:"tag,omitempty"`
}

// LiveState is the sealed content of a live download activity.
type LiveState struct {
	V        int     `json:"v"`
	Title    string  `json:"t"`
	Kind     string  `json:"k"`             // movie | series
	State    string  `json:"st"`            // downloading | importing | available | failed | canceled
	Progress float64 `json:"p"`             // 0..1
	ETA      int64   `json:"eta,omitempty"` // seconds from At, 0 = unknown
	At       int64   `json:"at"`            // when this state was measured
	Item     int64   `json:"item,omitempty"`
}

// Translator returns the texts of one language: Hungarian source text in, translation out, with
// {0}, {1}… filled from args.
type Translator func(lang string) func(hu string, args ...any) string

// Notifier pushes request changes, new episodes and live downloads to paired iOS devices (through the
// relay) and keeps them for paired Android devices, which fetch them.
type Notifier struct {
	st       *store.Store
	Relay    *Relay
	log      *slog.Logger
	Interval time.Duration
	Text     Translator // nil = Hungarian
	now      func() time.Time
}

// NewNotifier makes the notifier; Run drives it.
func NewNotifier(st *store.Store, relay *Relay, log *slog.Logger) *Notifier {
	return &Notifier{st: st, Relay: relay, log: log, Interval: 30 * time.Second, now: time.Now}
}

// SetClock replaces the notifier's clock (tests).
func (n *Notifier) SetClock(now func() time.Time) { n.now = now }

// Run sends pending notifications until ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	t := time.NewTicker(n.Interval)
	defer t.Stop()
	for {
		n.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Format fills {0}, {1}… in s.
func Format(s string, args ...any) string {
	for i, a := range args {
		s = strings.ReplaceAll(s, "{"+strconv.Itoa(i)+"}", fmt.Sprint(a))
	}
	return s
}

func (n *Notifier) text(lang string) func(string, ...any) string {
	if n.Text == nil || lang == "" || lang == "hu" {
		return Format
	}
	return n.Text(lang)
}

// Tick sends what is waiting now.
func (n *Notifier) Tick(ctx context.Context) {
	devs, err := n.st.PushIOSAll()
	if err != nil {
		n.log.Warn("push: devices", "err", err)
		return
	}
	if !n.Relay.Enabled() {
		// without the relay only the devices that fetch their notifications (Android) can be told
		devs = slices.DeleteFunc(devs, func(d store.PushIOS) bool { return !d.Inbox() })
	}
	if len(devs) == 0 {
		// nobody to tell: drop what piled up, so turning push on later does not flood
		n.st.ClearPushEvents()
		n.skipEpisodes()
		return
	}
	n.requests(ctx, devs)
	n.episodes(ctx, devs)
	n.recap(ctx, devs)
	n.live(ctx, devs)
}

// --- request changes ------------------------------------------------------------

// retryable is an error after which the rest is left for the next tick: the relay is unreachable or broken.
func retryable(err error) bool {
	if err == nil || errors.Is(err, ErrDisabled) {
		return false
	}
	var re *RelayError
	return !errors.As(err, &re) || re.Status >= 500
}

func (n *Notifier) requests(ctx context.Context, devs []store.PushIOS) {
	events, err := n.st.PushEvents(n.now().Add(-24*time.Hour).Unix(), 100)
	if err != nil {
		n.log.Warn("push: request events", "err", err)
		return
	}
	for _, e := range events {
		if ctx.Err() != nil {
			return
		}
		q, err := n.st.AcqRequest(e.RequestID)
		if err != nil || q.UserID == nil {
			n.st.DonePushEvent(e.ID)
			continue
		}
		var failed error
		for _, d := range devs {
			m, ok := n.requestMessage(e, q, d)
			if !ok {
				continue
			}
			if err := n.SendDevice(ctx, d, m); err != nil {
				n.log.Warn("push: request", "id", q.ID, "status", e.Status, "err", err)
				if retryable(err) {
					failed = err
					break
				}
			}
		}
		if failed != nil {
			return // retry this one and the rest on the next tick
		}
		n.st.DonePushEvent(e.ID)
	}
}

// requestMessage builds what device d hears about event e, if anything.
func (n *Notifier) requestMessage(e store.PushEvent, q *store.AcqRequest, d store.PushIOS) (Message, bool) {
	T := n.text(d.Lang)
	mine := d.UserID == *q.UserID
	m := Message{V: 1, Tag: fmt.Sprintf("req-%d", q.ID)}
	item := n.st.LibraryItemByTmdb(libKind(q.Kind), q.TmdbID)
	title := n.itemTitle(item, d, q.Title)
	switch {
	case e.Status == store.ReqPending:
		if mine || !d.IsAdmin || !d.Wants(CatAdminRequests) {
			return m, false
		}
		m.Type, m.Title, m.Body = "request_new", T("Új kérés"), T("{0} kérte: {1}", q.UserName, title)
		m.Href, m.Tag = "#/requests", fmt.Sprintf("req-new-%d", q.ID)
		return m, true
	case !mine || !d.Wants(CatRequests):
		return m, false
	}
	m.Title = title
	series := q.Kind == "series"
	switch e.Status {
	case store.ReqApproved:
		m.Type, m.Body = "request_approved", T("Jóváhagyták a kérésedet, hamarosan beszerzem.")
	case store.ReqDenied:
		m.Type, m.Body = "request_denied", T("Elutasították a kérésedet.")
		if note := strings.TrimSpace(q.Note); note != "" {
			m.Body = T("Elutasították a kérésedet: {0}", note)
		}
	case store.ReqFailed:
		m.Type = "request_failed"
		if series {
			m.Body = T("A kért sorozatot nem sikerült beszerezni.")
		} else {
			m.Body = T("A kért filmet nem sikerült beszerezni.")
		}
	case store.ReqAvailable:
		m.Type = "request_available"
		if series {
			m.Body = T("A kért sorozat megérkezett, már nézheted.")
		} else {
			m.Body = T("A kért film megérkezett, már nézheted.")
		}
	default:
		return m, false
	}
	if item > 0 {
		m.Item, m.Href = item, "#/item/"+strconv.FormatInt(item, 10)
		m.Image = fmt.Sprintf("/api/images/%d/poster", item)
	}
	return m, true
}

// libKind is the library item kind of a request kind.
func libKind(kind string) string {
	if kind == "series" {
		return "series"
	}
	return "movie"
}

// itemTitle is the title of a library item in the device's metadata language, or fallback.
func (n *Notifier) itemTitle(id int64, d store.PushIOS, fallback string) string {
	if id <= 0 {
		return fallback
	}
	it, err := n.st.Item(id, &store.Profile{ID: d.ProfileID, MetaLang: d.MetaLang})
	if err != nil || it == nil || it.Title == "" {
		return fallback
	}
	return it.Title
}

// --- new episodes -----------------------------------------------------------------

const (
	episodeCursor = "push_episode_id"
	episodeQuiet  = 2 * time.Minute // a season still being scanned arrives in one notification
	bulkSeries    = 20              // more series at once is a library import, not news
)

func (n *Notifier) skipEpisodes() {
	cur, _ := strconv.ParseInt(n.st.Setting(episodeCursor), 10, 64)
	if _, next, err := n.st.NewEpisodes(cur, n.now().Unix()); err == nil && next != cur {
		n.st.SetSetting(episodeCursor, strconv.FormatInt(next, 10))
	}
}

func (n *Notifier) episodes(ctx context.Context, devs []store.PushIOS) {
	cur, _ := strconv.ParseInt(n.st.Setting(episodeCursor), 10, 64)
	eps, next, err := n.st.NewEpisodes(cur, n.now().Add(-episodeQuiet).Unix())
	if err != nil {
		n.log.Warn("push: new episodes", "err", err)
		return
	}
	if next != cur {
		defer n.st.SetSetting(episodeCursor, strconv.FormatInt(next, 10))
	}
	bySeries := map[int64][]store.NewEpisode{}
	var order []int64
	for _, e := range eps {
		if bySeries[e.SeriesID] == nil {
			order = append(order, e.SeriesID)
		}
		bySeries[e.SeriesID] = append(bySeries[e.SeriesID], e)
	}
	if len(order) == 0 || len(order) > bulkSeries {
		return
	}
	profiles := map[int64]*store.Profile{}
	for _, sid := range order {
		followers, err := n.st.SeriesFollowers(sid)
		if err != nil || len(followers) == 0 {
			continue
		}
		follows := map[int64]bool{}
		for _, p := range followers {
			follows[p] = true
		}
		for _, d := range devs {
			if ctx.Err() != nil {
				return
			}
			if !follows[d.ProfileID] || !d.Wants(CatEpisodes) {
				continue
			}
			p, ok := profiles[d.ProfileID]
			if !ok {
				p, _ = n.st.Profile(d.ProfileID)
				profiles[d.ProfileID] = p
			}
			if p == nil {
				continue
			}
			view := *p
			view.MetaLang = d.MetaLang
			m, ok := n.episodeMessage(sid, bySeries[sid], &view, d.Lang)
			if !ok {
				continue
			}
			if err := n.SendDevice(ctx, d, m); err != nil {
				n.log.Warn("push: episodes", "series", sid, "err", err)
			}
		}
	}
}

func (n *Notifier) episodeMessage(seriesID int64, eps []store.NewEpisode, p *store.Profile, lang string) (Message, bool) {
	series, err := n.st.Item(seriesID, p) // nil when the profile may not see it
	if err != nil || series == nil {
		return Message{}, false
	}
	T := n.text(lang)
	m := Message{V: 1, Type: "episodes", Title: series.Title, Item: seriesID, Tag: fmt.Sprintf("series-%d", seriesID),
		Href: "#/item/" + strconv.FormatInt(seriesID, 10), Image: fmt.Sprintf("/api/images/%d/poster", seriesID)}
	if len(eps) > 1 {
		m.Body = T("{0} új rész", len(eps))
		return m, true
	}
	e := eps[0]
	label := fmt.Sprintf("S%02dE%02d", e.Season, e.Episode)
	if ep, err := n.st.Item(e.ID, p); err == nil && ep != nil && ep.Title != "" {
		label += " · " + ep.Title
		m.Item, m.Href = e.ID, "#/item/"+strconv.FormatInt(e.ID, 10)
	}
	m.Body = T("Új rész: {0}", label)
	return m, true
}

// --- monthly and yearly recap ------------------------------------------------------

const (
	recapLastDay     = 7                       // the home page shows last month's recap card until this day
	recapYearLastDay = 15                      // last year's recap can be opened until January 15
	recapHour        = 10                      // not before this hour of the server's day
	recapSent        = "push_recap_sent."      // + profile id: the last month (YYYY-MM) told about
	recapYearSent    = "push_recap_year_sent." // + profile id: the last year (YYYY) told about
)

// recapMonthsIn are the Hungarian "in <month>" forms, the source texts of the translations.
var recapMonthsIn = [12]string{"januárban", "februárban", "márciusban", "áprilisban", "májusban", "júniusban",
	"júliusban", "augusztusban", "szeptemberben", "októberben", "novemberben", "decemberben"}

// RecapYearOpen reports whether last year's recap can be opened on day t: January 1–15.
func RecapYearOpen(t time.Time) bool { return t.Month() == time.January && t.Day() <= recapYearLastDay }

// recap tells each profile once that last month's recap is ready (days 1–7), or in January that last
// year's is (days 1–15, instead of December's), if it watched anything in that period.
func (n *Notifier) recap(ctx context.Context, devs []store.PushIOS) {
	now := n.now()
	if now.Hour() < recapHour {
		return
	}
	yearly := RecapYearOpen(now)
	if !yearly && now.Day() > recapLastDay {
		return
	}
	from := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, now.Location())
	to := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	ym, keyBase := from.Format("2006-01"), recapSent
	if yearly {
		from = time.Date(now.Year()-1, time.January, 1, 0, 0, 0, 0, now.Location())
		ym, keyBase = from.Format("2006"), recapYearSent
	}
	byProfile := map[int64][]store.PushIOS{}
	var order []int64
	for _, d := range devs {
		if d.ProfileID == 0 || !d.Wants(CatRecap) {
			continue
		}
		if byProfile[d.ProfileID] == nil {
			order = append(order, d.ProfileID)
		}
		byProfile[d.ProfileID] = append(byProfile[d.ProfileID], d)
	}
	for _, pid := range order {
		if ctx.Err() != nil {
			return
		}
		key := keyBase + strconv.FormatInt(pid, 10)
		if n.st.Setting(key) >= ym {
			continue
		}
		p, err := n.st.Profile(pid)
		if err != nil || p == nil {
			continue
		}
		cnt, err := n.st.JournalCount(pid, from.Unix(), to.Unix())
		if err != nil {
			n.log.Warn("push: recap", "profile", pid, "err", err)
			continue
		}
		done := func() {
			n.st.SetSetting(key, ym)
			if yearly { // December is in the yearly one
				n.st.SetSetting(recapSent+strconv.FormatInt(pid, 10), to.AddDate(0, -1, 0).Format("2006-01"))
			}
		}
		if cnt == 0 || (!yearly && p.JournalRecapSeen >= ym) {
			done()
			continue
		}
		var failed bool
		for _, d := range byProfile[pid] {
			if err := n.SendDevice(ctx, d, n.recapMessage(from, yearly, cnt, d.Lang)); err != nil {
				n.log.Warn("push: recap", "profile", pid, "err", err)
				failed = failed || retryable(err)
			}
		}
		if failed {
			return // the relay is down: try again on the next tick
		}
		done()
	}
}

func (n *Notifier) recapMessage(from time.Time, yearly bool, count int, lang string) Message {
	T := n.text(lang)
	if yearly {
		y := from.Year()
		return Message{V: 1, Type: "recap_year", Tag: fmt.Sprintf("recap-%d", y),
			Title: T("Elkészült az éves összefoglalód: {0}", y),
			Body:  T("{0} nézés egy év alatt. Január 15-ig nézheted meg, milyen éved volt.", count),
			Href:  "#/journal?p=last&recap=1"}
	}
	per := "year"
	if from.Month() == time.December {
		per = "last"
	}
	return Message{V: 1, Type: "recap", Tag: "recap-" + from.Format("2006-01"),
		Title: T("Elkészült a havi összesítőd"),
		Body:  T("Nézd meg, mit és mennyit néztél {0}: {1} nézés a naplódban.", T(recapMonthsIn[from.Month()-1]), count),
		Href:  fmt.Sprintf("#/journal?p=%s&b=%d", per, int(from.Month())-1)}
}

// --- live downloads ---------------------------------------------------------------

const (
	liveEvery     = 90 * time.Second // at most this often while the progress moves
	liveKeepAlive = 10 * time.Minute // a state at least this often (it goes stale after liveStale)
	liveStale     = 20 * time.Minute // the activity shows itself as outdated after this
	liveNoToken   = 10 * time.Minute // a started activity whose token never came was dismissed
	liveLinger    = 30 * time.Minute // a finished download stays on the lock screen
	liveForget    = 24 * time.Hour
)

func liveRef(requestID int64) string { return fmt.Sprintf("req-%d", requestID) }

func (n *Notifier) live(ctx context.Context, devs []store.PushIOS) {
	now := n.now()
	n.st.PrunePushLive(now.Add(-liveForget).Unix())
	downloads, err := n.st.LiveDownloads()
	if err != nil {
		n.log.Warn("push: live downloads", "err", err)
		return
	}
	lives, err := n.st.PushLives()
	if err != nil {
		n.log.Warn("push: live activities", "err", err)
		return
	}
	type key struct {
		dev string
		req int64
	}
	known := map[key]store.PushLive{}
	for _, l := range lives {
		known[key{l.DeviceID, l.RequestID}] = l
	}
	byDevice := map[string]store.PushIOS{}
	for _, d := range devs {
		byDevice[d.DeviceID] = d
	}

	for _, dl := range downloads {
		for _, d := range devs {
			if ctx.Err() != nil {
				return
			}
			if !d.Live || d.Inbox() || d.UserID != *dl.Request.UserID {
				continue
			}
			k := key{d.DeviceID, dl.Request.ID}
			l, seen := known[k]
			delete(known, k)
			st := n.liveState(dl, d, now)
			switch {
			case !seen:
				err := n.sendLive(ctx, d, dl.Request.ID, "start", st, 10, now.Add(liveStale).Unix(), 0)
				if err != nil && !errors.Is(err, errNoLiveToken) {
					n.log.Warn("push: live start", "id", dl.Request.ID, "err", err)
					continue
				}
				// without a push-to-start token (older iOS, Live Activities off) it is not tried again
				n.st.SavePushLive(store.PushLive{DeviceID: d.DeviceID, RequestID: dl.Request.ID, StartedAt: now.Unix(), SentAt: now.Unix(),
					Progress: st.Progress, State: st.State, Ended: err != nil})
			case l.Ended:
			default:
				since := now.Sub(time.Unix(l.SentAt, 0))
				moved := st.Progress-l.Progress >= 0.01 || l.Progress-st.Progress >= 0.01
				changed := st.State != l.State
				if !changed && !(moved && since >= liveEvery) && since < liveKeepAlive {
					continue
				}
				prio := 5
				if changed {
					prio = 10
				}
				err := n.sendLive(ctx, d, dl.Request.ID, "update", st, prio, now.Add(liveStale).Unix(), 0)
				switch {
				case errors.Is(err, errNoLiveToken):
					// the app has not handed in the activity's token yet, or the user dismissed it
					if now.Sub(time.Unix(l.StartedAt, 0)) > liveNoToken {
						l.Ended = true
						n.st.SavePushLive(l)
					}
				case err != nil:
					n.log.Warn("push: live update", "id", dl.Request.ID, "err", err)
				default:
					l.SentAt, l.Progress, l.State = now.Unix(), st.Progress, st.State
					n.st.SavePushLive(l)
				}
			}
		}
	}

	// what is left is no longer downloading (or the device stopped wanting it): end it
	for _, l := range known {
		if l.Ended || ctx.Err() != nil {
			continue
		}
		d, ok := byDevice[l.DeviceID]
		if !ok {
			continue
		}
		q, err := n.st.AcqRequest(l.RequestID)
		final := LiveState{V: 1, State: "canceled", At: now.Unix()}
		dismiss := now.Unix()
		if err == nil {
			final.Title, final.Kind = q.Title, libKind(q.Kind)
			switch q.Status {
			case store.ReqAvailable:
				final.State, final.Progress = "available", 1
				final.Item = n.st.LibraryItemByTmdb(libKind(q.Kind), q.TmdbID)
				final.Title = n.itemTitle(final.Item, d, q.Title)
				dismiss = now.Add(liveLinger).Unix()
			case store.ReqFailed:
				final.State, final.Progress = "failed", l.Progress
				dismiss = now.Add(liveLinger).Unix()
			}
		}
		err = n.sendLive(ctx, d, l.RequestID, "end", final, 10, 0, dismiss)
		if err != nil && !errors.Is(err, errNoLiveToken) {
			n.log.Warn("push: live end", "id", l.RequestID, "err", err)
			continue
		}
		l.Ended, l.SentAt = true, now.Unix()
		n.st.SavePushLive(l)
	}
}

func (n *Notifier) liveState(dl store.LiveDownload, d store.PushIOS, now time.Time) LiveState {
	q := dl.Request
	st := LiveState{V: 1, Kind: libKind(q.Kind), State: dl.State, At: now.Unix(),
		Progress: float64(int(dl.Progress*1000)) / 1000}
	st.Title = n.itemTitle(n.st.LibraryItemByTmdb(st.Kind, q.TmdbID), d, q.Title)
	if dl.ETA > 0 && dl.ETA < 7*24*3600 {
		st.ETA = dl.ETA
	}
	return st
}

var errNoLiveToken = errors.New("push: nincs Live Activity token")

func (n *Notifier) sendLive(ctx context.Context, d store.PushIOS, requestID int64, event string, st LiveState, priority int, stale, dismissal int64) error {
	st.Title = clip(st.Title, 120)
	plain, err := json.Marshal(st)
	if err != nil {
		return err
	}
	sealed, err := Seal(d.Key, plain)
	if err != nil {
		return err
	}
	err = n.Relay.Live(ctx, LiveRequest{Handle: d.Handle, Ref: liveRef(requestID), Event: event, Payload: sealed,
		Priority: priority, StaleDate: stale, DismissalDate: dismissal})
	var re *RelayError
	switch {
	case errors.As(err, &re) && re.Status == http.StatusNotFound && re.Code == "no_live_token":
		return errNoLiveToken
	case errors.As(err, &re) && re.Code == "unknown_handle":
		n.st.DeletePushIOSHandle(d.Handle)
		return errNoLiveToken
	}
	return err
}

// --- sending ----------------------------------------------------------------------

// SendUser pushes m to every paired device of an account. Dead handles are dropped.
// It returns the first error that was not a dead handle.
func (n *Notifier) SendUser(ctx context.Context, userID int64, m Message) error {
	devs, err := n.st.PushIOSForUser(userID)
	if err != nil {
		return err
	}
	var first error
	for _, d := range devs {
		if err := n.SendDevice(ctx, d, m); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// SendDevice pushes m to one paired device.
func (n *Notifier) SendDevice(ctx context.Context, d store.PushIOS, m Message) error {
	plain, err := encode(m)
	if err != nil {
		return err
	}
	if d.Inbox() {
		// the Android app fetches it (GET /api/push/android/inbox); nothing to seal, it stays on this server
		return n.st.AddPushInbox(d.DeviceID, plain)
	}
	sealed, err := Seal(d.Key, plain)
	if err != nil {
		return err
	}
	err = n.Relay.Send(ctx, d.Handle, sealed, m.Tag)
	if errors.Is(err, ErrGone) {
		n.st.DeletePushIOSHandle(d.Handle)
		return nil
	}
	var re *RelayError
	if errors.As(err, &re) && re.Code == "unknown_handle" {
		n.st.DeletePushIOSHandle(d.Handle)
		return nil
	}
	return err
}

// encode marshals m, shortening the body (then the title) until it fits.
func encode(m Message) ([]byte, error) {
	m.Title, m.Body = clip(m.Title, 150), clip(m.Body, 400)
	for range 16 {
		b, err := json.Marshal(m)
		if err != nil || len(b) <= maxPlain {
			return b, err
		}
		if r := []rune(m.Body); len(r) > 40 {
			m.Body = string(r[:len(r)*2/3]) + "…"
		} else if r := []rune(m.Title); len(r) > 40 {
			m.Title = string(r[:len(r)*2/3]) + "…"
		} else {
			m.Image, m.Href = "", ""
		}
	}
	return nil, errors.New("push: az üzenet túl hosszú")
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
