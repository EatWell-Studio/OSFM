package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/EatWell-Studio/OSFM/internal/auth"
	"github.com/EatWell-Studio/OSFM/internal/backup"
	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
	"github.com/EatWell-Studio/OSFM/internal/store"
	"github.com/EatWell-Studio/OSFM/internal/version"
)

// Backups (G5, T042, M2-Q16): maintainers create, list, download and delete backups of the instance in
// pb_data/backups. A backup (internal/backup) holds the database, both layers and the instance keys, the master key
// included: whoever has the file can read every member's TOTP secret. So every route is for maintainers only, and
// creating, downloading and deleting need a fresh proof (auth.RequireFresh) and are audited. Restoring is not a route:
// it needs the instance stopped and is done with `osfm restore` on the server.
//
//   - GET [BackupsPath]: the backups, newest first: OSFM's (osfm_<UTC time>.zip) and those taken before migrations
//     (premigrate_<UTC time>.zip; pb_data only, without the keys).
//   - POST [BackupsPath]: creates a backup now and answers it (201); one at a time (409 [CodeBackupRunning]). Audit
//     [AuditBackupCreate].
//   - GET [BackupPath]: the file, as a download. Audit [AuditBackupDownload].
//   - POST [BackupDeletePath]: removes the file (204). Audit [AuditBackupDelete].
//
// A backup is named by its file name; any other name is 404.

// Paths of the backup routes.
const (
	BackupsPath      = "/api/osfm/backups"
	BackupPath       = "/api/osfm/backups/{name}"
	BackupDeletePath = "/api/osfm/backups/{name}/delete"
)

// Kinds of backup ([BackupInfo].Kind).
const (
	// BackupKindOSFM is a full backup: database, layers and keys; `osfm restore` restores it.
	BackupKindOSFM = "osfm"
	// BackupKindPremigrate is the copy of pb_data OSFM takes before applying migrations; it has no keys.
	BackupKindPremigrate = "premigrate"
)

// Error codes of the backup routes.
const (
	// CodeBackupRunning: another backup is being written (409); try again when it is done.
	CodeBackupRunning = "backup_running"
	// CodeBackupsUnavailable: the server has no data directory configuration for backups (503).
	CodeBackupsUnavailable = "backups_unavailable"
)

// Audit actions of the backup routes (actor: the maintainer; details: the file name, and the size for a new one).
const (
	AuditBackupCreate   = "backups.create"
	AuditBackupDownload = "backups.download"
	AuditBackupDelete   = "backups.delete"
)

// BackupContentType is the media type of a downloaded backup.
const BackupContentType = "application/zip"

// BackupInfo is a backup file.
type BackupInfo struct {
	// Name is the file name, which also names the backup in the routes.
	Name string `json:"name"`
	// Kind is [BackupKindOSFM] or [BackupKindPremigrate].
	Kind string `json:"kind"`
	// Size is the file size in bytes.
	Size int64 `json:"size"`
	// CreatedAt is when it was taken (from its name).
	CreatedAt time.Time `json:"createdAt"`
}

// BackupList is the answer of GET [BackupsPath].
type BackupList struct {
	Backups []BackupInfo `json:"backups"`
}

// Middleware IDs of the backup budgets.
const (
	backupCreateBudgetID   = "osfmAPIBackupCreateBudget"
	backupDownloadBudgetID = "osfmAPIBackupDownloadBudget"
)

// backupCreateRate is every maintainer's budget of new backups: 5 at once, then one every 5 minutes. Each copies the
// whole database. backupDownloadRate: 10 at once, then one a minute.
var (
	backupCreateRate   = ratelimit.Rate{Burst: 5, Every: 5 * time.Minute}
	backupDownloadRate = ratelimit.Rate{Burst: 10, Every: time.Minute}
)

// registerBackups registers the routes of this feature.
func registerBackups(se *core.ServeEvent, s *server) {
	h := &backupHandlers{s: s}
	se.Router.GET(BackupsPath, h.list).Bind(MaintainersOnly())
	se.Router.POST(BackupsPath, h.create).
		Bind(MaintainersOnly(), auth.RequireFresh(), s.MemberBudget(backupCreateBudgetID, backupCreateRate))
	se.Router.GET(BackupPath, h.download).
		Bind(MaintainersOnly(), auth.RequireFresh(), s.MemberBudget(backupDownloadBudgetID, backupDownloadRate))
	se.Router.POST(BackupDeletePath, h.delete).Bind(MaintainersOnly(), auth.RequireFresh(), s.WriteBudget())
}

type backupHandlers struct {
	s *server
	// running is held while a backup is written: one at a time per instance.
	running sync.Mutex
}

// backupsDir returns pb_data/backups.
func backupsDir(e *core.RequestEvent) string {
	return filepath.Join(e.App.DataDir(), core.LocalBackupsDirName)
}

func backupInfo(i backup.Info) BackupInfo {
	return BackupInfo{Name: i.Name, Kind: i.Kind, Size: i.Size, CreatedAt: i.CreatedAt}
}

func (h *backupHandlers) list(e *core.RequestEvent) error {
	list, err := backup.List(backupsDir(e))
	if err != nil {
		return e.InternalServerError("", err)
	}
	out := BackupList{Backups: make([]BackupInfo, len(list))}
	for i, b := range list {
		out.Backups[i] = backupInfo(b)
	}
	return respond(e, http.StatusOK, out)
}

func (h *backupHandlers) create(e *core.RequestEvent) error {
	cfg := h.s.Config()
	if cfg == nil {
		return fail(e, http.StatusServiceUnavailable, CodeBackupsUnavailable, "Backups are not available.")
	}
	if !h.running.TryLock() {
		return fail(e, http.StatusConflict, CodeBackupRunning, "A backup is being written. Try again when it is done.")
	}
	defer h.running.Unlock()

	// Not the request's context: a backup that has started is finished even if the browser goes away, rather than
	// leaving a half-written file behind for nothing.
	src := backup.Source{PBDataDir: e.App.DataDir(), SecretsDir: cfg.SecretsDir(), SoftwareVersion: version.String()}
	path, _, err := backup.Create(context.WithoutCancel(e.Request.Context()), backupsDir(e), src, time.Now())
	if errors.Is(err, backup.ErrExists) {
		return fail(e, http.StatusConflict, CodeBackupRunning, "A backup was written just now. Try again.")
	}
	if err != nil {
		return e.InternalServerError("", err)
	}
	info, err := backup.Stat(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return e.InternalServerError("", err)
	}
	if err := store.WriteAudit(e.App, auth.CurrentMember(e), AuditBackupCreate, "", "",
		map[string]any{"name": info.Name, "size": info.Size}); err != nil {
		return e.InternalServerError("", err)
	}
	return respond(e, http.StatusCreated, backupInfo(info))
}

func (h *backupHandlers) download(e *core.RequestEvent) error {
	f, info, err := backup.Open(backupsDir(e), e.Request.PathValue("name"))
	if errors.Is(err, backup.ErrNotFound) {
		return notFound(e)
	}
	if err != nil {
		return e.InternalServerError("", err)
	}
	defer func() { _ = f.Close() }()
	// Audited before the first byte leaves: a download that breaks off has still handed out part of the file.
	if err := store.WriteAudit(e.App, auth.CurrentMember(e), AuditBackupDownload, "", "",
		map[string]any{"name": info.Name}); err != nil {
		return e.InternalServerError("", err)
	}
	hdr := e.Response.Header()
	hdr.Set("Content-Type", BackupContentType)
	hdr.Set("Content-Disposition", `attachment; filename="`+info.Name+`"`) // the backup name rule: [a-z0-9_.]
	hdr.Set("Content-Length", strconv.FormatInt(info.Size, 10))
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Content-Type-Options", "nosniff")
	e.Response.WriteHeader(http.StatusOK)
	// Streamed from the file: never the whole backup in memory.
	_, err = io.Copy(e.Response, io.LimitReader(f, info.Size))
	return err
}

func (h *backupHandlers) delete(e *core.RequestEvent) error {
	name := e.Request.PathValue("name")
	err := backup.Delete(backupsDir(e), name)
	if errors.Is(err, backup.ErrNotFound) {
		return notFound(e)
	}
	if err != nil {
		return e.InternalServerError("", err)
	}
	if err := store.WriteAudit(e.App, auth.CurrentMember(e), AuditBackupDelete, "", "",
		map[string]any{"name": name}); err != nil {
		return e.InternalServerError("", err)
	}
	return e.NoContent(http.StatusNoContent)
}
